package gate_test

import (
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestAdoptionSiblingWorktree: a work tree git worktree add made beside
// the adopted clone holds the clone's history checked out; a search from
// a directory above it, which is not in the index, reaches it.
func TestAdoptionSiblingWorktree(t *testing.T) {
	a := newAdoption(t)
	parent := t.TempDir()
	wt := filepath.Join(parent, "wt")
	gitIn(t, a.adopted, "worktree", "add", "--quiet", "--detach", wt, "HEAD")
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		{"grep -rn over the common parent", gate.Event{Tool: gate.Shell, Command: "grep -rn Doe " + bashPath(parent), Cwd: a.outside}, gate.ReasonFinding},
		{"rg over the common parent", gate.Event{Tool: gate.Shell, Command: "rg Doe " + bashPath(parent), Cwd: a.outside}, gate.ReasonFinding},
		{"grep -rn .. from inside the parent", gate.Event{Tool: gate.Shell, Command: "grep -rn Doe ..", Cwd: filepath.Join(parent, "wt", "pbip")}, gate.ReasonFinding},
		{"the Grep tool on the parent", gate.Event{Tool: gate.Search, Path: parent, Cwd: a.outside}, gate.ReasonFinding},
		{"ls of the parent", gate.Event{Tool: gate.Shell, Command: "ls " + bashPath(parent), Cwd: a.outside}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check(t, a.decide(tc.ev), tc.reason)
		})
	}
}
