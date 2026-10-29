package scan

import (
	"slices"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
)

func TestIsAgentSettings(t *testing.T) {
	for path, want := range map[string]bool{
		".claude/settings.json":            true,
		".claude/settings.local.json":      true,
		"a/b/.claude/settings.json":        true,
		"pbip/.claude/settings.local.json": true,
		".Claude/Settings.JSON":            true,
		"settings.json":                    false,
		".claude/settings.json.bak":        false,
		".claude/commands/settings.json":   false,
		"x.claude/settings.json":           false,
		".claude":                          false,
		"":                                 false,
	} {
		if got := IsAgentSettings(path); got != want {
			t.Errorf("IsAgentSettings(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestHooksDisabled(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		want          bool
	}{
		{"empty object", "{}", false},
		{"other settings", `{"permissions": {"deny": ["Read(./x)"]}}`, false},
		{"false", `{"disableAllHooks": false}`, false},
		{"false with a byte order mark", "\xef\xbb\xbf{\"disableAllHooks\": false}", false},
		{"true", `{"disableAllHooks": true}`, true},
		{"true after false", `{"disableAllHooks": false, "disableAllHooks": true}`, true},
		{"a string", `{"disableAllHooks": "true"}`, true},
		{"null", `{"disableAllHooks": null}`, true},
		{"not JSON", `{"disableAllHooks": tru`, true},
		{"empty file", "", true},
		{"an array", `[]`, true},
		{"null document", `null`, true},
		{"env, harmless", `{"env": {"NOTE": "x", "PBIPX_NO_REVEAL": "1"}}`, false},
		{"env sets PBIPX_CONFIG_DIR", `{"env": {"PBIPX_CONFIG_DIR": "/tmp/x"}}`, true},
		{"env sets CLAUDE_CONFIG_DIR", `{"env": {"CLAUDE_CONFIG_DIR": "/tmp/x"}}`, true},
		{"env sets PATH", `{"env": {"PATH": "/tmp/x"}}`, true},
		{"env sets BASH_ENV", `{"env": {"BASH_ENV": "/tmp/x"}}`, true},
		{"env sets ENV", `{"env": {"ENV": "/tmp/x"}}`, true},
		{"env sets SHELLOPTS", `{"env": {"SHELLOPTS": "errexit"}}`, true},
		{"env sets one in another case", `{"env": {"Path": "/tmp/x"}}`, true},
		{"env sets one to empty", `{"env": {"BASH_ENV": ""}}`, true},
		{"env with disableAllHooks false", `{"disableAllHooks": false, "env": {"SHELLOPTS": "errexit"}}`, true},
		{"env not an object", `{"env": ["PATH=/tmp/x"]}`, true},
		{"env null", `{"env": null}`, true},
		{"env empty", `{"env": {}}`, false},
	} {
		if got := HooksDisabled([]byte(tc.content)); got != tc.want {
			t.Errorf("%s: HooksDisabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRunAgentSettings(t *testing.T) {
	p := loadPolicy(t, testPolicy)
	in := files{
		".claude/settings.json":             `{"disableAllHooks": true, "note": "Doe, Jane"}`,
		"tools/.claude/settings.local.json": `{"disableAllHooks": false}`,
		"pbip/.claude/settings.json":        `not json`,
		"notes/.claude/settings.local.json": `{}`,
	}
	paths := append(in.paths(), "missing/.claude/settings.json")

	// Off: the policy alone decides, and none of these is in scan scope
	// except the report file under the root.
	off := Run(paths, in, Options{Policy: p})
	if got := summarize(off.Findings); !slices.Equal(got, []summary{
		{"pbip/.claude/settings.json", finding.Error, "unparseable-json"},
	}) {
		t.Errorf("without AgentSettings: %v", got)
	}

	on := Run(paths, in, Options{Policy: p, AgentSettings: true})
	want := []summary{
		{".claude/settings.json", finding.Error, "hooks-disabled"},
		{"missing/.claude/settings.json", finding.Error, "unreadable"},
		{"pbip/.claude/settings.json", finding.Error, "hooks-disabled"},
		{"pbip/.claude/settings.json", finding.Error, "unparseable-json"},
	}
	if got := summarize(on.Findings); !slices.Equal(got, want) {
		t.Errorf("with AgentSettings: %v, want %v", got, want)
	}
	for _, f := range on.Findings {
		if f.Reasons[0] == finding.UnparseableJSON {
			continue // the report scanner's own finding
		}
		if f.Locator != "" || f.Context != "" || f.Column != nil {
			t.Errorf("%s: an agent-settings finding carries more than its path and reason", f.Path)
		}
	}
}
