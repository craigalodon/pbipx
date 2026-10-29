package setup_test

import (
	"encoding/json"
	"testing"

	"github.com/craigalodon/pbipx/internal/setup"
)

// preToolUseJSON is the PreToolUse array of a settings file, normalized.
func preToolUseJSON(t *testing.T, data []byte) string {
	t.Helper()
	var s struct {
		Hooks struct {
			PreToolUse any `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	out, err := json.Marshal(s.Hooks.PreToolUse)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// normalized is a JSON text re-encoded, for comparison.
func normalized(t *testing.T, s string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad want %s: %v", s, err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestMergeHookUpgrades(t *testing.T) {
	cmd := quote(testHook.Command)
	old := `'/old/pbipx' gate --agent claude-code; exit 2`
	oldQ := quote(old)
	cur := quote(setup.HookMatcher)
	gate := `{"type": "command", "command": ` + cmd + `}`
	other := `{"type": "command", "command": "audit-tool"}`
	tests := []struct {
		name     string
		existing string // the PreToolUse array
		force    bool
		want     string // the PreToolUse array after
		updated  bool
		others   int
		replaced int
	}{
		{
			name:     "old matcher updated in place",
			existing: `[{"matcher": "Bash", "hooks": [` + other + `]}, {"matcher": "Read|Grep|Bash", "hooks": [` + gate + `]}, {"matcher": "Edit", "hooks": [` + other + `]}]`,
			want:     `[{"matcher": "Bash", "hooks": [` + other + `]}, {"matcher": ` + cur + `, "hooks": [` + gate + `]}, {"matcher": "Edit", "hooks": [` + other + `]}]`,
			updated:  true,
		},
		{
			name:     "other keys of the entry and the hook kept",
			existing: `[{"matcher": "Read", "x": 1, "hooks": [{"type": "command", "command": ` + cmd + `, "timeout": 30}]}]`,
			want:     `[{"matcher": ` + cur + `, "x": 1, "hooks": [{"type": "command", "command": ` + cmd + `, "timeout": 30}]}]`,
			updated:  true,
		},
		{
			name:     "a stray shell removed",
			existing: `[{"matcher": ` + cur + `, "hooks": [{"type": "command", "command": ` + cmd + `, "shell": "powershell"}]}]`,
			want:     `[{"matcher": ` + cur + `, "hooks": [` + gate + `]}]`,
			updated:  true,
		},
		{
			name:     "no matcher",
			existing: `[{"hooks": [` + gate + `]}]`,
			want:     `[{"hooks": [` + gate + `], "matcher": ` + cur + `}]`,
			updated:  true,
		},
		{
			name:     "duplicates collapsed",
			existing: `[{"matcher": "Read", "hooks": [` + gate + `]}, {"matcher": "Bash", "hooks": [` + other + `]}, {"matcher": ` + cur + `, "hooks": [` + gate + `]}]`,
			want:     `[{"matcher": ` + cur + `, "hooks": [` + gate + `]}, {"matcher": "Bash", "hooks": [` + other + `]}]`,
			updated:  true,
		},
		{
			name:     "a duplicate beside another tool's hook leaves that hook",
			existing: `[{"matcher": ` + cur + `, "hooks": [` + gate + `]}, {"matcher": "Bash", "hooks": [` + other + `, ` + gate + `]}]`,
			want:     `[{"matcher": ` + cur + `, "hooks": [` + gate + `]}, {"matcher": "Bash", "hooks": [` + other + `]}]`,
			updated:  true,
		},
		{
			name:     "sharing an entry with another tool under another matcher",
			existing: `[{"matcher": "Read|Grep|Bash", "hooks": [` + other + `, ` + gate + `]}]`,
			want:     `[{"matcher": "Read|Grep|Bash", "hooks": [` + other + `]}, {"matcher": ` + cur + `, "hooks": [` + gate + `]}]`,
			updated:  true,
		},
		{
			name:     "another binary warned and left",
			existing: `[{"matcher": "Read|Grep|Bash", "hooks": [{"type": "command", "command": ` + oldQ + `}]}]`,
			want:     `[{"matcher": "Read|Grep|Bash", "hooks": [{"type": "command", "command": ` + oldQ + `}]}, {"matcher": ` + cur + `, "hooks": [` + gate + `]}]`,
			others:   1,
		},
		{
			name:     "another binary beside this one's is left",
			existing: `[{"matcher": "Read", "hooks": [` + gate + `]}, {"matcher": "Read|Grep|Bash", "hooks": [{"type": "command", "command": ` + oldQ + `}]}]`,
			want:     `[{"matcher": ` + cur + `, "hooks": [` + gate + `]}, {"matcher": "Read|Grep|Bash", "hooks": [{"type": "command", "command": ` + oldQ + `}]}]`,
			updated:  true,
			others:   1,
		},
		{
			name:     "--force replaces another binary",
			existing: `[{"matcher": "Bash", "hooks": [` + other + `]}, {"matcher": "Read|Grep|Bash", "hooks": [{"type": "command", "command": ` + oldQ + `}]}]`,
			force:    true,
			want:     `[{"matcher": "Bash", "hooks": [` + other + `]}, {"matcher": ` + cur + `, "hooks": [` + gate + `]}]`,
			updated:  true,
			replaced: 1,
		},
		{
			name:     "--force keeps this binary's and drops the other",
			existing: `[{"matcher": "Read|Grep|Bash", "hooks": [{"type": "command", "command": ` + oldQ + `}]}, {"matcher": ` + cur + `, "hooks": [` + gate + `]}]`,
			force:    true,
			want:     `[{"matcher": ` + cur + `, "hooks": [` + gate + `]}]`,
			updated:  true,
			replaced: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := `{"model": "example-model", "hooks": {"PreToolUse": ` + tt.existing + `}}`
			m, err := setup.MergeHook([]byte(existing), testHook, tt.force)
			if err != nil {
				t.Fatal(err)
			}
			if m.Present || m.Updated != tt.updated || m.Others != tt.others || m.Replaced != tt.replaced {
				t.Errorf("Merge = present %v, updated %v, others %d, replaced %d; want updated %v, others %d, replaced %d",
					m.Present, m.Updated, m.Others, m.Replaced, tt.updated, tt.others, tt.replaced)
			}
			if got, want := preToolUseJSON(t, m.Data), normalized(t, tt.want); got != want {
				t.Errorf("PreToolUse =\n%s\nwant\n%s", got, want)
			}
			var top map[string]any
			if err := json.Unmarshal(m.Data, &top); err != nil || top["model"] != "example-model" {
				t.Errorf("other settings changed")
			}

			// Idempotent: a second run changes nothing.
			again, err := setup.MergeHook(m.Data, testHook, tt.force)
			if err != nil {
				t.Fatal(err)
			}
			if !again.Present || again.Data != nil {
				t.Errorf("a second run would change the file: %+v", again)
			}
		})
	}
}
