package gate_test

import (
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
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

// TestAdoptionMainWorktreeFromLinked: the approval record roots only a
// linked work tree; the main work tree, which holds the finding, is
// reached from it, so a search over the main tree's parent denies.
func TestAdoptionMainWorktreeFromLinked(t *testing.T) {
	ctl := newControlDirs(t)
	store := &approval.Store{Dir: ctl.store}
	main := newRepo(t, leakyFiles())
	linked := filepath.Join(t.TempDir(), "wt")
	gitIn(t, main, "worktree", "add", "--quiet", "--detach", linked, "HEAD")
	approveIn(t, store, linked) // only the linked work tree is in the record
	decide := func(ev gate.Event) gate.Decision { return gate.Decide(ev, adoptionOptions(store)) }
	outside := t.TempDir()
	parent := filepath.Dir(main)
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		// A container path above main: main is reached only when it is
		// indexed as the record's main work tree.
		{"grep -r over the main tree's parent", gate.Event{Tool: gate.Shell, Command: "grep -r Doe " + bashPath(parent), Cwd: outside}, gate.ReasonFinding},
		{"the Grep tool on the main tree's parent", gate.Event{Tool: gate.Search, Path: parent, Cwd: outside}, gate.ReasonFinding},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check(t, decide(tc.ev), tc.reason)
		})
	}
}
