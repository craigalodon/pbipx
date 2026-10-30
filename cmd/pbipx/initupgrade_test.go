package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate/claudecode"
)

// settingsWith is a settings file whose PreToolUse holds another tool's
// hook and then the given entries, as matcher and command pairs.
func settingsWith(t *testing.T, pairs ...string) string {
	t.Helper()
	entries := []any{map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "audit-tool"}}}}
	for i := 0; i+1 < len(pairs); i += 2 {
		entries = append(entries, map[string]any{"matcher": pairs[i], "hooks": []any{map[string]any{"type": "command", "command": pairs[i+1]}}})
	}
	data, err := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": entries}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInitUpgradesTheHook(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	ours := claudecode.HookCommand(e.exe, runtime.GOOS)
	writeFile(t, e.config, "settings.json", settingsWith(t, "Read|Grep|Bash", ours, "Read", ours))

	o := runIn(t, "", "init")
	if o.code != 0 {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	if !strings.Contains(o.stdout, "Updated the gate's hook") || strings.Contains(o.stdout, "Registered the gate's hook") {
		t.Errorf("stdout:\n%s", o.stdout)
	}
	want := []string{"Bash audit-tool", setupHookLine(e.exe)}
	if got := registeredHooks(t, e.settings); !slices.Equal(got, want) {
		t.Errorf("hooks = %q, want %q", got, want)
	}
	if entries, _ := os.ReadDir(e.config); len(entries) != 2 {
		t.Errorf("the config directory has %d entries, want the settings and a backup", len(entries))
	}

	// Idempotent.
	before, _ := os.ReadFile(e.settings)
	o = runIn(t, "", "init")
	after, _ := os.ReadFile(e.settings)
	if o.code != 0 || !strings.Contains(o.stdout, "already registered") || string(before) != string(after) {
		t.Errorf("second run: exit %d, changed %v, stdout:\n%s", o.code, string(before) != string(after), o.stdout)
	}
}

// TestInitWidensTheMatcher re-runs init over the previous release's
// registration, which did not name PowerShell or LSP.
func TestInitWidensTheMatcher(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	ours := claudecode.HookCommand(e.exe, runtime.GOOS)
	writeFile(t, e.config, "settings.json", settingsWith(t, "Read|Grep|Bash|Write|Edit|MultiEdit|NotebookEdit", ours))

	o := runIn(t, "", "init")
	if o.code != 0 || !strings.Contains(o.stdout, "Updated the gate's hook") {
		t.Fatalf("exit = %d, stdout:\n%s", o.code, o.stdout)
	}
	want := []string{"Bash audit-tool", setupHookLine(e.exe)}
	got := registeredHooks(t, e.settings)
	if !slices.Equal(got, want) {
		t.Errorf("hooks = %q, want %q", got, want)
	}
	if len(got) == 2 && (!strings.Contains(got[1], "|PowerShell|") || !strings.Contains(got[1], "|LSP|")) {
		t.Errorf("the matcher was not widened: %q", got[1])
	}
}

func TestInitAnotherBinary(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	theirs := claudecode.HookCommand(filepath.Join(filepath.Dir(e.exe), "old", "pbipx"), runtime.GOOS)
	writeFile(t, e.config, "settings.json", settingsWith(t, "Read|Grep|Bash", theirs))

	o := runIn(t, "", "init")
	if o.code != 0 || !strings.Contains(o.stdout, "Warning:") || !strings.Contains(o.stdout, "--force") {
		t.Fatalf("exit = %d, stdout:\n%s", o.code, o.stdout)
	}
	want := []string{"Bash audit-tool", "Read|Grep|Bash " + theirs, setupHookLine(e.exe)}
	if got := registeredHooks(t, e.settings); !slices.Equal(got, want) {
		t.Errorf("without --force: hooks = %q, want %q", got, want)
	}

	o = runIn(t, "", "init", "--force")
	if o.code != 0 || !strings.Contains(o.stdout, "Updated the gate's hook") || strings.Contains(o.stdout, "Warning:") {
		t.Fatalf("--force: exit = %d, stdout:\n%s", o.code, o.stdout)
	}
	want = []string{"Bash audit-tool", setupHookLine(e.exe)}
	if got := registeredHooks(t, e.settings); !slices.Equal(got, want) {
		t.Errorf("with --force: hooks = %q, want %q", got, want)
	}
}
