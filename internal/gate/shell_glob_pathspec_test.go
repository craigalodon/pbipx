package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellGitShellGlobs: an unquoted glob in git's arguments is expanded
// by the shell before git sees it, so the gate judges the words bash would
// produce: p?ip names the root pbip when it is there. A quoted glob stays
// a git pathspec glob, which only the name-only run can judge.
func TestShellGitShellGlobs(t *testing.T) {
	tree := newRepo(t, leakyFiles())
	hist, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	// The root is in the working tree: the shell expands the pattern to it.
	run(t, tree, []decideCase{
		{"log -p of p?ip", sh("git log -p -- p?ip"), git},
		{"log -p of p[b]ip", sh("git log -p -- p[b]ip"), git},
		{"log -p of pb[i]p", sh("git log -p -- pb[i]p"), git},
		{"log -p of pbi*", sh("git log -p -- pbi*"), git},
		{"diff HEAD~1 of p?ip", sh("git diff HEAD~1 -- p?ip"), git},
		{"show of p?ip", sh("git show HEAD -- p?ip"), git},
		{"log -p of p?ip/B.Report", sh("git log -p -- p?ip/B.Report"), git},
		{"log -p of a glob that matches nothing", sh("git log -p -- zz?zz"), ""},
		{"log -p of README.m?", sh("git log -p -- README.m?"), ""},
		{"log --oneline of p?ip names the root", sh("git log --oneline -- p?ip"), gate.ReasonFinding},
		// A quoted glob is git's: the name-only run decides, as it does.
		{"log -p of a quoted p?ip", sh("git log -p -- 'p?ip'"), ""},
		{"log -p of a quoted pbi*", sh("git log -p -- 'pbi*'"), git},
		{"log -p of a double-quoted pbi*", sh(`git log -p -- "pbi*"`), git},
	})
	// The root is only in history: the shell finds nothing to expand and
	// passes the pattern on, which git matches as a pathspec.
	run(t, hist, []decideCase{
		{"history: log -p of p?ip stays a pathspec glob", sh("git log -p -- p?ip"), git},
		{"history: log -p of p[b]ip stays a pathspec glob", sh("git log -p -- p[b]ip"), git},
		{"history: log -p of pbi*", sh("git log -p -- pbi*"), git},
		{"history: diff HEAD~1 of pb*", sh("git diff HEAD~1 -- pb*"), git},
		{"history: log -p of 'pbi*'", sh("git log -p -- 'pbi*'"), git},
		{"history: log -p of README.m?", sh("git log -p -- README.m?"), ""},
	})
}
