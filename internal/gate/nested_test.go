package gate_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/gate"
)

// worktree is where Claude Code adds a linked work tree.
const worktree = ".claude/worktrees/wt"

// nestedWorkTrees is a repository whose own tree is clean, with a linked work
// tree under .claude/worktrees whose protected root leaks and holds an
// unvettable file, and two directories that hold a .git file: one with a
// policy of its own that protects data/ as well, one whose policy does not
// load.
func nestedWorkTrees(t *testing.T) string {
	t.Helper()
	root := newRepo(t, cleanFiles())
	if err := corpus.AddWorktree(root, worktree); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(root, filepath.FromSlash(worktree))
	write(t, wt, leakVisual, slicer("student", "full_name", "Doe, Jane"))
	write(t, wt, unvettable, "not a real pbix")

	own := strings.NewReplacer(`protected_roots = ["pbip"]`, `protected_roots = ["pbip", "data"]`,
		`sql     = ["sql/**/*.sql", "pbip/**/*.sql"]`, `sql     = ["sql/**/*.sql", "pbip/**/*.sql", "data/**/*.sql"]`).
		Replace(defaultPolicy(t))
	if !strings.Contains(own, `"data"`) || !strings.Contains(own, `"data/**/*.sql"`) {
		t.Fatal("the nested policy was not written")
	}
	write(t, root, "vendor/copy/.git", "gitdir: ../../.git/modules/copy\n")
	write(t, root, "vendor/copy/.pbipx.toml", own)
	write(t, root, "vendor/copy/data/export.pbix", "not a real pbix")
	write(t, root, "vendor/broken/.git", "gitdir: ../../.git/modules/broken\n")
	write(t, root, "vendor/broken/.pbipx.toml", "version = 99\n")
	write(t, root, "vendor/broken/"+unvettable, "not a real pbix")
	return root
}

func TestNestedWorkTrees(t *testing.T) {
	root := nestedWorkTrees(t)
	wt := func(p string) string { return worktree + "/" + p }
	run(t, root, []decideCase{
		{"read of a leak in a linked work tree's root", gate.Event{Tool: gate.Read, Path: wt(leakVisual)}, gate.ReasonFinding},
		{"read of it by an absolute path", gate.Event{Tool: gate.Read, Path: filepath.Join(root, filepath.FromSlash(wt(leakVisual)))}, gate.ReasonFinding},
		{"read of it from inside the work tree's parent", gate.Event{Tool: gate.Read, Path: "wt/" + leakVisual, Cwd: ".claude/worktrees"}, gate.ReasonFinding},
		{"read of local state there", gate.Event{Tool: gate.Read, Path: wt(localState)}, gate.ReasonLocalState},
		{"read of an unvettable file there", gate.Event{Tool: gate.Read, Path: wt(unvettable)}, gate.ReasonUnvettable},
		{"read of a clean file there", gate.Event{Tool: gate.Read, Path: wt(reportJSON)}, ""},
		{"read outside its roots", gate.Event{Tool: gate.Read, Path: wt("README.md")}, ""},
		{"read under a nested policy's own root", gate.Event{Tool: gate.Read, Path: "vendor/copy/data/export.pbix"}, gate.ReasonUnvettable},
		{"read under the outer root, nested policy broken", gate.Event{Tool: gate.Read, Path: "vendor/broken/" + unvettable}, gate.ReasonUnvettable},

		{"grep over .claude", gate.Event{Tool: gate.Search, Path: ".claude"}, gate.ReasonFinding},
		{"grep over the work tree", gate.Event{Tool: gate.Search, Path: worktree}, gate.ReasonFinding},
		{"grep away from every work tree", gate.Event{Tool: gate.Search, Path: "notes"}, ""},

		{"grep -rn over .claude/worktrees", gate.Event{Tool: gate.Shell, Command: "grep -rn Jane .claude/worktrees"}, gate.ReasonFinding},
		{"cd into the work tree, then cat", gate.Event{Tool: gate.Shell, Command: "cd " + bashPath(worktree) + " && cat " + leakVisual}, gate.ReasonFinding},
		{"cat from inside the work tree", gate.Event{Tool: gate.Shell, Command: "cat " + leakVisual, Cwd: worktree}, gate.ReasonFinding},
		{"cat of local state in the work tree", gate.Event{Tool: gate.Shell, Command: "cat " + bashPath(wt(localState))}, gate.ReasonLocalState},
		{"cat outside its roots", gate.Event{Tool: gate.Shell, Command: "cat " + bashPath(wt("README.md"))}, ""},
		{"ls of a directory without work trees", gate.Event{Tool: gate.Shell, Command: "ls notes"}, ""},
		// A tree scan covers every nested work tree's roots, as it covers
		// every protected root whichever one was touched.
		{"grep -rn over the main root", gate.Event{Tool: gate.Shell, Command: "grep -rn Jane pbip"}, gate.ReasonFinding},
	})
}

// TestNestedWorkTreeFoldCase checks a root spelled in another case inside
// a nested work tree, on a file system that folds case.
func TestNestedWorkTreeFoldCase(t *testing.T) {
	defer gate.SetFoldCase(true)()
	root := nestedWorkTrees(t)
	upper := worktree + "/PBIP/" + strings.TrimPrefix(leakVisual, "pbip/")
	run(t, root, []decideCase{
		{"read", gate.Event{Tool: gate.Read, Path: upper}, gate.ReasonFinding},
		{"cat", gate.Event{Tool: gate.Shell, Command: "cat " + upper}, gate.ReasonFinding},
	})
}

// TestNestedWorkTreeMessageNamesTheCopy checks that a deny for a nested
// work tree names the file by its path in the repository.
func TestNestedWorkTreeMessageNamesTheCopy(t *testing.T) {
	root := nestedWorkTrees(t)
	d := gate.Decide(gate.Event{Tool: gate.Search, Path: ".claude", Cwd: root}, gate.Options{})
	check(t, d, gate.ReasonFinding)
	if !strings.Contains(d.Message, worktree+"/"+leakVisual) {
		t.Errorf("the message does not name the copy by its repository path: %s", d.Message)
	}
}
