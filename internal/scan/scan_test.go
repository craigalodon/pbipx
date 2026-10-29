package scan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/baseline"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/secret"
)

// The value planted in these tests.
const sentinel = "Doe, Jane"

const testPolicy = `version = 1
protected_roots = ["pbip"]

[scan]
report  = ["pbip/**/*.json"]
model   = ["pbip/**/*.tmdl"]
prose   = ["notes/**/*.md"]
exclude = ["**/.pbi/**"]

[files]
local_state     = ["**/.pbi/**"]
unscanned_allow = ["pbip/**/*.png"]

[columns]
block = ["full_name"]
warn  = ["cohort_year", "section_number"]

[values]
allow = ["(none selected)"]

[[accept]]
column      = "enrollment.section_number"
path        = "pbip/A.Report/definition"
note        = "reviewed"
reviewed_by = "a reviewer"
`

func loadPolicy(t *testing.T, doc string) *policy.Policy {
	t.Helper()
	path := filepath.Join(t.TempDir(), policy.FileName)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// files is a Reader over a map; a path that is absent fails to read.
type files map[string]string

func (f files) Read(path string) ([]byte, error) {
	s, ok := f[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(s), nil
}

func (f files) paths() []string {
	out := make([]string, 0, len(f))
	for p := range f {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

type summary struct {
	path    string
	tier    finding.Tier
	reasons string
}

func summarize(fs []finding.Finding) []summary {
	out := make([]summary, len(fs))
	for i, f := range fs {
		rs := make([]string, len(f.Reasons))
		for j, r := range f.Reasons {
			rs[j] = string(r)
		}
		out[i] = summary{f.Path, f.Tier, strings.Join(rs, ",")}
	}
	return out
}

// warnScanner is a stand-in report scanner: each line "<table>.<column>=<value>"
// is a literal on that column.
func warnScanner(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	var out []finding.Finding
	for i, line := range strings.Split(string(data), "\n") {
		col, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		table, name, _ := strings.Cut(col, ".")
		if v, fired := judge.Literal(value, judge.String, []finding.Column{{Table: table, Name: name}}, p); fired {
			out = append(out, v.Finding(path, finding.SlicerSelection, "line"+string(rune('0'+i)), value))
		}
	}
	return out, nil
}

func TestRunClassifies(t *testing.T) {
	p := loadPolicy(t, testPolicy)
	in := files{
		"pbip/Example.pbix":                 sentinel,
		"pbip/A.Report/.pbi/cache.abf":      sentinel,
		"pbip/A.Report/logo.png":            sentinel,
		"docs/roster.txt":                   sentinel,
		"pbip/A.SemanticModel/model.tmdl":   "table student",
		"notes/clean.md":                    "nothing here\n",
		"notes/leak.md":                     "intro\n- " + sentinel + "\n",
		"notes/latin1.md":                   "caf\xe9\n",
		"notes/bom.md":                      "\xef\xbb\xbf" + sentinel + "\n",
		"pbip/A.Report/definition/a.json":   "enrollment.cohort_year=Fall 2150",
		"pbip/A.Report/definition/b.json":   "student.full_name=x",
		"pbip/A.Report/definition/acc.json": "enrollment.section_number=0100",
	}
	paths := append(in.paths(), "notes/unreadable.md")
	scanners := map[policy.Kind]judge.Scanner{policy.Prose: Prose, policy.Report: warnScanner}
	res := Run(paths, in, Options{Policy: p, Scanners: scanners})

	want := []summary{
		{"notes/bom.md", finding.Block, "person-name-shape"},
		{"notes/latin1.md", finding.Error, "not-utf8"},
		{"notes/leak.md", finding.Block, "person-name-shape"},
		{"notes/unreadable.md", finding.Error, "unreadable"},
		{"pbip/A.Report/definition/a.json", finding.Warn, "warn-column"},
		{"pbip/A.Report/definition/b.json", finding.Block, "block-column"},
		{"pbip/A.SemanticModel/model.tmdl", finding.Error, "unvettable"}, // no model scanner
		{"pbip/Example.pbix", finding.Error, "unvettable"},
	}
	if got := summarize(res.Findings); !slices.Equal(got, want) {
		t.Errorf("findings =\n%v\nwant\n%v", got, want)
	}
	if res.Accepted != 1 || res.Suppressed != 0 {
		t.Errorf("accepted %d, suppressed %d", res.Accepted, res.Suppressed)
	}
	leak := res.Findings[2]
	if leak.Context != finding.Text || leak.Locator != "line:2" || leak.Column != nil {
		t.Errorf("prose finding = %+v", leak)
	}
	// The line is carried, compared by silhouette so the test needs no
	// Reveal.
	if secret.Shape(leak.Value) != secret.Shape(secret.New("- "+sentinel)) {
		t.Error("the prose finding does not carry its line")
	}
}

// A scanned kind with no scanner yet fails closed (principle 8). Models
// have none until phase 3.
func TestDefaultScannersLeaveModelsUnvettable(t *testing.T) {
	p := loadPolicy(t, testPolicy)
	in := files{"pbip/A.SemanticModel/definition/model.tmdl": "model Model"}
	res := Run(in.paths(), in, Options{Policy: p, Scanners: Scanners()})
	if got := summarize(res.Findings); len(got) != 1 || got[0].reasons != "unvettable" {
		t.Errorf("a model with no scanner: %v", got)
	}
}

func TestBaselineSuppressesWarningsOnly(t *testing.T) {
	p := loadPolicy(t, testPolicy)
	in := files{
		"pbip/A.Report/definition/a.json": "enrollment.cohort_year=Fall 2150",
		"pbip/A.Report/definition/b.json": "student.full_name=x",
	}
	scanners := map[policy.Kind]judge.Scanner{policy.Report: warnScanner}
	all := Run(in.paths(), in, Options{Policy: p, Scanners: scanners})
	b := &baseline.Baseline{Entries: map[string]baseline.Entry{}}
	for _, f := range all.Findings {
		b.Entries[f.Fingerprint()] = baseline.Entry{Severity: "warn", Expires: "2199-12-31"}
	}
	now := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	res := Run(in.paths(), in, Options{Policy: p, Scanners: scanners, Baseline: b, Now: now})
	if got := summarize(res.Findings); len(got) != 1 || got[0].tier != finding.Block {
		t.Errorf("findings = %v, want the block alone", got)
	}
	if res.Suppressed != 1 {
		t.Errorf("suppressed = %d, want 1", res.Suppressed)
	}
}

func TestFileScannerFailures(t *testing.T) {
	p := loadPolicy(t, testPolicy)
	tests := []struct {
		name    string
		scanner judge.Scanner
		reason  finding.Reason
		locator string
	}{
		{"parse error", func(string, []byte, *policy.Policy) ([]finding.Finding, error) {
			return nil, &judge.ParseError{Reason: finding.UnparseableTMDL, Line: 7, Msg: "space indentation"}
		}, finding.UnparseableTMDL, "line:7"},
		{"parse error with a finding reason", func(string, []byte, *policy.Policy) ([]finding.Finding, error) {
			return nil, &judge.ParseError{Reason: finding.BlockColumn}
		}, finding.Unvettable, ""},
		{"other error", func(string, []byte, *policy.Policy) ([]finding.Finding, error) {
			return []finding.Finding{{Tier: finding.Block}}, errors.New(sentinel)
		}, finding.Unvettable, ""},
		{"panic", func(string, []byte, *policy.Policy) ([]finding.Finding, error) {
			panic(sentinel)
		}, finding.Unvettable, ""},
	}
	for _, tt := range tests {
		got := File("pbip/a.tmdl", []byte("x"), tt.scanner, p)
		if len(got) != 1 || got[0].Tier != finding.Error || !slices.Equal(got[0].Reasons, []finding.Reason{tt.reason}) || got[0].Locator != tt.locator {
			t.Errorf("%s: %+v", tt.name, got)
		}
	}
}

func TestProseHonorsPolicy(t *testing.T) {
	p := loadPolicy(t, testPolicy+"\n[shapes]\nemail = false\n")
	text := "- (none selected) plus words here\nmail jane.doe@example.com\n# Roe, Richard\n"
	got := summarize(File("notes/a.md", []byte(text), Prose, p))
	want := []summary{{"notes/a.md", finding.Block, "person-name-shape"}}
	if !slices.Equal(got, want) {
		t.Errorf("prose = %v, want %v", got, want)
	}
	// A commit message's comment lines are scanned; the scissors line ends
	// it.
	if got := summarize(File("-", []byte(text), CommitMessage("#"), p)); len(got) != 1 {
		t.Errorf("commit message: comment line was not checked: %v", got)
	}
	cut := "subject\n# ------------------------ >8 ------------------------\n" + text
	if got := File("-", []byte(cut), CommitMessage("#"), p); len(got) != 0 {
		t.Errorf("commit message: lines below the scissors were checked: %v", summarize(got))
	}
	if got := File("-", []byte(cut), CommitMessage(";"), p); len(got) != 1 {
		t.Errorf("commit message: a scissors line with another comment character stopped the scan")
	}
}
