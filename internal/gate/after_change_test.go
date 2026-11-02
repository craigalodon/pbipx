package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellGitAfterChangeHeadIsCommit: after a change, HEAD and the
// revisions relative to it are still commits, unless the line can point
// HEAD at something else (symbolic-ref, update-ref, replace, a subcommand
// the gate does not know): a summary of them prints no content (O3).
func TestShellGitAfterChangeHeadIsCommit(t *testing.T) {
	root := newRepo(t, cleanFiles())
	gitIn(t, root, "commit", "--quiet", "--allow-empty", "--message", "Two")
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"commit, then show --stat HEAD", sh(`git add -A && git commit -m "x" && git show --stat HEAD`), ""},
		{"commit, then show -s HEAD", sh(`git commit -m x && git show -s HEAD`), ""},
		{"commit, then show --name-only HEAD", sh(`git commit -m x && git show --name-only HEAD`), ""},
		{"commit, then show --name-status @~1", sh(`git commit -m x && git show --name-status @~1`), ""},
		{"commit, then show --stat of a range", sh(`git commit -m x && git show --stat HEAD^..HEAD`), ""},
		{"commit, then show HEAD", sh(`git commit -m x && git show HEAD`), git},
		{"update-ref, then show --stat HEAD", sh(`git update-ref HEAD HEAD && git show --stat HEAD`), git},
		{"symbolic-ref, then show --stat HEAD", sh(`git symbolic-ref HEAD refs/tags/t && git show --stat HEAD`), git},
		{"replace, then show --stat HEAD", sh(`git replace HEAD HEAD && git show --stat HEAD`), git},
		{"an alias, then show --stat HEAD", sh(`git frob && git show --stat HEAD`), git},
		{"commit, then show --stat of a tag", sh(`git commit -m x && git show --stat t`), git},
	})
}

// TestShellGitAfterChangeSpecs: after a change, a read whose pathspecs
// provably avoid every root is allowed; with none, only the working-tree
// diff after changes that can only shrink it (O7).
func TestShellGitAfterChangeSpecs(t *testing.T) {
	root := newRepo(t, cleanFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"add, then diff --cached of a path outside the roots", sh("git add -A && git diff --cached -- README.md"), ""},
		{"fetch, then log -p of a path outside the roots", sh("git fetch && git log -p -- README.md"), ""},
		{"add, then grep of a path outside the roots", sh("git add -A && git grep -n x HEAD -- README.md"), ""},
		{"add of a path, then diff", sh("git add README.md && git diff"), ""},
		{"stash, then diff", sh("git stash && git diff"), ""},
		{"fetch, then diff", sh("git fetch && git diff"), ""},
		{"add, then diff --stat", sh("git add -A && git diff --stat"), ""},
		{"add, then diff --cached of a root", sh("git add -A && git diff --cached -- pbip"), git},
		{"add, then diff --cached of a glob", sh("git add -A && git diff --cached -- '*.md'"), git},
		{"add, then diff --cached with no path", sh("git add -A && git diff --cached"), git},
		{"add, then log -p --full-diff", sh("git add -A && git log -p --full-diff -- README.md"), git},
		{"add, then show of a path", sh("git add -A && git show HEAD -- README.md"), git},
		{"add, then stash show -p", sh("git add -A && git stash show -p -- README.md"), git},
		{"reset, then diff", sh("git reset && git diff"), git},
		{"restore --staged, then diff", sh("git restore --staged . && git diff"), git},
		{"add -N, then diff", sh("git add -N pbip && git diff"), git},
		{"stash -p, then diff", sh("git stash -p && git diff"), git},
		{"add, then diff of a revision", sh("git add -A && git diff HEAD"), git},
		{"switch and pull, then diff", sh("git switch main && git pull && git diff"), git},
	})
}
