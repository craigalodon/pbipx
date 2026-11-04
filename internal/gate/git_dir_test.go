package gate_test

import (
	"os"
	"path/filepath"
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
		// The repository's parent is no work tree, and holds the repository.
		{"-C a directory above the repository", sh("git -C .. log -p -- src"), git},
	})
}

// TestShellGitDashCApartFromTheRepository: git -C a directory outside every
// repository, and not above this one, runs nowhere near the protected
// roots. (The test once used /tmp for it, which holds the repository only
// where the temporary directory is under /tmp: not on macOS.) Reached
// through a symlink, as macOS reaches /var, the repository's parent is
// still found above it.
func TestShellGitDashCApartFromTheRepository(t *testing.T) {
	root, _ := historyRepo(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip("no symlinks:", err)
	}
	apart := filepath.ToSlash(t.TempDir())
	above := filepath.ToSlash(filepath.Dir(root))
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	for _, from := range []string{root, alias} {
		run(t, from, []decideCase{
			{"-C a directory apart from the repository", sh("git -C '" + apart + "' log -p -- src"), ""},
			{"-C the repository's parent, spelled absolutely", sh("git -C '" + above + "' log -p -- src"), gate.ReasonGitContent},
		})
	}
}
