package gate_test

import (
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/gate"
)

// notesRepo is cleanFiles committed, then a second commit that changes
// only a note, tagged: the working tree is clean, the first commit holds
// files under the root and the second does not.
func notesRepo(t *testing.T) string {
	t.Helper()
	root := newRepo(t, cleanFiles())
	write(t, root, cleanNote, "Nothing personal here, still.\n")
	gitIn(t, root, "commit", "--quiet", "--all", "--message", "Edit a note")
	gitIn(t, root, "tag", "--annotate", "--message", "a note", "notes-tag")
	return root
}

func TestShellGitNameOnly(t *testing.T) {
	root := notesRepo(t)
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		// A clean working tree: the diffs print nothing.
		{"diff", sh("git diff"), ""},
		{"diff HEAD", sh("git diff HEAD"), ""},
		{"diff --cached", sh("git diff --cached"), ""},
		{"diff against the first commit", sh("git diff HEAD~1"), ""},
		{"diff between the commits", sh("git diff HEAD~1 HEAD"), ""},
		{"diff of a path without --", sh("git diff HEAD~1 HEAD notes"), ""},
		{"diff of a working tree file against the first commit", sh("git diff HEAD~1 -- " + reportJSON), ""},
		{"diff with a patch flag", sh("git diff -p --stat HEAD~1"), ""},

		// show, log and diff-tree of a commit that touches no root.
		{"show", sh("git show"), ""},
		{"show HEAD", sh("git show HEAD"), ""},
		{"show a tag of the commit", sh("git show notes-tag"), ""},
		{"show --format", sh("git show --format=%B HEAD"), ""},
		{"show the first commit", sh("git show HEAD~1"), git},
		{"show the first commit's notes", sh("git show HEAD~1 -- notes"), ""},
		{"show with a path, no --", sh("git show HEAD~1 notes"), ""},
		{"show --max-count with a value", sh("git show --max-count 1 HEAD"), ""},
		{"show -n with a value", sh("git show -n 1 HEAD~1"), git},
		{"show --author with a value", sh("git show --author Test HEAD"), ""},
		{"show --stat", sh("git show --stat HEAD~1"), ""},
		{"show --output to a file", sh("git show --output=out.patch HEAD~1"), git},
		{"show --remerge-diff", sh("git show --remerge-diff HEAD"), git},
		{"log -p -1", sh("git log -p -1"), ""},
		{"log -p", sh("git log -p"), git},
		{"log -p of a path", sh("git log -p " + cleanNote), ""},
		{"log -p -- notes", sh("git log -p -- notes"), ""},
		{"log -p of the root", sh("git log -p pbip"), git},
		{"log -p --since", sh("git log -p --since 2000-01-01 -- notes"), ""},
		{"log -p --oneline", sh("git log -p --oneline -1"), ""},
		{"log -p --graph", sh("git log -p --graph -1"), ""},
		{"log -L", sh("git log -L1,2:" + cleanNote), git},
		{"log -p --stdin", sh("git log -p --stdin"), git},
		{"diff-tree -p", sh("git diff-tree -p HEAD"), ""},
		{"diff-tree -p of the first commit", sh("git diff-tree -p --root HEAD~1"), git},
		{"stash list -p with no stash", sh("git stash list -p"), ""},

		// grep: the files it would print lines from.
		{"grep for text away from the roots", sh("git grep Nothing"), ""},
		{"grep for text under a root", sh("git grep layoutOptimization"), git},
		{"grep -e with a path", sh("git grep -e layoutOptimization notes"), ""},
		{"grep -l", sh("git grep -l layoutOptimization"), ""},
		{"grep -c", sh("git grep -c layoutOptimization"), ""},
		{"grep -q", sh("git grep -q layoutOptimization"), ""},
		{"grep -il cluster", sh("git grep -il layoutoptimization"), ""},
		{"grep -e -l is a pattern", sh("git grep -e -l -e layoutOptimization"), git},
		{"grep -l undone", sh("git grep -l --no-files-with-matches layoutOptimization"), git},
		{"grep -O", sh("git grep -O layoutOptimization"), git},
		{"grep -Ocat", sh("git grep -Ocat x"), git},
		{"grep -f -", sh("git grep -f - notes"), git},
		{"grep a revision away", sh("git grep layoutOptimization HEAD~1 -- notes"), ""},
		{"grep a revision", sh("git grep layoutOptimization HEAD~1"), git},
		{"grep a tree", sh("git grep layoutOptimization HEAD~1:pbip"), git},
		{"grep from inside a root, limited away", gate.Event{Tool: gate.Shell, Command: "git grep Nothing -- :/notes", Cwd: "pbip/A.Report"}, ""},

		// HEAD@{...}:path cuts at the colon outside the braces.
		{"show at a date, path under a root", sh("git show 'HEAD@{2000-01-01 10:00}:" + reportJSON + "'"), git},
		{"cat-file at a reflog entry, path away", sh("git cat-file -p 'HEAD@{0}:README.md'"), ""},
		// The reflog's first entry is dated 2100 and its second today, so
		// HEAD@{10:00} names the first commit once the clock passes 10:00
		// and the second before; a time late in 2099 always names the
		// second (git reads no year past 2099 as a date).
		{"show at a time is a commit, not a path", sh("git show 'HEAD@{2099-12-31 10:00}'"), ""},

		// An alias defined earlier in the line, or a git-<name> program.
		{"alias defined earlier in the line", sh("git config alias.lp 'log -p' && git lp"), git},
		{"unknown subcommand", sh("git frobnicate"), git},
	})
}

func TestShellGitWorkingTree(t *testing.T) {
	git := gate.ReasonGitContent
	staged := newRepo(t, cleanFiles())
	write(t, staged, reportJSON, `{"layoutOptimization": "Fit"}`)
	gitIn(t, staged, "add", reportJSON)
	run(t, staged, []decideCase{
		{"status", sh("git status"), ""},
		{"status -v", sh("git status -v"), git},
		{"status --verbose", sh("git status --verbose"), git},
		{"status -sv cluster", sh("git status -sv"), git},
		{"status -v undone", sh("git status -v --no-verbose"), ""},
		{"commit --dry-run", sh("git commit --dry-run"), ""},
		{"commit --dry-run -v", sh("git commit --dry-run -v"), git},
		{"diff --cached", sh("git diff --cached"), git},
		{"diff", sh("git diff"), ""},
	})

	dirty := newRepo(t, cleanFiles())
	write(t, dirty, reportJSON, `{"layoutOptimization": "Fit"}`)
	write(t, dirty, cleanNote, "Still nothing.\n")
	gitIn(t, dirty, "add", cleanNote)
	run(t, dirty, []decideCase{
		{"status -v of a staged note", sh("git status -v"), ""},
		{"status -vv", sh("git status -vv"), git},
		{"status -v -v", sh("git status -v -v"), git},
		{"commit --dry-run --verbose", sh("git commit --dry-run --verbose"), git},
		{"diff", sh("git diff"), git},
		{"diff --stat", sh("git diff --stat"), ""},
		{"diff of notes", sh("git diff notes"), ""},
		{"diff -- notes", sh("git diff -- notes"), ""},
		{"diff --output to a file", sh("git diff --output=out.patch"), git},
		{"diff --output value apart", sh("git diff --output out.patch -- notes"), ""},
		{"diff --check", sh("git diff --check"), git},
		{"diff --no-index away", sh("git diff --no-index " + cleanNote + " README.md"), ""},
		{"diff --no-index of a root file", sh("git diff --no-index README.md " + reportJSON), git},
		{"diff --no-index of a directory", sh("git diff --no-index notes sql"), git},
		{"diff-files -p", sh("git diff-files -p"), git},
		{"diff-index -p", sh("git diff-index -p HEAD -- notes"), ""},
		{"add -p of notes", sh("git add -p notes"), ""},
		{"restore -p with a source and no path", sh("git restore -p -s HEAD"), git},
		{"restore -p with a source and a path", sh("git restore -p --source HEAD notes"), ""},
		{"checkout -p with a tree-ish and a path", sh("git checkout -p HEAD notes"), ""},
		{"checkout -p with a tree-ish", sh("git checkout -p HEAD"), git},
		{"reset -p of notes", sh("git reset -p HEAD notes"), ""},
	})
}

func TestShellGitStashes(t *testing.T) {
	git := gate.ReasonGitContent
	root := newRepo(t, cleanFiles())
	write(t, root, cleanNote, "A stashed note.\n")
	gitIn(t, root, "stash", "push", "--quiet", "--message", "a note")
	run(t, root, []decideCase{
		{"stash show -p of a note", sh("git stash show -p"), ""},
		{"stash list -p of a note", sh("git stash list -p"), ""},
		{"stash show -u", sh("git stash show -u"), ""},
	})
	write(t, root, reportJSON, `{"layoutOptimization": "Fit"}`)
	gitIn(t, root, "stash", "push", "--quiet", "--message", "a report")
	run(t, root, []decideCase{
		{"stash show -p of a report", sh("git stash show -p"), git},
		{"stash show -p of the older stash", sh("git stash show -p 'stash@{1}'"), ""},
		{"stash list -p", sh("git stash list -p"), git},
		{"stash list --patch -1 of the note", sh("git stash list --patch --skip 1"), ""},
		{"stash list", sh("git stash list"), ""},
	})
}

func TestShellGitRenamesAndMerges(t *testing.T) {
	git := gate.ReasonGitContent
	// The report is long enough for git to find it renamed.
	files := cleanFiles()
	files[reportJSON] = "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3,\n  \"d\": 4,\n  \"e\": 5\n}\n"
	root := newRepo(t, files)
	gitIn(t, root, "branch", "side")
	// On main: the report moves out of the root.
	gitIn(t, root, "mv", reportJSON, "notes/report.json")
	gitIn(t, root, "commit", "--quiet", "--message", "Move the report")
	run(t, root, []decideCase{
		{"show a rename out of the root", sh("git show"), git},
		{"show a rename with renames off", sh("git show --no-renames"), git},
		{"log -p -1 of the rename", sh("git log -p -1"), git},
	})
	// On side: a note changes; the merge edits the moved report.
	gitIn(t, root, "checkout", "--quiet", "side")
	write(t, root, cleanNote, "Side note.\n")
	gitIn(t, root, "commit", "--quiet", "--all", "--message", "Side note")
	gitIn(t, root, "checkout", "--quiet", "-")
	gitIn(t, root, "merge", "--quiet", "--no-commit", "side")
	write(t, root, "notes/report.json", "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3,\n  \"d\": 4,\n  \"e\": 6\n}\n")
	gitIn(t, root, "add", "notes/report.json")
	gitIn(t, root, "commit", "--quiet", "--message", "Merge side")
	run(t, root, []decideCase{
		{"log -p -1 of a merge prints no diff", sh("git log -p -1"), ""},
		{"show a merge", sh("git show"), git},
		{"show a merge with -m", sh("git show -m"), git},
		{"show a merge's first parent", sh("git show --first-parent"), ""},
		{"log --cc of a merge", sh("git log --cc -1"), git},
		{"log -p -1 -- sql", sh("git log -p -1 -- sql"), ""},
		{"log --remerge-diff", sh("git log --remerge-diff -1"), git},
	})
}

func TestShellGitOtherRepositories(t *testing.T) {
	git := gate.ReasonGitContent
	root := newRepo(t, cleanFiles())
	other := newRepo(t, leakyFiles())
	plain := t.TempDir()
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, root, "worktree", "add", "--quiet", wt)
	run(t, root, []decideCase{
		{"another repository", sh("git -C " + shellPath(other) + " log -p"), ""},
		{"another repository's blob", sh("git -C " + shellPath(other) + " show HEAD:" + leakVisual), ""},
		{"another repository, diffing a file here", sh("git -C " + shellPath(other) + " diff --no-index README.md " + shellPath(filepath.Join(root, reportJSON))), git},
		{"outside any repository, grepping here", sh("git -C " + shellPath(plain) + " grep --no-index -e x " + shellPath(filepath.Join(root, "pbip"))), git},
		{"outside any repository", sh("git -C " + shellPath(plain) + " grep --no-index -e x ."), ""},
		{"a directory not yet made", sh("git -C " + shellPath(filepath.Join(plain, "later")) + " log -p"), git},
		{"a directory above the repository", sh("git -C " + shellPath(filepath.Dir(root)) + " grep --no-index -e x"), git},
		{"a linked work tree shares history", sh("git -C " + shellPath(wt) + " log -p"), git},
		{"a linked work tree, a path away", sh("git -C " + shellPath(wt) + " log -p -- notes"), ""},
		{"a linked work tree's blob", sh("git -C " + shellPath(wt) + " show HEAD:" + reportJSON), git},
		{"a linked work tree's blame", sh("git -C " + shellPath(wt) + " blame " + reportJSON), git},
		{"a linked work tree's blame away", sh("git -C " + shellPath(wt) + " blame README.md"), ""},
	})

	// A repository nested in the work tree: the roots apply relative to it.
	inner := filepath.Join(root, "vendor", "inner")
	write(t, inner, "README.md", "Inner.\n")
	write(t, inner, "pbip/x.json", "{}\n")
	if err := corpus.GitSetup(inner, nil, nil); err != nil {
		t.Fatal(err)
	}
	run(t, root, []decideCase{
		{"a nested repository's root file", sh("git -C vendor/inner blame pbip/x.json"), git},
		{"a nested repository's log", sh("git -C vendor/inner log -p"), git},
		{"a nested repository's log, a path away", sh("git -C vendor/inner log -p -- README.md"), ""},
		{"a nested repository's blame away", sh("git -C vendor/inner blame README.md"), ""},
	})
}
