package gate_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

func TestPowerShell(t *testing.T) {
	ps := func(cmd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd} }
	t.Run("leaking tree", func(t *testing.T) {
		root := newRepo(t, leakyFiles())
		run(t, root, []decideCase{
			{"any command scans the tree", ps("Get-ChildItem"), gate.ReasonFinding},
			{"a read away from the roots still scans", ps("Get-Content notes\\clean.md"), gate.ReasonFinding},
			{"from a directory away from the roots", gate.Event{Tool: gate.PowerShell, Command: "Get-Date", Cwd: "notes"}, gate.ReasonFinding},
		})
	})
	t.Run("clean tree", func(t *testing.T) {
		root := newRepo(t, cleanFiles())
		run(t, root, []decideCase{
			{"a listing", ps("Get-ChildItem -Recurse pbip"), ""},
			{"a read of a clean note", ps("Get-Content notes/clean.md"), ""},
			{"names a settings file", ps("Get-Content .claude\\settings.json"), gate.ReasonAgentSettings},
			{"names a settings file, slashes", ps("Set-Content -Path:.claude/settings.local.json -Value x"), gate.ReasonAgentSettings},
			{"names a settings file, quoted", ps(`Copy-Item x '.claude/settings.json'`), gate.ReasonAgentSettings},
			{"names a .claude directory", ps("Copy-Item x .claude"), gate.ReasonAgentSettings},
			{"names local state", ps("Get-Content " + strings.ReplaceAll(localState, "/", `\`)), gate.ReasonLocalState},
			{"runs policy approve", ps("pbipx policy approve"), gate.ReasonAgentSettings},
			{"policy approve quoted in one string", ps("Write-Output 'pbipx policy approve'"), ""},
		})
	})
}

func TestPowerShellAdoption(t *testing.T) {
	a := newAdoption(t)
	ps := func(cmd, cwd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd, Cwd: cwd} }
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		{"not adopted", ps("Get-ChildItem -Recurse", a.plain), ""},
		{"not adopted, read of a leak", ps("Get-Content "+leakVisual, a.plain), ""},
		{"outside every repository", ps("Get-ChildItem", a.outside), ""},
		{"adopted, leaking tree", ps("Get-ChildItem", a.adopted), gate.ReasonFinding},
		{"outside, a path into the adopted repository", ps("Get-Content '"+filepath.Join(a.adopted, "README.md")+"'", a.outside), gate.ReasonFinding},
		{"outside, a directory holding it", ps("Get-ChildItem -Recurse "+filepath.Dir(a.adopted), a.outside), gate.ReasonFinding},
		{"outside, a path that cannot be placed", ps(`Get-Content $env:USERPROFILE\x`, a.outside), gate.ReasonFinding},
		{"outside, names a user settings file", ps("Get-Content "+filepath.Join(a.ctl.claude, "settings.json"), a.outside), gate.ReasonAgentSettings},
		{"outside, names the approval store", ps("Get-ChildItem "+a.ctl.store, a.outside), gate.ReasonAgentSettings},
		{"outside, policy approve", ps("pbipx policy approve", a.outside), gate.ReasonAgentSettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check(t, a.decide(tc.ev), tc.reason)
		})
	}
}

func TestLSP(t *testing.T) {
	lsp := func(paths ...string) gate.Event { return gate.Event{Tool: gate.LSP, Paths: paths} }
	t.Run("leaking tree", func(t *testing.T) {
		root := newRepo(t, leakyFiles())
		run(t, root, []decideCase{
			{"a leaking file", lsp(leakVisual), gate.ReasonFinding},
			{"a clean note", lsp(cleanNote), ""},
			{"a clean note, then a leak", lsp(cleanNote, leakVisual), gate.ReasonFinding},
			{"local state", lsp(localState), gate.ReasonLocalState},
			{"a protected directory", lsp("pbip"), gate.ReasonFinding},
			{"no path scans the tree", lsp(), gate.ReasonFinding},
		})
	})
	t.Run("clean tree", func(t *testing.T) {
		root := newRepo(t, cleanFiles())
		run(t, root, []decideCase{
			{"no path", lsp(), ""},
			{"a report file", lsp(reportJSON), ""},
		})
	})
	t.Run("adoption", func(t *testing.T) {
		a := newAdoption(t)
		check(t, a.decide(gate.Event{Tool: gate.LSP, Cwd: a.plain}), "")
		check(t, a.decide(gate.Event{Tool: gate.LSP, Paths: []string{filepath.Join(a.adopted, leakVisual)}, Cwd: a.outside}), gate.ReasonFinding)
		check(t, a.decide(gate.Event{Tool: gate.LSP, Cwd: a.adopted}), gate.ReasonFinding)
	})
}
