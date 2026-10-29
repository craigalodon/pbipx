package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/baseline"
	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/scan"
)

const testPolicy = `version = 1
protected_roots = ["pbip"]

[scan]
report = ["pbip/**/*.json"]
prose  = ["notes/**/*.md"]

[columns]
block = ["full_name"]
warn  = ["cohort_year"]
`

// Planted values: the name is the sentinel; the others are values too.
const (
	plantedCohort = "Fall 2150"
	plantedEmail  = "jane.doe@example.com"
)

// reportScanner stands in for the report scanner: each line
// "<table>.<column>=<value>" is a literal on that column.
func reportScanner(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	var out []finding.Finding
	for i, line := range strings.Split(string(data), "\n") {
		col, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		table, name, _ := strings.Cut(col, ".")
		if v, fired := judge.Literal(value, judge.String, []finding.Column{{Table: table, Name: name}}, p); fired {
			out = append(out, v.Finding(path, finding.SlicerSelection, "entry"+strings.Repeat("+", i), value))
		}
	}
	return out, nil
}

// isolate clears what would change pbipx's behavior from outside the test:
// every GIT_ variable (a pre-commit hook sets GIT_INDEX_FILE), git's user and
// system configuration, and the no-reveal variables.
func isolate(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") || name == "CI" || name == "PRE_COMMIT" || name == "PBIPX_NO_REVEAL" {
			t.Setenv(name, value) // restored after the test
			_ = os.Unsetenv(name)
		}
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	prevScanners, prevTerminal, prevNow := scanners, stdoutIsTerminal, now
	scanners = func() map[policy.Kind]judge.Scanner {
		m := scan.Scanners()
		m[policy.Report] = reportScanner
		return m
	}
	stdoutIsTerminal = func(io.Writer) bool { return false }
	now = func() time.Time { return time.Date(2100, 6, 15, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { scanners, stdoutIsTerminal, now = prevScanners, prevTerminal, prevNow })
}

// newRepo makes a committed repository in a directory named dirName, with
// the test policy, a block and a warning in reports, and a note with a name.
// It changes into the repository.
func newRepo(t *testing.T, dirName string) string {
	t.Helper()
	isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, dirName)
	files := map[string]string{
		policy.FileName:                       testPolicy,
		"pbip/A.Report/definition/warn.json":  "enrollment.cohort_year=" + plantedCohort,
		"pbip/A.Report/definition/block.json": "student.full_name=" + sentinel,
		"notes/review.md":                     "# Review\n\n- " + sentinel + "\nmail " + plantedEmail + "\n",
	}
	for name, content := range files {
		writeFile(t, root, name, content)
	}
	if err := corpus.GitSetup(root, nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type outcome struct {
	code           int
	stdout, stderr string
}

func runIn(t *testing.T, stdin string, args ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(stdin), &stdout, &stderr)
	return outcome{code, stdout.String(), stderr.String()}
}

// leaks names the planted value found in the output, or "".
func (o outcome) leaks() string {
	all := o.stdout + o.stderr
	for _, v := range []string{"Doe", "Jane", plantedCohort, "2150", plantedEmail, "jane.doe"} {
		if strings.Contains(all, v) {
			return v
		}
	}
	return ""
}

func jsonResult(t *testing.T, o outcome) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(o.stdout), &m); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	return m
}

func TestNoOutputCarriesAValue(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		stdin string
		args  []string
		code  int
	}{
		{"scan human", nil, "", []string{"scan"}, finding.ExitBlock},
		{"scan json", nil, "", []string{"scan", "--all", "--format", "json"}, finding.ExitBlock},
		{"scan shape", nil, "", []string{"scan", "--show-shape", "--strict"}, finding.ExitBlock},
		{"scan staged", nil, "", []string{"scan", "--staged"}, finding.ExitBlock},
		{"scan rev", nil, "", []string{"scan", "--rev", "HEAD"}, finding.ExitBlock},
		{"scan paths", nil, "", []string{"scan", "notes", "pbip/A.Report/definition/warn.json"}, finding.ExitBlock},
		{"scan-text file", nil, "", []string{"scan-text", "notes/review.md"}, finding.ExitBlock},
		{"scan-text stdin", nil, "Roe, Richard\n" + sentinel + "\n", []string{"scan-text", "-", "--format", "json"}, finding.ExitBlock},
		{"scan-text commit message", nil, "# " + sentinel + "\nsubject\n", []string{"scan-text", "--commit-msg", "-"}, finding.ExitClean},
		// An argument that names no file is reported by position, never
		// echoed.
		{"scan-text missing file", nil, "", []string{"scan-text", sentinel}, finding.ExitOperational},
		{"scan-text missing file json", nil, "", []string{"scan-text", "--format", "json", "notes/" + sentinel + ".md"}, finding.ExitOperational},
		{"scan-text directory", func(t *testing.T, root string) {
			writeFile(t, root, "notes/"+sentinel+"/a.md", "clean\n")
		}, "", []string{"scan-text", "notes/" + sentinel}, finding.ExitOperational},
		{"scan-text outside the repository", nil, "", []string{"scan-text", "../" + sentinel}, finding.ExitOperational},
		{"policy error", func(t *testing.T, root string) {
			writeFile(t, root, policy.FileName, testPolicy+"\n[values]\nallow = [\""+sentinel+"\"\n")
		}, "", []string{"scan", "--format", "json"}, finding.ExitOperational},
		{"policy unknown key", func(t *testing.T, root string) {
			writeFile(t, root, policy.FileName, testPolicy+"\n[\""+sentinel+"\"]\n")
		}, "", []string{"scan"}, finding.ExitOperational},
		{"policy flag", nil, "", []string{"scan", "--policy", sentinel + ".toml"}, finding.ExitOperational},
		{"baseline error", func(t *testing.T, root string) {
			writeFile(t, root, baseline.FileName, "version = 1\n[entries.\""+sentinel+"\"]\nseverity = \"block\"\n")
		}, "", []string{"scan", "--format", "json"}, finding.ExitOperational},
		{"bad revision", nil, "", []string{"scan", "--rev", sentinel}, finding.ExitUsage},
		{"revision as an option", nil, "", []string{"scan", "--rev", "--output=" + sentinel}, finding.ExitUsage},
		{"path outside", nil, "", []string{"scan", "../" + sentinel}, finding.ExitUsage},
		{"path missing", nil, "", []string{"scan", "notes/" + sentinel}, finding.ExitUsage},
		{"two sources", nil, "", []string{"scan", "--staged", "notes/" + sentinel}, finding.ExitUsage},
		{"bad format", nil, "", []string{"scan", "--format", sentinel}, finding.ExitUsage},
		{"record unknown", nil, "", []string{"baseline", "record", sentinel, "--note", sentinel}, finding.ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newRepo(t, "repo")
			if tt.setup != nil {
				tt.setup(t, root)
			}
			o := runIn(t, tt.stdin, tt.args...)
			if o.code != tt.code {
				t.Errorf("exit = %d, want %d (stderr %q)", o.code, tt.code, o.stderr)
			}
			if v := o.leaks(); v != "" {
				t.Errorf("output carries a planted value")
			}
		})
	}
}

func TestScanJSON(t *testing.T) {
	newRepo(t, "repo")
	o := runIn(t, "", "scan", "--format", "json")
	m := jsonResult(t, o)
	findings, _ := m["findings"].([]any)
	if len(findings) != 4 {
		t.Fatalf("got %d findings, want 4 (block, warning, two prose lines)", len(findings))
	}
	for _, f := range findings {
		if fp, _ := f.(map[string]any)["fingerprint"].(string); !strings.HasPrefix(fp, "fp1:") {
			t.Errorf("a finding has no fingerprint")
		}
	}
}

func TestScanHuman(t *testing.T) {
	newRepo(t, "repo")
	o := runIn(t, "", "scan")
	for _, want := range []string{"block  notes/review.md", "context      text", "locator      line:3", "column       student.full_name",
		"reasons      block-column, person-name-shape", "fingerprint  fp1:", "value        <redacted>",
		"pbipx: 3 blocks, 1 warning, 0 errors; 0 accepted by policy, 0 suppressed by the baseline"} {
		if !strings.Contains(o.stdout, want) {
			t.Errorf("human output lacks %q", want)
		}
	}
	shaped := runIn(t, "", "scan", "--show-shape")
	if !strings.Contains(shaped.stdout, "value        Aa{2}, Aa{3}") {
		t.Errorf("--show-shape does not print the silhouette")
	}
}

func TestNotARepository(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)
	for _, args := range [][]string{{"scan"}, {"scan-text", "x.md"}, {"baseline", "verify"}} {
		o := runIn(t, "", args...)
		if o.code != finding.ExitOperational || !strings.HasPrefix(o.stderr, "pbipx: not-a-repository:") {
			t.Errorf("%v: exit %d, stderr %q", args, o.code, o.stderr)
		}
	}
	o := runIn(t, "", "scan", "--format", "json")
	if jsonResult(t, o)["failure"] != "not-a-repository" {
		t.Errorf("json failure = %v", o.stdout)
	}
}

func TestGitStderrNeverReachesOutput(t *testing.T) {
	// The repository's own path holds the value, and git's messages quote
	// paths. A corrupt index makes git fail and complain about it.
	root := newRepo(t, "Doe-Jane-repo")
	writeFile(t, root, ".git/index", "not an index")
	o := runIn(t, "", "scan", "--staged")
	if o.code != finding.ExitOperational {
		t.Errorf("exit = %d, want %d", o.code, finding.ExitOperational)
	}
	if !strings.Contains(o.stderr, "git ls-files failed") {
		t.Errorf("stderr = %q, want the failing subcommand", o.stderr)
	}
	if o.leaks() != "" {
		t.Error("git's output reached pbipx's output")
	}
}

func TestShowValuesRefused(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		terminal bool
		args     []string
	}{
		{"not a terminal", "", false, nil},
		{"CI", "CI", true, nil},
		{"PRE_COMMIT", "PRE_COMMIT", true, nil},
		{"PBIPX_NO_REVEAL", "PBIPX_NO_REVEAL", true, nil},
		{"empty CI", "CI=", true, nil},
		{"json", "", true, []string{"--format", "json"}},
	}
	for _, tt := range tests {
		for _, command := range [][]string{{"scan"}, {"scan-text", "notes/review.md"}} {
			t.Run(tt.name+"/"+command[0], func(t *testing.T) {
				newRepo(t, "repo")
				stdoutIsTerminal = func(io.Writer) bool { return tt.terminal }
				if tt.env != "" {
					name, value, cut := strings.Cut(tt.env, "=")
					if !cut {
						value = "1"
					}
					t.Setenv(name, value)
				}
				args := append(append(append([]string{}, command...), "--show-values"), tt.args...)
				o := runIn(t, "", args...)
				if o.code != finding.ExitUsage {
					t.Errorf("exit = %d, want %d", o.code, finding.ExitUsage)
				}
				if o.stdout != "" || !strings.Contains(o.stderr, "--show-values is refused") {
					t.Errorf("stdout %q, stderr %q", o.stdout, o.stderr)
				}
				if o.leaks() != "" {
					t.Error("a refused --show-values printed a value")
				}
			})
		}
	}
}

func TestShowValuesOnATerminal(t *testing.T) {
	newRepo(t, "repo")
	stdoutIsTerminal = func(io.Writer) bool { return true }
	o := runIn(t, "", "scan-text", "--show-values", "notes/review.md")
	if o.code != finding.ExitBlock || !strings.Contains(o.stdout, "value        - "+sentinel) {
		t.Errorf("exit %d; the value is not shown on an interactive terminal", o.code)
	}
}

func TestScanTextNamesArgumentsByPosition(t *testing.T) {
	newRepo(t, "repo")
	o := runIn(t, "", "scan-text", "--format", "json", "notes/review.md", sentinel)
	var got struct {
		Findings []struct {
			Path string `json:"path"`
			Tier string `json:"tier"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(o.stdout), &got); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, f := range got.Findings {
		paths[f.Path] = f.Tier
	}
	if paths["notes/review.md"] != "block" || paths["argument 2"] != "error" || len(paths) != 2 {
		t.Errorf("paths = %v, want the file by its path and the missing one by position", paths)
	}
	if o.leaks() != "" {
		t.Error("output carries the argument")
	}
}

func TestScanTextNeedsInput(t *testing.T) {
	newRepo(t, "repo")
	if o := runIn(t, "", "scan-text"); o.code != finding.ExitUsage {
		t.Errorf("exit = %d, want %d", o.code, finding.ExitUsage)
	}
}

func fingerprintOf(t *testing.T, tier string) string {
	t.Helper()
	o := runIn(t, "", "scan", "--no-baseline", "--format", "json")
	for _, f := range jsonResult(t, o)["findings"].([]any) {
		m := f.(map[string]any)
		if m["tier"] == tier && m["context"] == "slicer-selection" {
			return m["fingerprint"].(string)
		}
	}
	t.Fatalf("no %s finding", tier)
	return ""
}

func TestBaselineRecordAndVerify(t *testing.T) {
	root := newRepo(t, "repo")
	warn := fingerprintOf(t, "warn")
	block := fingerprintOf(t, "block")

	if o := runIn(t, "", "baseline", "record", block, "--note", "reviewed"); o.code != finding.ExitUsage {
		t.Errorf("recording a block: exit %d, want %d", o.code, finding.ExitUsage)
	}
	if o := runIn(t, "", "baseline", "record", warn); o.code != finding.ExitUsage {
		t.Errorf("recording without a note: exit %d, want %d", o.code, finding.ExitUsage)
	}
	for _, env := range []string{"CI", "PRE_COMMIT"} {
		t.Setenv(env, "1")
		if o := runIn(t, "", "baseline", "record", warn, "--note", "reviewed"); o.code != finding.ExitOperational {
			t.Errorf("recording under %s: exit %d, want %d", env, o.code, finding.ExitOperational)
		}
		_ = os.Unsetenv(env)
	}
	if _, err := os.Stat(filepath.Join(root, baseline.FileName)); err == nil {
		t.Fatal("a refused record wrote the baseline")
	}

	o := runIn(t, "", "baseline", "record", warn, "--note", "a reviewed persisted selection")
	if o.code != finding.ExitClean || !strings.Contains(o.stdout, "expires 2100-12-12") {
		t.Fatalf("record: exit %d, stdout %q, stderr %q", o.code, o.stdout, o.stderr)
	}
	data, err := os.ReadFile(filepath.Join(root, baseline.FileName)) //nolint:gosec // G304: the test's own repository
	if err != nil {
		t.Fatal(err)
	}
	if (outcome{stdout: string(data)}).leaks() != "" {
		t.Fatal("the baseline file holds a value")
	}

	scanned := runIn(t, "", "scan", "--strict", "--format", "json")
	if jsonResult(t, scanned)["suppressed"] != 1.0 {
		t.Errorf("the recorded warning is not suppressed")
	}
	if o := runIn(t, "", "baseline", "verify"); o.code != finding.ExitClean {
		t.Errorf("verify: exit %d", o.code)
	}
	now = func() time.Time { return time.Date(2101, 1, 1, 0, 0, 0, 0, time.UTC) }
	o = runIn(t, "", "baseline", "verify")
	if o.code != finding.ExitWarnStrict || !strings.Contains(o.stdout, "expired  "+warn) {
		t.Errorf("verify after expiry: exit %d, stdout %q", o.code, o.stdout)
	}
	if o := runIn(t, "", "baseline"); o.code != finding.ExitUsage {
		t.Errorf("baseline alone: exit %d", o.code)
	}
}
