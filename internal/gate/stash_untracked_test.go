package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellStashShowUntrackedConfig: -u and --include-untracked are no
// output format, so with stash.showPatch true git still prints a patch for
// them; with a summary format given the config is not read.
func TestShellStashShowUntrackedConfig(t *testing.T) {
	root := newRepo(t, cleanFiles())
	write(t, root, leakVisual, slicer("student", "full_name", "Doe, Jane"))
	gitIn(t, root, "add", "--all")
	gitIn(t, root, "stash", "--quiet")
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	gitIn(t, root, "config", "stash.showPatch", "true")
	run(t, root, []decideCase{
		{"showPatch true, no option", sh("git stash show"), git},
		{"showPatch true, -u", sh("git stash show -u"), git},
		{"showPatch true, --include-untracked", sh("git stash show --include-untracked"), git},
		{"showPatch true, --only-untracked (the stash holds none)", sh("git stash show --only-untracked"), ""},
		{"showPatch true, -u with a stash", sh("git stash show -u stash@{0}"), git},
		{"showPatch true, --stat -u", sh("git stash show --stat -u"), ""},
		{"showPatch true, --name-only", sh("git stash show --name-only"), ""},
	})
	gitIn(t, root, "config", "stash.showPatch", "false")
	run(t, root, []decideCase{
		{"showPatch false, -u", sh("git stash show -u"), ""},
		{"showPatch false, --include-untracked stash@{0}", sh("git stash show --include-untracked stash@{0}"), ""},
		{"showPatch false, -p", sh("git stash show -p"), git},
	})
}
