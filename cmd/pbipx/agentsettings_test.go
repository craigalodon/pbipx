package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
)

// disabling is a Claude Code settings file that turns hooks off, with a
// planted value beside it.
const disabling = `{"disableAllHooks": true, "env": {"NOTE": "` + sentinel + `"}}`

// newSettingsRepo makes a committed repository with the test policy and
// files, none of them in the policy's scan scope, and changes into it.
// staged files are added to the index and not committed.
func newSettingsRepo(t *testing.T, files map[string]string, staged ...string) string {
	t.Helper()
	isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "repo")
	writeFile(t, root, policy.FileName, testPolicy)
	for name, content := range files {
		writeFile(t, root, name, content)
	}
	if err := corpus.GitSetup(root, nil, staged); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

func TestScanAgentSettings(t *testing.T) {
	tests := []struct {
		name   string
		files  map[string]string
		staged []string
		args   []string
		code   int
		path   string // the file the error names; "" for clean
	}{
		{"all, at the root", map[string]string{".claude/settings.json": disabling}, nil, []string{"scan"}, finding.ExitOperational, ".claude/settings.json"},
		{"all, local settings in a subdirectory", map[string]string{"tools/.claude/settings.local.json": disabling}, nil, []string{"scan", "--all"}, finding.ExitOperational, "tools/.claude/settings.local.json"},
		{"all, unparseable", map[string]string{".claude/settings.json": "{" + sentinel}, nil, []string{"scan"}, finding.ExitOperational, ".claude/settings.json"},
		{"staged", map[string]string{".claude/settings.json": disabling}, []string{".claude/settings.json"}, []string{"scan", "--staged"}, finding.ExitOperational, ".claude/settings.json"},
		{"rev", map[string]string{".claude/settings.json": disabling}, nil, []string{"scan", "--rev", "HEAD"}, finding.ExitOperational, ".claude/settings.json"},
		{"a path", map[string]string{".claude/settings.json": disabling}, nil, []string{"scan", ".claude/settings.json"}, finding.ExitOperational, ".claude/settings.json"},
		{"a directory", map[string]string{"tools/.claude/settings.json": disabling}, nil, []string{"scan", "tools"}, finding.ExitOperational, "tools/.claude/settings.json"},

		{"hooks left on", map[string]string{".claude/settings.json": `{"disableAllHooks": false}`}, nil, []string{"scan"}, finding.ExitClean, ""},
		{"no hooks setting", map[string]string{".claude/settings.json": `{"env": {"NOTE": "` + sentinel + `"}}`}, nil, []string{"scan"}, finding.ExitClean, ""},
		{"another file in .claude", map[string]string{".claude/other.json": disabling}, nil, []string{"scan"}, finding.ExitClean, ""},
		{"a path away from it", map[string]string{".claude/settings.json": disabling, "notes/a.md": "nothing\n"}, nil, []string{"scan", "notes"}, finding.ExitClean, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newSettingsRepo(t, tt.files, tt.staged...)
			for _, format := range []string{"human", "json"} {
				o := runIn(t, "", append(tt.args, "--format", format)...)
				if o.code != tt.code {
					t.Fatalf("%s: exit = %d, want %d; stdout %q stderr %q", format, o.code, tt.code, o.stdout, o.stderr)
				}
				if v := o.leaks(); v != "" {
					t.Errorf("%s: the output carries %q", format, v)
				}
				if strings.Contains(o.stdout+o.stderr, "disableAllHooks\": true") {
					t.Errorf("%s: the output quotes the file", format)
				}
				switch {
				case tt.path == "" && strings.Contains(o.stdout, string(finding.HooksDisabled)):
					t.Errorf("%s: a clean file is reported: %s", format, o.stdout)
				case tt.path != "" && format == "human" && !strings.Contains(o.stdout, "error  "+tt.path+"\n  reasons      hooks-disabled\n"):
					t.Errorf("human output lacks the error: %s", o.stdout)
				case tt.path != "" && format == "json":
					findings, _ := jsonResult(t, o)["findings"].([]any)
					if len(findings) != 1 {
						t.Fatalf("findings = %v, want one", findings)
					}
					f := findings[0].(map[string]any)
					if f["path"] != tt.path || f["tier"] != "error" || f["reasons"].([]any)[0] != "hooks-disabled" {
						t.Errorf("finding = %v", f)
					}
				}
			}
		})
	}
}
