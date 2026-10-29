package setup_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/setup"
)

// tree makes the named files and directories (a trailing "/") under a new
// temporary directory, each file empty.
func tree(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		p := filepath.Join(root, filepath.FromSlash(n))
		if strings.HasSuffix(n, "/") {
			if err := os.MkdirAll(p, 0o750); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		roots    []string
		topLevel bool
		skipped  int
	}{
		{"none", []string{"README.md", "sql/student.sql", "docs/"}, nil, false, 0},
		{
			"one report and model",
			[]string{"reports/Example.Report/definition/report.json", "reports/Example.SemanticModel/definition/model.tmdl", "reports/Example.pbip"},
			[]string{"reports"}, false, 0,
		},
		{
			"several",
			[]string{
				"finance/Budget.Report/definition.pbir",
				"finance/Budget.SemanticModel/",
				"finance/archive/Old.Report/",          // nested in finance
				"teams/a/Roster.SemanticModel/",        // a deeper root
				"Overview.Report/definition.pbir",      // at the repository root
				"Overview.pbip",                        // at the repository root
				"loose/Standalone.pbip",                // a project file alone
				".hidden/Secret.Report/",               // a hidden directory
				"odd[1]/Example.Report/",               // a glob character
				"Overview.Report/Inner.SemanticModel/", // inside a PBIP folder
			},
			[]string{"Overview.Report", "finance", "loose", "teams/a"}, true, 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := setup.Detect(tree(t, tt.files...))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(l.Roots, tt.roots) {
				t.Errorf("roots = %q, want %q", l.Roots, tt.roots)
			}
			if l.TopLevelPBIP != tt.topLevel {
				t.Errorf("TopLevelPBIP = %v, want %v", l.TopLevelPBIP, tt.topLevel)
			}
			if l.Skipped != tt.skipped {
				t.Errorf("Skipped = %d, want %d", l.Skipped, tt.skipped)
			}
		})
	}
}

func TestPolicyLoadsAndClassifies(t *testing.T) {
	tests := []struct {
		name   string
		layout setup.Layout
		root   string // a root to check paths under
	}{
		{"placeholder", setup.Layout{}, setup.PlaceholderRoot},
		{"one root", setup.Layout{Roots: []string{"reports"}}, "reports"},
		{"several roots", setup.Layout{Roots: []string{"Overview.Report", "finance", "teams/a"}, TopLevelPBIP: true}, "teams/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), policy.FileName)
			if err := os.WriteFile(path, setup.Policy(tt.layout), 0o600); err != nil {
				t.Fatal(err)
			}
			p, err := policy.Load(path)
			if err != nil {
				t.Fatalf("the written policy does not load: %v", err)
			}
			want := tt.layout.Roots
			if len(want) == 0 {
				want = []string{setup.PlaceholderRoot}
			}
			if got := p.ProtectedRoots(); !slices.Equal(got, want) {
				t.Errorf("roots = %q, want %q", got, want)
			}
			r := tt.root
			checks := []struct {
				path  string
				class policy.Class
				kind  policy.Kind
			}{
				{r + "/Example.Report/definition/pages/p1/page.json", policy.Scanned, policy.Report},
				{r + "/Example.Report/definition.pbir", policy.Scanned, policy.Report},
				{r + "/Example.Report/.platform", policy.Scanned, policy.Report},
				{r + "/Example.pbip", policy.Scanned, policy.Report},
				{r + "/Example.SemanticModel/definition/tables/student.tmdl", policy.Scanned, policy.Model},
				{r + "/Example.SemanticModel/.pbi/localSettings.json", policy.LocalState, ""},
				{r + "/Example.SemanticModel/.pbi/cache.abf", policy.Unvettable, ""},
				{r + "/Example.Report/StaticResources/RegisteredResources/logo.png", policy.UnscannedAllowed, ""},
				{r + "/Example.Report/notes.txt", policy.Unvettable, ""},
			}
			if tt.layout.TopLevelPBIP {
				checks = append(checks, struct {
					path  string
					class policy.Class
					kind  policy.Kind
				}{"Overview.pbip", policy.Scanned, policy.Report})
			}
			for _, c := range checks {
				class, kind := p.Classify(c.path)
				if class != c.class || kind != c.kind {
					t.Errorf("Classify(%s) = %s %s, want %s %s", c.path, class, kind, c.class, c.kind)
				}
			}
			if !p.BlocksColumn("full_name") || !p.WarnsColumn("cohort_year") {
				t.Errorf("the starting column lists are missing")
			}
		})
	}
}

var testHook = setup.Hook{Command: `'/opt/pbipx/bin/pbipx' gate --agent claude-code; s=$?; [ "$s" -eq 0 ] && exit 0; exit 2`}

// preToolUse decodes the PreToolUse entries of a settings file.
func preToolUse(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var s struct {
		Hooks struct {
			PreToolUse []map[string]any `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	return s.Hooks.PreToolUse
}

func TestMergeHookPreservesEverythingElse(t *testing.T) {
	existing := []byte(`{
  "model": "example-model",
  "permissions": {"deny": ["Read(./secrets/**)"]},
  "hooks": {
    "PostToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "fmt-tool"}]}],
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "audit-tool"}]}]
  },
  "zzz": 1.50
}`)
	m, err := setup.MergeHook(existing, testHook, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Present || m.Data == nil {
		t.Fatalf("the hook was not added")
	}
	out := string(m.Data)
	// Order and number literals are kept as written.
	order := []string{`"model"`, `"permissions"`, `"hooks"`, `"PostToolUse"`, `"PreToolUse"`, `"zzz": 1.50`}
	last := -1
	for _, k := range order {
		i := strings.Index(out, k)
		if i <= last {
			t.Fatalf("%s is missing or out of order in:\n%s", k, out)
		}
		last = i
	}
	if strings.Contains(out, `\u0026`) || strings.Contains(out, `\u003e`) {
		t.Errorf("the command is HTML-escaped")
	}
	entries := preToolUse(t, m.Data)
	if len(entries) != 2 {
		t.Fatalf("PreToolUse has %d entries, want 2", len(entries))
	}
	if entries[0]["matcher"] != "Bash" || entries[1]["matcher"] != setup.HookMatcher {
		t.Errorf("entries = %v", entries)
	}
	hooks, _ := entries[1]["hooks"].([]any)
	h, _ := hooks[0].(map[string]any)
	if h["type"] != "command" || h["command"] != testHook.Command {
		t.Errorf("hook = %v", h)
	}
	if _, ok := h["shell"]; ok {
		t.Errorf("shell is set for a POSIX hook")
	}
	var decoded map[string]any
	if err := json.Unmarshal(m.Data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["model"] != "example-model" || decoded["permissions"] == nil {
		t.Errorf("unrelated keys changed: %v", decoded)
	}

	// Idempotent: merging again changes nothing.
	again, err := setup.MergeHook(m.Data, testHook, false)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Present || again.Data != nil {
		t.Errorf("a second merge would change the file")
	}
}

func TestMergeHookShapes(t *testing.T) {
	win := setup.Hook{Command: "& 'C:/pbipx/pbipx.exe' gate --agent claude-code", Shell: "powershell"}
	tests := []struct {
		name     string
		existing string
		hook     setup.Hook
		present  bool
		others   int
	}{
		{"no file", "", testHook, false, 0},
		{"empty object", "{}", testHook, false, 0},
		{"null PreToolUse", `{"hooks": {"PreToolUse": null}}`, testHook, false, 0},
		{"windows shell", "{}", win, false, 0},
		{"same command, other matcher", `{"hooks": {"PreToolUse": [{"matcher": "Read", "hooks": [{"type": "command", "command": ` + quote(testHook.Command) + `}]}]}}`, testHook, false, 0},
		{"stale binary", `{"hooks": {"PreToolUse": [{"matcher": "Read|Grep|Bash", "hooks": [{"type": "command", "command": "'/old/pbipx' gate --agent claude-code; exit 2"}]}]}}`, testHook, false, 1},
		{"present among others", `{"hooks": {"PreToolUse": ["odd", {"matcher": ` + quote(setup.HookMatcher) + `, "hooks": [{"type": "command", "command": "x"}, {"type": "command", "command": ` + quote(testHook.Command) + `}]}]}}`, testHook, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := setup.MergeHook([]byte(tt.existing), tt.hook, false)
			if err != nil {
				t.Fatal(err)
			}
			if m.Present != tt.present || m.Others != tt.others {
				t.Errorf("Present = %v, Others = %d; want %v, %d", m.Present, m.Others, tt.present, tt.others)
			}
			if tt.present {
				return
			}
			entries := preToolUse(t, m.Data)
			h := entries[len(entries)-1]["hooks"].([]any)[0].(map[string]any)
			if h["command"] != tt.hook.Command {
				t.Errorf("the added hook is not last")
			}
			if tt.hook.Shell != "" && h["shell"] != tt.hook.Shell {
				t.Errorf("shell = %v, want %q", h["shell"], tt.hook.Shell)
			}
		})
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestMergeHookRefusesOtherShapes(t *testing.T) {
	for _, existing := range []string{
		`[]`,
		`"text"`,
		`{"hooks": "none"}`,
		`{"hooks": {"PreToolUse": {}}}`,
		`{"hooks": {}, "hooks": {}}`,
		`{"a": 1} {"b": 2}`,
		`{"a": 1`,
		`not json`,
	} {
		if _, err := setup.MergeHook([]byte(existing), testHook, false); !errors.Is(err, setup.ErrSettings) {
			t.Errorf("MergeHook(%s) error = %v, want ErrSettings", existing, err)
		}
	}
}

func TestWriteSettings(t *testing.T) {
	at := time.Date(2100, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("new file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config", "settings.json")
		backup, err := setup.WriteSettings(path, []byte("{}\n"), at)
		if err != nil {
			t.Fatal(err)
		}
		if backup != "" {
			t.Errorf("a backup of no file: %s", backup)
		}
		got, err := read(path)
		if err != nil || string(got) != "{}\n" {
			t.Fatalf("content = %q, %v", got, err)
		}
		if runtime.GOOS != "windows" {
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o600 {
				t.Errorf("mode = %v, want 0600", info.Mode().Perm())
			}
		}
	})

	t.Run("existing file through a link", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symbolic links need privileges on Windows")
		}
		dir := t.TempDir()
		target := filepath.Join(dir, "dotfiles", "settings.json")
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(`{"old": true}`), 0o400); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "settings.json")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		backup, err := setup.WriteSettings(link, []byte(`{"new": true}`), at)
		if err != nil {
			t.Fatal(err)
		}
		if want := target + ".pbipx-backup-21000102T030405Z"; backup != want {
			t.Errorf("backup = %s, want %s", backup, want)
		}
		if got, _ := read(backup); string(got) != `{"old": true}` {
			t.Errorf("backup content = %q", got)
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("the link was replaced")
		}
		if got, _ := read(target); string(got) != `{"new": true}` {
			t.Errorf("content = %q", got)
		}
		if info, _ := os.Stat(target); info.Mode().Perm() != 0o400 {
			t.Errorf("mode = %v, want 0400", info.Mode().Perm())
		}
		entries, _ := os.ReadDir(filepath.Dir(target))
		if len(entries) != 2 {
			t.Errorf("a temporary file was left: %d entries", len(entries))
		}
	})
}

func TestSnippet(t *testing.T) {
	s, err := setup.Snippet(testHook)
	if err != nil {
		t.Fatal(err)
	}
	if entries := preToolUse(t, s); len(entries) != 1 || entries[0]["matcher"] != setup.HookMatcher {
		t.Errorf("snippet = %s", s)
	}
}

func TestIgnores(t *testing.T) {
	tests := []struct {
		name     string
		existing *string
		missing  []string
	}{
		{"no file", nil, setup.IgnoreLines},
		{"all present", ptr("/.pbi\n**/*.pbix\n*.abf\n"), nil},
		{"some, no final newline", ptr("bin/\n.pbi/"), []string{"*.pbix", "*.abf"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".gitignore")
			if tt.existing != nil {
				if err := os.WriteFile(path, []byte(*tt.existing), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			missing, err := setup.MissingIgnores(root)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(missing, tt.missing) {
				t.Fatalf("missing = %q, want %q", missing, tt.missing)
			}
			if err := setup.AppendIgnores(root, missing); err != nil {
				t.Fatal(err)
			}
			if again, _ := setup.MissingIgnores(root); len(again) != 0 {
				t.Errorf("still missing after append: %q", again)
			}
			got, _ := read(path)
			if tt.existing != nil && !bytes.HasPrefix(got, []byte(*tt.existing)) {
				t.Errorf("the existing lines changed")
			}
		})
	}
}

func ptr(s string) *string { return &s }

// read reads a test's temporary file.
func read(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // G304: a test's temporary file
}
