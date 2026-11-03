package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellStashShowOptions: git stash show prints a patch when it is
// given any diff option that is not an output format (checked against git
// 2.53: -w, -b, -M, -R, --color, --histogram, --no-prefix, --abbrev,
// --pretty=raw, --notes, --graph ... all print one). It prints none with no
// options (stash.showPatch unset), or with only output formats that print a
// summary. Anything else goes through the name-only run.
func TestShellStashShowOptions(t *testing.T) {
	root := newRepo(t, cleanFiles())
	write(t, root, leakVisual, slicer("student", "full_name", "Doe, Jane"))
	gitIn(t, root, "add", "--all")
	gitIn(t, root, "stash", "--quiet")
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	denied := []string{
		"-p", "--patch", "-w", "-b", "-M", "-C", "-R", "-a", "-z", "--color", "--no-color", "--histogram",
		"--patience", "--minimal", "--diff-algorithm=myers", "--no-prefix", "--abbrev", "--full-index",
		"--text", "--no-renames", "--pretty=raw", "--notes", "--graph", "-U1", "--word-diff", "-B",
		"--ignore-all-space", "--ext-diff", "--textconv", "-W", "--binary", "--exit-code", "--relative",
		"--line-prefix=x", "--src-prefix=x/", "--stat -w", "--name-only -R", "-u -w",
	}
	cases := []decideCase{}
	for _, o := range denied {
		cases = append(cases, decideCase{"stash show " + o, sh("git stash show " + o), git})
	}
	for _, o := range []string{
		"", "--stat", "--numstat", "--shortstat", "--name-only", "--name-status", "--summary", "--dirstat",
		"--compact-summary", "-u", "--include-untracked", "--stat -u", "--stat=80", "--name-only --include-untracked",
	} {
		cases = append(cases, decideCase{"stash show " + o, sh("git stash show " + o), ""})
	}
	run(t, root, cases)
}
