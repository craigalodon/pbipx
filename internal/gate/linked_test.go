package gate_test

import (
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/corpus"
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
// linked work tree, at parent/wt; the main clone, at
// parent/sub/deep/repo, holds the finding. A search of parent/sub from
// parent reaches the main work tree only when it is indexed as the clone's
// main work tree: nothing on the way down to it is in the index, and the
// search's path is not inside it.
func TestAdoptionMainWorktreeFromLinked(t *testing.T) {
	isolateGit(t)
	ctl := newControlDirs(t)
	store := &approval.Store{Dir: ctl.store}
	parent := t.TempDir()
	main := filepath.Join(parent, "sub", "deep", "repo")
	write(t, main, ".pbipx.toml", defaultPolicy(t))
	for name, content := range leakyFiles() {
		write(t, main, name, content)
	}
	if err := corpus.GitSetup(main, nil, nil); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(parent, "wt")
	gitIn(t, main, "worktree", "add", "--quiet", "--detach", linked, "HEAD")
	approveIn(t, store, linked) // only the linked work tree is in the record
	decide := func(ev gate.Event) gate.Decision { return gate.Decide(ev, adoptionOptions(store)) }
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		{"grep -r of sub from the parent", gate.Event{Tool: gate.Shell, Command: "grep -r Doe sub", Cwd: parent}, gate.ReasonFinding},
		{"rg of sub from the parent", gate.Event{Tool: gate.Shell, Command: "rg Doe sub", Cwd: parent}, gate.ReasonFinding},
		{"the Grep tool on sub", gate.Event{Tool: gate.Search, Path: filepath.Join(parent, "sub"), Cwd: parent}, gate.ReasonFinding},
		{"ls of sub", gate.Event{Tool: gate.Shell, Command: "ls sub", Cwd: parent}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check(t, decide(tc.ev), tc.reason)
		})
	}
}
