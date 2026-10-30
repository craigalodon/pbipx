package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/gate/claudecode"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/setup"
)

// initEnv is a test repository for init, with a Claude Code configuration
// directory and an installed binary outside it. No test touches the real
// user's settings: HOME and CLAUDE_CONFIG_DIR are temporary.
type initEnv struct {
	root     string // the repository
	home     string
	config   string // CLAUDE_CONFIG_DIR
	store    string // PBIPX_CONFIG_DIR
	exe      string // the binary init reports running as
	settings string
}

// newInitRepo makes a committed repository holding files (empty content
// for each; a trailing "/" makes a directory) and changes into it.
func newInitRepo(t *testing.T, files ...string) initEnv {
	t.Helper()
	isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := initEnv{
		root:   filepath.Join(base, "repo"),
		home:   filepath.Join(base, "home"),
		config: filepath.Join(base, "claude-config"),
		store:  filepath.Join(base, "pbipx-config"),
		exe:    filepath.Join(base, "bin", "pbipx"),
	}
	e.settings = filepath.Join(e.config, "settings.json")
	if err := os.MkdirAll(e.root, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "/") {
			if err := os.MkdirAll(filepath.Join(e.root, filepath.FromSlash(f)), 0o750); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, e.root, f, "")
	}
	writeFile(t, filepath.Dir(e.exe), "pbipx", "")
	if err := corpus.GitSetup(e.root, nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", e.home)
	t.Setenv("USERPROFILE", e.home)
	t.Setenv("CLAUDE_CONFIG_DIR", e.config)
	t.Setenv(approval.EnvConfigDir, e.store)
	e.useExe(t, e.exe)
	t.Chdir(e.root)
	return e
}

func (e initEnv) useExe(t *testing.T, path string) {
	t.Helper()
	prev := executable
	executable = func() (string, error) { return path, nil }
	t.Cleanup(func() { executable = prev })
}

// written reports whether init wrote anything: the policy, the settings
// directory, or .gitignore.
func (e initEnv) written(t *testing.T) []string {
	t.Helper()
	var w []string
	for _, p := range []string{
		filepath.Join(e.root, policy.FileName), filepath.Join(e.root, ".gitignore"), e.config, filepath.Join(e.home, ".claude"),
		filepath.Join(e.store, "approved"),
	} {
		if _, err := os.Lstat(p); err == nil {
			w = append(w, p)
		}
	}
	return w
}

// registeredHooks are the PreToolUse commands in a settings file.
func registeredHooks(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // G304: a test's temporary file
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for _, e := range s.Hooks.PreToolUse {
		for _, h := range e.Hooks {
			cmds = append(cmds, e.Matcher+" "+h.Command)
		}
	}
	return cmds
}

func TestInitOneProject(t *testing.T) {
	e := newInitRepo(t,
		"reports/Example.Report/definition.pbir",
		"reports/Example.Report/definition/report.json",
		"reports/Example.SemanticModel/definition/tables/student.tmdl",
		"reports/Example.pbip",
	)
	o := runIn(t, "", "init")
	if o.code != 0 {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	p, err := policy.Load(filepath.Join(e.root, policy.FileName))
	if err != nil {
		t.Fatalf("the written policy does not load: %v", err)
	}
	if class, kind := p.Classify("reports/Example.Report/definition/report.json"); class != policy.Scanned || kind != policy.Report {
		t.Errorf("a report file is %s %s, want scanned report", class, kind)
	}
	if !strings.Contains(o.stdout, "Protected roots:\n  reports\n") {
		t.Errorf("stdout does not list the root:\n%s", o.stdout)
	}
	for _, want := range []string{"pbipx policy approve", "pbipx-scan", "pbipx-commit-msg", "--gitignore", e.settings} {
		if !strings.Contains(o.stdout, want) {
			t.Errorf("stdout does not mention %q", want)
		}
	}
	want := setupHookLine(e.exe)
	if got := registeredHooks(t, e.settings); len(got) != 1 || got[0] != want {
		t.Errorf("hooks = %q, want [%q]", got, want)
	}
	if data, _ := os.ReadFile(e.settings); !strings.Contains(string(data), `"timeout": 300`) {
		t.Errorf("the hook has no 300-second timeout")
	}

	// A second run changes nothing and keeps the policy.
	before, _ := os.ReadFile(e.settings)
	policyBefore, _ := os.ReadFile(filepath.Join(e.root, policy.FileName))
	o = runIn(t, "", "init")
	if o.code != 0 {
		t.Fatalf("second run: exit = %d", o.code)
	}
	after, _ := os.ReadFile(e.settings)
	policyAfter, _ := os.ReadFile(filepath.Join(e.root, policy.FileName))
	if string(before) != string(after) || string(policyBefore) != string(policyAfter) {
		t.Errorf("a second run changed a file")
	}
	if !strings.Contains(o.stdout, "already registered") || !strings.Contains(o.stdout, "left unchanged") {
		t.Errorf("second run stdout:\n%s", o.stdout)
	}
	if entries, _ := os.ReadDir(e.config); len(entries) != 1 {
		t.Errorf("the config directory has %d entries, want the settings file alone", len(entries))
	}
}

func setupHookLine(exe string) string {
	return setup.HookMatcher + " " + claudecode.HookCommand(exe, runtime.GOOS)
}

func TestInitLayouts(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		roots []string
	}{
		{"none", []string{"README.md", "sql/"}, []string{"pbip"}},
		{"several", []string{
			"finance/Budget.Report/definition.pbir",
			"finance/Budget.SemanticModel/definition/model.tmdl",
			"teams/a/Roster.SemanticModel/definition/model.tmdl",
			"Overview.Report/definition.pbir",
		}, []string{"Overview.Report", "finance", "teams/a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newInitRepo(t, tt.files...)
			o := runIn(t, "", "init")
			if o.code != 0 {
				t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
			}
			p, err := policy.Load(filepath.Join(e.root, policy.FileName))
			if err != nil {
				t.Fatalf("the written policy does not load: %v", err)
			}
			got := p.ProtectedRoots()
			if strings.Join(got, ",") != strings.Join(tt.roots, ",") {
				t.Errorf("roots = %q, want %q", got, tt.roots)
			}
			for _, r := range tt.roots {
				if class, _ := p.Classify(r + "/Example.Report/definition/page.json"); class != policy.Scanned {
					t.Errorf("a report file under %s is %s", r, class)
				}
			}
			if tt.name == "none" && !strings.Contains(o.stdout, "placeholder") {
				t.Errorf("stdout does not say the root is a placeholder:\n%s", o.stdout)
			}
		})
	}
}

func TestInitKeepsSettings(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	existing := `{"theme": "dark", "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "audit-tool"}]}], "Stop": []}}`
	writeFile(t, e.config, "settings.json", existing)
	now = func() time.Time { return time.Date(2100, 1, 2, 3, 4, 5, 0, time.UTC) }

	o := runIn(t, "", "init")
	if o.code != 0 {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	hooks := registeredHooks(t, e.settings)
	if len(hooks) != 2 || hooks[0] != "Bash audit-tool" || hooks[1] != setupHookLine(e.exe) {
		t.Errorf("hooks = %q", hooks)
	}
	var s map[string]any
	data, _ := os.ReadFile(e.settings)
	if err := json.Unmarshal(data, &s); err != nil || s["theme"] != "dark" {
		t.Errorf("an unrelated key changed")
	}
	backup := e.settings + ".pbipx-backup-21000102T030405.000000000Z"
	if got, err := os.ReadFile(backup); err != nil || string(got) != existing { //nolint:gosec // G304: a test's temporary file
		t.Errorf("backup = %q, %v", got, err)
	}
}

func TestInitHomeSettings(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if o := runIn(t, "", "init"); o.code != 0 {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	if got := registeredHooks(t, filepath.Join(e.home, ".claude", "settings.json")); len(got) != 1 {
		t.Errorf("hooks = %q", got)
	}
	if _, err := os.Stat(e.config); err == nil {
		t.Errorf("CLAUDE_CONFIG_DIR was written though unset")
	}
}

func TestInitWritesNothing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		exe      func(e initEnv) string
		code     int
		stdout   string
		stderr   string
		policyOK bool // the policy and the record that adopts the repository alone are written
	}{
		{"binary inside the repository", []string{"init"}, func(e initEnv) string { return filepath.Join(e.root, "bin", "pbipx") }, finding.ExitUsage, "", "inside the repository", false},
		{"binary is the repository root", []string{"init"}, func(e initEnv) string { return e.root }, finding.ExitUsage, "", "inside the repository", false},
		{"temporary build", []string{"init"}, func(initEnv) string { return filepath.Join(os.TempDir(), "go-build0", "b001", "exe", "pbipx") }, finding.ExitUsage, "", "temporary build", false},
		{"dry run", []string{"init", "--dry-run", "--gitignore"}, nil, 0, "Would register", "", false},
		{"managed dry run", []string{"init", "--managed", "--dry-run"}, nil, 0, "managed-settings.d", "", false},
		{"managed names allowManagedHooksOnly", []string{"init", "--managed", "--dry-run"}, nil, 0, `"allowManagedHooksOnly": true`, "", false},
		{"managed names file mentions", []string{"init", "--managed", "--dry-run"}, nil, 0, `"CLAUDE_CODE_DISABLE_FILE_MENTIONS": "1"`, "", false},
		{"user-level recommends managed", []string{"init", "--dry-run"}, nil, 0, "use pbipx init --managed", "", false},
		{"managed", []string{"init", "--managed"}, nil, 0, `"PreToolUse"`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
			if tt.exe != nil {
				e.useExe(t, tt.exe(e))
			}
			o := runIn(t, "", tt.args...)
			if o.code != tt.code {
				t.Fatalf("exit = %d, want %d; stderr %q", o.code, tt.code, o.stderr)
			}
			if !strings.Contains(o.stdout, tt.stdout) || !strings.Contains(o.stderr, tt.stderr) {
				t.Errorf("stdout %q, stderr %q", o.stdout, o.stderr)
			}
			w := e.written(t)
			if tt.policyOK {
				if len(w) != 2 || filepath.Base(w[0]) != policy.FileName || filepath.Base(w[1]) != "approved" {
					t.Errorf("written = %q, want the policy and the record alone", w)
				}
				return
			}
			if len(w) != 0 {
				t.Errorf("written = %q, want nothing", w)
			}
		})
	}
}

func TestInitPolicyExists(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	path := filepath.Join(e.root, policy.FileName)
	writeFile(t, e.root, policy.FileName, testPolicy)

	if o := runIn(t, "", "init"); o.code != 0 || !strings.Contains(o.stdout, "left unchanged") {
		t.Fatalf("exit = %d, stdout %q", o.code, o.stdout)
	}
	if got, _ := os.ReadFile(path); string(got) != testPolicy { //nolint:gosec // G304: a test's temporary file
		t.Errorf("the policy was overwritten without --force")
	}
	if o := runIn(t, "", "init", "--force"); o.code != 0 {
		t.Fatalf("--force: exit = %d", o.code)
	}
	if got, _ := os.ReadFile(path); string(got) == testPolicy { //nolint:gosec // G304: a test's temporary file
		t.Errorf("--force did not overwrite the policy")
	}
}

func TestInitGitignore(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	writeFile(t, e.root, ".gitignore", "*.abf\n")
	o := runIn(t, "", "init", "--gitignore")
	if o.code != 0 {
		t.Fatalf("exit = %d", o.code)
	}
	got, _ := os.ReadFile(filepath.Join(e.root, ".gitignore"))
	if !strings.HasPrefix(string(got), "*.abf\n") || !strings.Contains(string(got), "\n.pbi/\n*.pbix\n") || strings.Count(string(got), "*.abf") != 1 {
		t.Errorf(".gitignore = %q", got)
	}
	if !strings.Contains(o.stdout, "Added to .gitignore: .pbi/ *.pbix") {
		t.Errorf("stdout = %q", o.stdout)
	}
}

func TestInitRefusesSettingsInRepository(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(e.root, ".claude"))
	o := runIn(t, "", "init")
	if o.code != finding.ExitUsage || !strings.Contains(o.stderr, "inside the repository") {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	if _, err := os.Stat(filepath.Join(e.root, policy.FileName)); err == nil {
		t.Errorf("the policy was written")
	}
}

func TestInitNeedsARepository(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "config"))
	t.Chdir(dir)
	o := runIn(t, "", "init")
	if o.code != finding.ExitOperational || !strings.Contains(o.stderr, "not-a-repository") {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("something was written")
	}
}

// TestInitRefusesSymlinks checks that init writes nothing through a
// .pbipx.toml or .gitignore that is a symbolic link, which could point
// outside the repository.
func TestInitRefusesSymlinks(t *testing.T) {
	tests := []struct {
		name, file string
		args       []string
	}{
		{"policy with --force", policy.FileName, []string{"init", "--force"}},
		{"gitignore with --gitignore", ".gitignore", []string{"init", "--gitignore"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
			target := filepath.Join(filepath.Dir(e.root), "outside.txt")
			writeFile(t, filepath.Dir(target), filepath.Base(target), "outside\n")
			if err := os.Symlink(target, filepath.Join(e.root, tt.file)); err != nil {
				t.Skip("symlinks are not available")
			}
			o := runIn(t, "", tt.args...)
			if o.code != finding.ExitUsage || !strings.Contains(o.stderr, "symbolic link") {
				t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
			}
			if got, _ := os.ReadFile(target); string(got) != "outside\n" { //nolint:gosec // G304: a test's temporary file
				t.Errorf("the link's target was written: %q", got)
			}
			if _, err := os.Stat(e.settings); err == nil {
				t.Errorf("the settings were written")
			}
		})
	}

	t.Run("a dangling link", func(t *testing.T) {
		e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
		target := filepath.Join(filepath.Dir(e.root), "absent.toml")
		if err := os.Symlink(target, filepath.Join(e.root, policy.FileName)); err != nil {
			t.Skip("symlinks are not available")
		}
		if o := runIn(t, "", "init", "--force"); o.code != finding.ExitUsage {
			t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
		}
		if _, err := os.Lstat(target); err == nil {
			t.Errorf("the link's target was created")
		}
	})
}

// TestInitRefusesSettingsLinkedIntoRepository checks the settings path with
// symlinks resolved: CLAUDE_CONFIG_DIR outside the repository, but a link
// to a directory inside it.
func TestInitRefusesSettingsLinkedIntoRepository(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir", ".claude/")
	link := filepath.Join(filepath.Dir(e.root), "linked-config")
	if err := os.Symlink(filepath.Join(e.root, ".claude"), link); err != nil {
		t.Skip("symlinks are not available")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", link)
	o := runIn(t, "", "init")
	if o.code != finding.ExitUsage || !strings.Contains(o.stderr, "inside the repository") {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	if _, err := os.Stat(filepath.Join(e.root, ".claude", "settings.json")); err == nil {
		t.Errorf("settings were written into the repository")
	}
}

// TestInitAdopts checks that init records the repository as adopted, so
// the gate acts for it before the policy is approved, and goes on acting
// when the policy is deleted.
func TestInitAdopts(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	if o := runIn(t, "", "init"); o.code != 0 || !strings.Contains(o.stdout, "Recorded this repository") {
		t.Fatalf("exit = %d, stdout %q, stderr %q", o.code, o.stdout, o.stderr)
	}
	store, err := approval.Open()
	if err != nil {
		t.Fatal(err)
	}
	roots, _, err := store.Roots()
	if err != nil || len(roots) != 1 || roots[0] != e.root {
		t.Errorf("Roots = %q, %v; want the repository", roots, err)
	}
	event := readEvent(t, e.root)
	if o := runIn(t, event, "gate"); o.code != 2 || !strings.HasPrefix(o.stderr, "pbipx: policy-unapproved:") {
		t.Errorf("gate after init: exit %d, stderr %q", o.code, o.stderr)
	}
	if err := os.Remove(filepath.Join(e.root, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	if o := runIn(t, event, "gate"); o.code != 2 || !strings.HasPrefix(o.stderr, "pbipx: policy-error:") {
		t.Errorf("gate with the policy deleted: exit %d, stderr %q", o.code, o.stderr)
	}
}

// TestInitDryRunDoesNotAdopt checks that a dry run records nothing, and
// that the gate then leaves a repository with no policy alone.
func TestInitDryRunDoesNotAdopt(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	if o := runIn(t, "", "init", "--dry-run"); o.code != 0 || !strings.Contains(o.stdout, "Would record") {
		t.Fatalf("exit = %d, stdout %q", o.code, o.stdout)
	}
	if o := runIn(t, readEvent(t, e.root), "gate"); o.code != 0 {
		t.Errorf("gate in a repository init only dry-ran in: exit %d, stderr %q", o.code, o.stderr)
	}
}

// readEvent is a Claude Code Read event of README.md in root.
func readEvent(t *testing.T, root string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"cwd":             root,
		"tool_name":       "Read",
		"tool_input":      map[string]string{"file_path": "README.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
