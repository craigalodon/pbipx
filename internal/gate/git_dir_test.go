package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellGitDashC: git -C DIR names the directory git runs in. A
// directory that is the working directory, or inside the same repository,
// is judged as the pathspecs relative to it would be without -C.
func TestShellGitDashC(t *testing.T) {
	root, _ := historyRepo(t)
	write(t, root, "src/main.go", "package main\n")
	gitIn(t, root, "add", "src/main.go")
	gitIn(t, root, "commit", "--quiet", "--message", "Add src")
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"log -p of src", sh("git log -p -- src"), ""},
		{"-C . log -p of src", sh("git -C . log -p -- src"), ""},
		{"-C ./ log -p of src", sh("git -C ./ log -p -- src"), ""},
		{"-C src log -p of .", sh("git -C src log -p -- ."), ""},
		{"-C src log -p of a file", sh("git -C src log -p -- main.go"), ""},
		{"-C . log -p of a root", sh("git -C . log -p -- pbip"), git},
		{"-C . log -p with no path", sh("git -C . log -p"), git},
		{"-C pbip log -p of .", sh("git -C pbip log -p -- ."), git},
		{"-C src/.. log -p of .", sh("git -C src/.. log -p -- ."), git},
		{"-C src log -p of ..", sh("git -C src log -p -- .."), git},
		{"-C src log -p of ../pbip", sh("git -C src log -p -- ../pbip"), git},
		{"-C . -C src log -p of .", sh("git -C . -C src log -p -- ."), ""},
		{"-C with an unknown directory", sh(`git -C "$D" log -p -- src`), git},
		{"-C a directory that is no work tree", sh("git -C /tmp log -p -- src"), git},
	})
}
