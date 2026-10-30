package gate_test

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// gitIn runs git in root with a fixed identity and returns its output,
// trimmed.
func gitIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...) //nolint:gosec // G204: fixed git subcommands with the test's own operands, passed as argv
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", args[0], err)
	}
	return strings.TrimSpace(string(out))
}

// historyRepo is a repository whose first commit holds a leak under the
// protected root and whose second removes it: the working tree is clean,
// and only history holds the value. It has an annotated tag of the leaky
// blob, one of the first commit, and a few aliases.
func historyRepo(t *testing.T) (root, blob string) {
	t.Helper()
	root = newRepo(t, leakyFiles())
	blob = gitIn(t, root, "rev-parse", "HEAD:"+leakVisual)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(leakVisual))); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "commit", "--quiet", "--all", "--message", "Remove the slicer")
	gitIn(t, root, "tag", "--annotate", "--message", "a blob", "blob-tag", blob)
	gitIn(t, root, "tag", "--annotate", "--message", "a commit", "commit-tag", "HEAD~1")
	for name, value := range map[string]string{
		"lg":   "log --oneline",
		"sp":   "show -p",
		"bang": "!cat README.md",
		"two":  "lg",
		"st":   "status --short",
		"q":    "log 'unclosed",
		"cfg":  "-c core.pager=less log --oneline",
	} {
		gitIn(t, root, "config", "alias."+name, value)
	}
	return root, blob
}

func TestShellGitHistory(t *testing.T) {
	root, blob := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		// The working tree is clean: naming the root alone allows.
		{"cat of the clean tree", sh("ls " + path.Dir(leakVisual)), ""},

		// blame takes the file before or after the revision.
		{"blame file then revision", sh("git blame " + leakVisual + " HEAD~1"), git},
		{"blame revision then file", sh("git blame HEAD~1 " + leakVisual), git},
		{"blame revision -- file", sh("git blame HEAD~1 -- " + leakVisual), git},
		{"annotate file then revision", sh("git annotate " + leakVisual + " HEAD~1"), git},
		{"blame file outside then revision", sh("git blame README.md HEAD~1"), ""},
		{"blame revision then file outside", sh("git blame HEAD~1 README.md"), ""},
		{"blame with a line range", sh("git blame -L 1,2 README.md HEAD"), ""},
		{"blame --contents in a root", sh("git blame --contents=" + reportJSON + " README.md"), git},
		{"blame with no file", sh("git blame"), git},

		// :/<text> and ^{/<text>} search commit messages.
		{"show :/text", sh("git show ':/Remove'"), git},
		{"show :/text with --stat", sh("git show --stat ':/corpus'"), git},
		{"show rev:/text", sh("git show 'HEAD:/x'"), git},
		{"show ^{/text}", sh("git show 'HEAD^{/corpus}'"), git},
		{"cat-file -p :/text", sh("git cat-file -p ':/corpus'"), git},

		// show prints blobs, trees and tags whatever the summary flags say.
		{"show --stat blob", sh("git show --stat " + blob), git},
		{"show -s blob", sh("git show -s " + blob), git},
		{"show --no-patch blob", sh("git show --no-patch " + blob), git},
		{"show --stat tree", sh("git show --stat 'HEAD~1^{tree}'"), git},
		{"show blob limited to a path away", sh("git show " + blob + " -- notes"), git},
		{"show --stat tag of a blob", sh("git show --stat blob-tag"), git},
		{"show --stat tag of a commit", sh("git show --stat commit-tag"), ""},
		{"show tag of a commit", sh("git show commit-tag"), git},
		{"show --stat commit", sh("git show --stat HEAD~1"), ""},
		{"show --stat range", sh("git show --stat HEAD~1..HEAD"), ""},
		{"show --stat exclusion", sh("git show --stat ^HEAD~1 HEAD"), ""},
		{"show --stat range to a blob", sh("git show --stat HEAD.." + blob), git},
		{"show --stat unresolvable", sh("git show --stat no-such-ref"), git},
		{"show --stat -n 1", sh("git show --stat -n 1 HEAD"), ""},
		{"show --stat --all", sh("git show --stat --all"), git},
		{"show --stat --stdin", sh("git show --stat --stdin"), git},
		{"cat-file -p tag", sh("git cat-file -p blob-tag"), git},
		{"cat-file -t tag", sh("git cat-file -t blob-tag"), ""},
		{"cat-file --batch-command", sh("git cat-file --batch-command"), git},

		// Other commands that print content.
		{"diff-tree -p", sh("git diff-tree -p HEAD"), git},
		{"diff-tree --cc", sh("git diff-tree --cc HEAD"), git},
		{"diff-tree --stdin", sh("git diff-tree --stdin"), git},
		{"diff-tree raw", sh("git diff-tree -r HEAD"), ""},
		{"diff-tree -p limited away", sh("git diff-tree -p HEAD -- notes"), ""},
		{"diff-index -p", sh("git diff-index -p HEAD~1"), git},
		{"diff-index -p of a clean tree", sh("git diff-index -p HEAD"), ""},
		{"diff-index raw", sh("git diff-index HEAD"), ""},
		{"diff-files -p of a clean tree", sh("git diff-files -p"), ""},
		{"fast-export", sh("git fast-export --all"), git},
		{"fast-export limited", sh("git fast-export --all -- notes"), git},
		{"pack-objects", sh("git pack-objects --stdout"), git},
		{"unpack-file", sh("git unpack-file " + blob), git},
		{"bundle create to a file", sh("git bundle create all.bundle --all"), ""},
		{"bundle create to stdout", sh("git bundle create - --all"), git},
		{"bundle create quiet to stdout", sh("git bundle create -q - --all"), git},
		{"bundle list-heads", sh("git bundle list-heads all.bundle"), ""},
		{"log --remerge-diff", sh("git log --remerge-diff"), git},
		{"log -G", sh("git log -G Doe --oneline"), git},
		{"log -S attached", sh("git log -SDoe --oneline"), git},
		{"log -S limited away", sh("git log -S Doe --oneline -- notes"), ""},
		{"log --format=%B", sh("git log --format=%B"), ""},
		{"whatchanged -p", sh("git whatchanged -p"), git},
		{"rebase --show-current-patch", sh("git rebase --show-current-patch"), git},
		{"am --show-current-patch=diff", sh("git am --show-current-patch=diff"), git},
		{"rebase --exec", sh("git rebase --exec 'make test' HEAD~1"), git},
		{"rebase -x", sh("git rebase -x true HEAD~1"), git},
		{"rebase", sh("git rebase HEAD~1"), ""},
		{"request-pull -p", sh("git request-pull -p HEAD~1 origin"), git},
		{"request-pull", sh("git request-pull HEAD~1 origin"), ""},
		{"merge-tree trivial", sh("git merge-tree HEAD~1 HEAD HEAD"), git},
		{"merge-tree --trivial-merge", sh("git merge-tree --trivial-merge HEAD~1 HEAD HEAD"), git},
		{"merge-tree write-tree", sh("git merge-tree HEAD HEAD~1"), ""},
		{"merge-file --object-id", sh("git merge-file -p --object-id " + blob + " " + blob + " " + blob), git},
		{"submodule foreach", sh("git submodule foreach true"), git},
		{"submodule status", sh("git submodule status"), ""},
		{"bisect run", sh("git bisect run true"), git},

		// Interactive hunks.
		{"add -p", sh("git add -p"), git},
		{"add --patch", sh("git add --patch"), git},
		{"add -i", sh("git add -i"), git},
		{"add -up cluster", sh("git add -up"), git},
		{"add -p limited away", sh("git add -p notes"), ""},
		{"add -p -- limited away", sh("git add -p -- notes"), ""},
		{"add -p from a file", sh("git add -p --pathspec-from-file=list"), git},
		{"add", sh("git add notes"), ""},
		{"checkout -p", sh("git checkout -p"), git},
		{"checkout -b", sh("git checkout -b topic-p"), ""},
		{"restore -p", sh("git restore -p"), git},
		{"restore -p limited away", sh("git restore -p notes"), ""},
		{"reset -p", sh("git reset -p"), git},
		{"stash -p", sh("git stash -p"), git},
		{"stash push -p", sh("git stash push -p"), git},
		{"stash push", sh("git stash push -m wip"), ""},
		{"commit -p", sh("git commit -p"), git},
		{"commit --interactive", sh("git commit --interactive"), git},
		{"commit -am", sh("git commit -am 'add patch'"), ""},
		{"commit -m", sh("git commit -m patch"), ""},

		// stash show with stash.showPatch unset.
		{"stash show", sh("git stash show"), ""},

		// Aliases.
		{"alias to log", sh("git lg"), ""},
		{"alias to log with -p", sh("git lg -p"), git},
		{"alias to log with -p limited away", sh("git lg -p -- notes"), ""},
		{"alias to show -p", sh("git sp"), git},
		{"shell alias", sh("git bang"), git},
		{"alias of an alias", sh("git two"), git},
		{"alias to status", sh("git st"), ""},
		{"alias that does not split", sh("git q"), git},
		{"alias with an unsafe -c", sh("git cfg"), git},
		{"not an alias", sh("git no-such-command"), git},
		{"alias set with -c", sh("git -c alias.x=status x"), git},
		{"alias set with --config-env", sh("git --config-env=alias.x=X x"), git},

		// Global options and environment.
		{"-c pager.log=false", sh("git -c pager.log=false log --oneline"), ""},
		{"-c core.pager=cat", sh("git -c core.pager=cat log --oneline"), ""},
		{"-c core.pager command", sh("git -c core.pager='sh -c true' log --oneline"), git},
		{"-c pager.status command", sh("git -c pager.status='less -R' status"), git},
		{"-c diff.external", sh("git -c diff.external=cat diff --stat"), git},
		{"-c stash.showPatch", sh("git -c stash.showPatch=true stash show"), git},
		{"-c log.showSignature", sh("git -c log.showSignature=true log --oneline"), git},
		{"-c textconv", sh("git -c diff.json.textconv=cat diff --stat"), git},
		{"-c core.editor command", sh("git -c core.editor=cat commit"), git},
		{"-c core.editor true", sh("git -c core.editor=true commit"), ""},
		{"-c color.ui", sh("git -c color.ui=never status"), ""},
		{"-c core.quotepath", sh("git -c core.quotePath=off status"), ""},
		{"--exec-path=", sh("git --exec-path=/tmp status"), git},
		{"--literal-pathspecs", sh("git --literal-pathspecs diff HEAD~1 -- . ':!pbip'"), git},
		{"--icase-pathspecs", sh("git --icase-pathspecs diff HEAD~1 -- PBIP"), git},
		{"--icase-pathspecs away", sh("git --icase-pathspecs diff HEAD~1 -- NOTES"), ""},
		{"--git-dir with a summary", sh("git --git-dir=.git show --stat " + blob), git},
		{"GIT_PAGER=cat", sh("GIT_PAGER=cat git log --oneline"), ""},
		{"GIT_PAGER command", sh("GIT_PAGER='less -R' git log --oneline"), git},
		{"GIT_EXTERNAL_DIFF", sh("GIT_EXTERNAL_DIFF=cat git diff --stat"), git},
		{"GIT_CONFIG_COUNT", sh("GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=alias.x GIT_CONFIG_VALUE_0=status git status"), git},
		{"env GIT_DIR", sh("env GIT_DIR=.git git status"), git},
		{"GIT_EDITOR=true", sh("GIT_EDITOR=true git commit"), ""},
		{"GIT_EDITOR=cat", sh("GIT_EDITOR=cat git commit -v"), git},
		{"author identity", sh("GIT_AUTHOR_NAME=x git commit -m msg"), ""},
	})
}

func TestShellGitStashShowPatch(t *testing.T) {
	root := newRepo(t, cleanFiles())
	gitIn(t, root, "config", "stash.showPatch", "true")
	write(t, root, reportJSON, `{"layoutOptimization": "Fit"}`)
	gitIn(t, root, "stash", "push", "--quiet", "--message", "wip")
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	run(t, root, []decideCase{
		{"stash show", sh("git stash show"), gate.ReasonGitContent},
		{"stash show --stat", sh("git stash show --stat"), ""},
		// stash show takes no pathspec: a word after "--" names the stash.
		{"stash show -p names a stash that does not exist", sh("git stash show -p -- notes"), ""},
		{"stash show -p of a root names no stash", sh("git stash show -p -- pbip"), ""},
		{"stash show -p of stash 0 after --", sh("git stash show -p -- 0"), gate.ReasonGitContent},
		{"stash show -p of a stash past the last", sh("git stash show -p 3"), gate.ReasonGitContent},
		{"stash list", sh("git stash list"), ""},
	})
}

// TestShellGitThroughLinkedDirectory runs git with the working directory
// spelled through a symlink, as a temporary directory under /var is on
// macOS: git prints the top of the work tree resolved, and a path inside
// it must still be placed there.
func TestShellGitThroughLinkedDirectory(t *testing.T) {
	root := newRepo(t, cleanFiles())
	write(t, root, reportJSON, `{"layoutOptimization": "Fit"}`)
	write(t, root, cleanNote, "Still nothing personal.\n")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symlinks are not available")
	}
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	shIn := func(dir, cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd, Cwd: dir} }
	run(t, link, []decideCase{
		{"diff of notes", sh("git diff notes"), ""},
		{"diff -- notes", sh("git diff -- notes"), ""},
		{"diff excluding the root", sh("git diff -- . ':!pbip'"), ""},
		{"diff from inside a root, limited away", shIn("pbip/A.Report", "git diff -- ../../notes"), ""},
		{"blame away", sh("git blame README.md"), ""},
		{"diff of the root", sh("git diff -- pbip"), gate.ReasonGitContent},
		{"diff of everything", sh("git diff"), gate.ReasonGitContent},
	})
}
