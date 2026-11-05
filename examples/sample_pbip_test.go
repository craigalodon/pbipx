package examples_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/corpus"
)

// The sample project (examples/sample-pbip/README.md). project/ is the clean
// project to copy out; planted/visual.json replaces one visual in it to show
// a finding.
const (
	sampleDir  = "sample-pbip"
	projectDir = "project"
	plantedDir = "planted"
)

// slicerVisual is the visual that planted/visual.json replaces, relative to
// project/.
const slicerVisual = "pbip/School.Report/definition/pages/5d7a0c2e9b1f4a368d02/visuals/1b4e7a0d3c6f9285e1a7/visual.json"

// sentinels are the planted fake person and its parts: none may appear in any
// output of any command.
var sentinels = []string{"Doe, Jane", "Doe", "Jane"}

// isReportJSON reports whether name is a file whose bytes Desktop writes without a final
// newline.
func isReportJSON(name string) bool {
	switch {
	case strings.HasSuffix(name, ".json"), strings.HasSuffix(name, ".pbir"),
		strings.HasSuffix(name, ".pbip"), strings.HasSuffix(name, ".pbism"),
		name == ".platform":
		return true
	}
	return false
}

// TestSampleKeepsDesktopSerialization holds the sample to Desktop's
// serialization (CONTRIBUTING.md): LF, and no final newline in report JSON.
func TestSampleKeepsDesktopSerialization(t *testing.T) {
	for _, top := range []string{projectDir, plantedDir} {
		err := filepath.WalkDir(filepath.Join(sampleDir, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			data, err := os.ReadFile(p) //nolint:gosec // G304: a path found by walking the sample
			if err != nil {
				return err
			}
			if bytes.Contains(data, []byte("\r")) {
				t.Errorf("%s holds a carriage return", p)
			}
			if isReportJSON(d.Name()) && bytes.HasSuffix(data, []byte("\n")) {
				t.Errorf("%s ends with a newline", p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestSampleAdoptsNothing holds the sample to the rule that this repository
// has no policy of its own: no .pbipx.toml at the root or anywhere in the
// sample, so the gate and the tests treat this repository as they did.
func TestSampleAdoptsNothing(t *testing.T) {
	for _, p := range []string{".pbipx.toml", filepath.Join("..", ".pbipx.toml")} {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s exists (or cannot be checked): the sample is only adopted in a copy", p)
		}
	}
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Name() == ".pbipx.toml" {
			t.Errorf("%s: a policy inside examples/ would adopt this repository", p)
		}
		if err == nil && d.Name() == ".git" {
			t.Errorf("%s: examples/ must not hold a git work tree", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestSamplePlantsExactlyOneFinding checks that the clean project plants
// nothing and that the planted file replaces a file of the project.
func TestSamplePlantsExactlyOneFinding(t *testing.T) {
	err := filepath.WalkDir(filepath.Join(sampleDir, projectDir), func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304: a path found by walking the sample
		if err != nil {
			return err
		}
		for _, s := range sentinels {
			if bytes.Contains(data, []byte(s)) {
				t.Errorf("%s holds a planted value", p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	planted, err := os.ReadFile(filepath.Join(sampleDir, plantedDir, "visual.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(planted, []byte("Doe, Jane")) {
		t.Error("planted/visual.json plants nothing")
	}
	if _, err := os.Stat(filepath.Join(sampleDir, projectDir, filepath.FromSlash(slicerVisual))); err != nil {
		t.Errorf("the visual that planted/visual.json replaces: %v", err)
	}
}

// outcome is what one command produced.
type outcome struct {
	exit           int
	stdout, stderr string
}

func (o outcome) all() string { return o.stdout + "\n" + o.stderr }

// demo is the walkthrough's machine: a repository, and the configuration
// directories of pbipx and Claude Code, all temporary.
type demo struct {
	t      *testing.T
	binary string
	root   string
	env    []string
	cfg    string
	// outputs are every command's output, checked for the planted values at
	// the end of the run.
	outputs []outcome
}

func (d *demo) run(stdin []byte, args ...string) outcome {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.binary, args...) //nolint:gosec // G204: the binary is the one the test built
	cmd.Dir = d.root
	cmd.Env = d.env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	o := outcome{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		o.exit = exitErr.ExitCode()
	default:
		d.t.Fatalf("running pbipx %s: %v", args[0], err)
	}
	d.outputs = append(d.outputs, o)
	return o
}

// gate runs the agent hook on a PreToolUse event.
func (d *demo) gate(tool string, input map[string]string) outcome {
	d.t.Helper()
	ev, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"cwd":             d.root,
		"tool_name":       tool,
		"tool_input":      input,
	})
	if err != nil {
		d.t.Fatal(err)
	}
	return d.run(ev, "gate")
}

// readSlicer is the gate's verdict on an agent reading the slicer's file.
func (d *demo) readSlicer() outcome {
	d.t.Helper()
	return d.gate("Read", map[string]string{"file_path": filepath.Join(d.root, filepath.FromSlash(slicerVisual))})
}

func (d *demo) want(step string, o outcome, exit int) {
	d.t.Helper()
	if o.exit != exit {
		d.t.Errorf("%s: exit = %d, want %d", step, o.exit, exit)
	}
}

// copyDir copies the regular files of src into dst, byte for byte.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304: a path found by walking the sample
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600) //nolint:gosec // G703: target is under the test's temporary directory
	})
}

// TestSampleDemo runs the README's walkthrough on a temporary copy, with
// temporary configuration directories for pbipx and Claude Code, and the
// approval written as internal/corpus writes it, not at a terminal.
func TestSampleDemo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH; the demo needs it")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(tmp, "bin", "pbipx")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "github.com/craigalodon/pbipx/cmd/pbipx") //nolint:gosec // G204: the output path is the test's own temporary directory
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building pbipx: %v\n%s", err, out)
	}

	d := &demo{t: t, binary: binary, root: filepath.Join(tmp, "repo"), cfg: filepath.Join(tmp, "pbipx-config")}
	claudeDir := filepath.Join(tmp, "claude-config")
	home := filepath.Join(tmp, "home")
	for _, dir := range []string{d.root, d.cfg, claudeDir, home} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	d.env = append(corpus.CommandEnv(os.Environ(), &corpus.Spec{}, d.cfg),
		"CLAUDE_CONFIG_DIR="+claudeDir,
		"HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "APPDATA="+filepath.Join(home, "AppData"))

	// Step 1: copy the folder out to a fresh git repository.
	if err := copyDir(filepath.Join(sampleDir, projectDir), d.root); err != nil {
		t.Fatal(err)
	}
	if err := corpus.GitSetup(d.root, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Step 2: pbipx init. It writes the policy and the hook, in the
	// temporary directories only.
	o := d.run(nil, "init")
	d.want("init", o, 0)
	policyPath := filepath.Join(d.root, ".pbipx.toml")
	if _, err := os.Stat(policyPath); err != nil {
		t.Fatalf("init wrote no policy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "settings.json")); err != nil {
		t.Errorf("init registered no hook in the temporary Claude configuration: %v", err)
	}
	if !strings.Contains(string(mustRead(t, policyPath)), `protected_roots = ["pbip"]`) {
		t.Error("init did not protect the sample's pbip directory")
	}

	// Step 3: until a person approves the policy, the gate denies.
	o = d.readSlicer()
	d.want("read before approval", o, 2)
	if !strings.HasPrefix(o.stderr, "pbipx: policy-unapproved:") {
		t.Error("the unapproved gate did not name policy-unapproved")
	}

	// Step 4: approve, as internal/corpus does, since a test has no terminal.
	digest, err := approval.DigestFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := corpus.Approve(d.root, d.cfg, digest); err != nil {
		t.Fatal(err)
	}
	o = d.run(nil, "policy", "status")
	d.want("policy status", o, 0)

	// Step 5: the clean project. The read is allowed, the scan is clean, and
	// explain prints structure.
	d.want("clean read", d.readSlicer(), 0)
	d.want("clean cat", d.gate("Bash", map[string]string{"command": "cat " + slicerVisual}), 0)
	d.want("clean scan", d.run(nil, "scan", "--all"), 0)
	o = d.run(nil, "explain", "pbip")
	d.want("clean explain", o, 0)
	for _, name := range []string{"student", "full_name", "Student Count", "Enrollment Count", "enrollment"} {
		if !strings.Contains(o.stdout, name) {
			t.Errorf("explain does not print the name %q", name)
		}
	}

	// Step 6: plant the finding, as the README says: copy planted/visual.json
	// over the slicer's file.
	planted := mustRead(t, filepath.Join(sampleDir, plantedDir, "visual.json"))
	if err := os.WriteFile(filepath.Join(d.root, filepath.FromSlash(slicerVisual)), planted, 0o600); err != nil {
		t.Fatal(err)
	}

	// Step 7: the gate denies reading it, however the agent asks; the scan
	// blocks; explain still prints no value.
	o = d.readSlicer()
	d.want("planted read", o, 2)
	if !strings.HasPrefix(o.stderr, "pbipx: ") {
		t.Error("the deny does not start with the pbipx class prefix")
	}
	d.want("planted cat", d.gate("Bash", map[string]string{"command": "cat " + slicerVisual}), 2)
	d.want("planted grep", d.gate("Grep", map[string]string{"pattern": "Literal", "path": path.Dir(slicerVisual)}), 2)
	o = d.run(nil, "scan", "--all")
	d.want("planted scan", o, 1)
	if !strings.Contains(o.all(), "full_name") {
		t.Error("the scan does not name the column that blocked")
	}
	d.want("planted explain", d.run(nil, "explain", "pbip"), 0)

	// No output of any command holds a planted value.
	for i, out := range d.outputs {
		for _, s := range sentinels {
			if strings.Contains(out.all(), s) {
				t.Errorf("output %d of the run holds a planted value", i)
			}
		}
	}
	if len(d.outputs) < 10 {
		t.Errorf("only %d commands ran", len(d.outputs))
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p) //nolint:gosec // G304: a path the test chose
	if err != nil {
		t.Fatal(err)
	}
	return data
}
