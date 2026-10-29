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
			mergeAndCheck(t, existing, testHook, tt.force, tt.want, tt.updated, tt.others, tt.replaced)
		})
	}
}

// TestMergeHookUpgradesCommandForm checks that a hook in an earlier form of
// the command for the same binary, without the leading "set +e; " and
// without a timeout, is rewritten in place, not counted as another binary.
func TestMergeHookUpgradesCommandForm(t *testing.T) {
	tail := `gate --agent claude-code; s=$?; [ "$s" -eq 0 ] && exit 0; exit 2`
	oldForm := `'/opt/pbipx/bin/pbipx' ` + tail
	h := setup.Hook{Command: `set +e; '/opt/pbipx/bin/pbipx' ` + tail, Timeout: 300}
	cur := quote(setup.HookMatcher)
	tests := []struct{ name, existing, want string }{
		{
			"earlier command form",
			`[{"matcher": "Read|Grep|Bash", "hooks": [{"type": "command", "command": ` + quote(oldForm) + `}]}]`,
			`[{"matcher": ` + cur + `, "hooks": [{"type": "command", "command": ` + quote(h.Command) + `, "timeout": 300}]}]`,
		},
		{
			"current form, another timeout",
			`[{"matcher": ` + cur + `, "hooks": [{"type": "command", "command": ` + quote(h.Command) + `, "timeout": 5}]}]`,
			`[{"matcher": ` + cur + `, "hooks": [{"type": "command", "command": ` + quote(h.Command) + `, "timeout": 300}]}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := `{"model": "example-model", "hooks": {"PreToolUse": ` + tt.existing + `}}`
			mergeAndCheck(t, existing, h, false, tt.want, true, 0, 0)
		})
	}
	t.Run("new registration has the timeout", func(t *testing.T) {
		mergeAndCheck(t, `{"model": "example-model"}`, h, false,
			`[{"matcher": `+cur+`, "hooks": [{"type": "command", "command": `+quote(h.Command)+`, "timeout": 300}]}]`, false, 0, 0)
	})
}

// mergeAndCheck merges h into existing and checks the result, and that a
// second merge changes nothing.
func mergeAndCheck(t *testing.T, existing string, h setup.Hook, force bool, wantPre string, updated bool, others, replaced int) {
	t.Helper()
	m, err := setup.MergeHook([]byte(existing), h, force)
	if err != nil {
		t.Fatal(err)
	}
	if m.Present || m.Updated != updated || m.Others != others || m.Replaced != replaced {
		t.Errorf("Merge = present %v, updated %v, others %d, replaced %d; want updated %v, others %d, replaced %d",
			m.Present, m.Updated, m.Others, m.Replaced, updated, others, replaced)
	}
	if got, want := preToolUseJSON(t, m.Data), normalized(t, wantPre); got != want {
		t.Errorf("PreToolUse =\n%s\nwant\n%s", got, want)
	}
	var top map[string]any
	if err := json.Unmarshal(m.Data, &top); err != nil || top["model"] != "example-model" {
		t.Errorf("other settings changed")
	}

	// Idempotent: a second run changes nothing.
	again, err := setup.MergeHook(m.Data, h, force)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Present || again.Data != nil {
		t.Errorf("a second run would change the file: %+v", again)
	}
}
