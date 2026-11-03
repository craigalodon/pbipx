package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellCommitVerboseTwice: git commit with a verbose count of 2 or more
// (-vv, -v -v, --verbose --verbose, clustered with -m or -F, or
// commit.verbose 2) prints the working-tree diff when it fails with
// nothing staged, as git status -vv does. With one -v the rule is the
// one it was.
func TestShellCommitVerboseTwice(t *testing.T) {
	// The finding is in the working tree, unstaged: the committed slicer is
	// clean.
	files := cleanFiles()
	files[leakVisual] = slicer("enrollment", "cohort_year", "2150")
	leaky := newRepo(t, files)
	write(t, leaky, leakVisual, slicer("student", "full_name", "Doe, Jane"))
	clean := newRepo(t, files)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	twice := []string{
		`git commit -vv`,
		`git commit -vv -m x`,
		`git commit -v -v`,
		`git commit -v -v -m x`,
		`git commit --verbose --verbose`,
		`git commit --verbose -v -m x`,
		`git commit -vvm x`,
		`git commit -vm x -v`,
		`git commit -m x -vv`,
		`git commit -vvF msg.txt`,
		`git commit -avv -m x`,
		`git commit --amend -vv --no-edit`,
		`git commit -vvv`,
	}
	for _, l := range twice {
		run(t, leaky, []decideCase{{"leaky: " + l, sh(l), git}})
		run(t, clean, []decideCase{{"clean: " + l, sh(l), ""}})
	}
	run(t, leaky, []decideCase{
		{"one -v", sh(`git commit -v -m x`), ""},
		{"one --verbose", sh(`git commit --verbose -m x`), ""},
		{"-vv then --no-verbose", sh(`git commit -vv --no-verbose -m x`), ""},
		{"-vv with a short format", sh(`git commit --dry-run --short -vv`), ""},
		{"no -v", sh(`git commit -m x`), ""},
	})
	gitIn(t, leaky, "config", "commit.verbose", "2")
	run(t, leaky, []decideCase{
		{"commit.verbose 2", sh(`git commit -m x`), git},
		{"commit.verbose 2, --no-verbose", sh(`git commit --no-verbose -m x`), ""},
	})
	gitIn(t, leaky, "config", "commit.verbose", "1")
	run(t, leaky, []decideCase{{"commit.verbose 1", sh(`git commit -m x`), ""}})
}
