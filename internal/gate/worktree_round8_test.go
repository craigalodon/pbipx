package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellWorktreeAfterWriteRound8: the working-tree prints F1 missed,
// pathspecs that keep a print out of every root, git commands that write
// no files (they run no hooks and change no tree), and a tree rewritten
// from history before a writer.
func TestShellWorktreeAfterWriteRound8(t *testing.T) {
	files := leakyFiles()
	files["src/main.go"] = "package main\n"
	leaky := newRepo(t, files)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	finding, git := gate.ReasonFinding, gate.ReasonGitContent
	run(t, leaky, []decideCase{
		{"make, then diff-files -p", sh("make fmt && git diff-files -p"), finding},
		{"make, then diff-files --patch", sh("make fmt && git diff-files --patch"), finding},
		{"make, then diff-index -p HEAD", sh("make fmt && git diff-index -p HEAD"), finding},
		{"make, then diff-index -p --cached HEAD", sh("make fmt && git diff-index -p --cached HEAD"), ""},
		{"make, then diff of src", sh("make fmt && git diff -- src"), ""},
		{"gofmt, then diff of src", sh("gofmt -w src && git diff -- src"), ""},
		{"sed -i, then diff of a file with no --", sh("sed -i s/a/b/ src/main.go && git diff src/main.go"), finding},
		{"sed -i, then diff -- of a file", sh("sed -i s/a/b/ src/main.go && git diff -- src/main.go"), ""},
		{"restore --staged, then diff of src", sh("git restore --staged src/main.go && git diff -- src"), ""},
		{"make, then diff of a root", sh("make fmt && git diff -- pbip"), finding},
		{"make, then diff of a glob", sh("make fmt && git diff -- '*.json'"), finding},
		{"add, then diff", sh("git add src && git diff"), ""},
		{"stash, then diff", sh("git stash && git diff"), ""},
		{"fetch and tag, then diff", sh("git fetch && git tag -f t && git diff"), ""},
		{"commit, then diff", sh("git commit -m x && git diff"), git},
		{"checkout of a branch, then diff", sh("git checkout main && git diff"), git},
		{"an input redirection from .git/config, then status", sh("cat < .git/config; git status"), ""},
		{"read from .git/HEAD, then log", sh("read -r x < .git/HEAD; git log --oneline -1"), ""},
		{"a write into .git/config, then status", sh("echo x >> .git/config; git status"), git},
	})

	// HEAD holds the finding; its fix is staged, so git diff prints
	// nothing. A stash puts HEAD's content back in the tree, and a writer
	// after it makes git diff print it.
	fixed := newRepo(t, leakyFiles())
	write(t, fixed, leakVisual, slicer("enrollment", "cohort_year", "2150"))
	gitIn(t, fixed, "add", "--all")
	run(t, fixed, []decideCase{
		{"stash, make, diff", sh("git stash && make fmt && git diff"), git},
		{"stash -u, npm run, diff", sh("git stash -u && npm run format && git diff"), git},
		{"stash; make; diff", sh("git stash; make fmt; git diff"), git},
		{"stash push of a root, make, diff", sh("git stash push -- pbip && make fmt && git diff"), git},
		{"checkout of paths, make, diff", sh("git checkout HEAD -- pbip && make fmt && git diff"), git},
		{"stash, then diff", sh("git stash && git diff"), ""},
		{"make, then diff", sh("make fmt && git diff"), ""},
	})
}
