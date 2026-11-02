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

// TestShellGitHistoryGaps: history holds the leak, committed then deleted,
// and the working tree is clean.
func TestShellGitHistoryGaps(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	ps := func(cmd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		// commit --amend prints HEAD^ against the index: the commit that
		// deleted the leak shows it.
		{"commit --amend --dry-run -v", sh("git commit --amend --dry-run -v"), git},
		{"commit --amend --dry-run -v, PowerShell", ps("git commit --amend --dry-run -v"), git},
		{"commit --dry-run -v, no amend", sh("git commit --dry-run -v"), ""},
		{"commit --amend --dry-run, no -v", sh("git commit --amend --dry-run"), ""},

		// Brace expansion makes one word several.
		{"log of a brace list", sh("git log {-p,-2}"), git},
		{"show limited to a brace list", sh("git show HEAD~1 -- {pbip,README.md}"), git},
		{"diff limited to a brace list", sh("git diff HEAD~1 -- {README.md,pbip}"), git},
		{"commit of a brace list", sh("git commit {--amend,--dry-run,-v}"), git},
		{"log of a brace sequence", sh("git log -{1..2}"), ""},
		{"log of a brace list with a patch flag", sh("git log -{p,1}"), git},
		{"log of a nested brace list", sh("git log {--oneline,{-p,-1}}"), git},
		{"log of a quoted brace list", sh("git log --oneline '{-p,-2}'"), ""},
		{"log with a reflog selector", sh("git log --oneline HEAD@{1}"), ""},
		{"log, one line each", sh("git log --oneline -2"), ""},
		{"log of a brace list away from the roots", sh("git log -p -- {README.md,notes}"), ""},
		{"log of more brace words than the gate expands", sh("git log --oneline x{1..100}"), git},
		{"log of an invalid brace", sh("git log --oneline {a}"), ""},
		{"case words echoed, then git log -p", sh("echo case x in; (git log -p)"), git},
	})
}

// TestShellGitAbbreviations: git's parse-options takes an unambiguous
// prefix of a long option. The leak is in history only.
func TestShellGitAbbreviations(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	ps := func(cmd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"--amen", sh("git commit --amen --dry-run -v"), git},
		{"--am", sh("git commit --am --dry-run -v"), git},
		{"--dry", sh("git commit --amend --dry -v"), git},
		{"--verb", sh("git commit --amend --dry-run --verb"), git},
		{"--verb, PowerShell", ps("git commit --amend --dry-run --verb"), git},
		{"an ambiguous abbreviation", sh("git commit --dry-run --ver"), git},
		{"rebase --show-cur", sh("git rebase --show-cur"), git},
		{"am --show", sh("git am --show"), git},
		{"reset --har, then a read", sh("git reset --har HEAD~1 && cat README.md"), git},
		{"stash push --pat", sh("git stash push --pat"), git},
		{"grep --open", sh("git grep --open x -- README.md"), git},
		{"--author's value is not an option", sh("git commit --author --verb -m msg"), ""},
		{"--amend with a message", sh("git commit --amend -m msg"), ""},
		{"--no-verify with a message", sh("git commit --no-verify -m msg"), ""},
		{"--mess", sh(`git commit --allow-empty --mess "$MSG"`), ""},
		{"--no-verb", sh("git commit --amend --dry-run -v --no-verb"), ""},
		// -- as an option's value hides later options.
		{"log --grep --", sh("git log --grep -- -p"), git},
		{"log -n --", sh("git log -n -- -p"), git},
		{"log, an unknown option before --", sh("git log --frobnicate -- -p"), git},
		{"log -- of a path", sh("git log --oneline -- README.md"), ""},
	})

	// The finding staged: the staged diff shows it.
	staged := newRepo(t, cleanFiles())
	write(t, staged, leakVisual, slicer("student", "full_name", "Doe, Jane"))
	gitIn(t, staged, "add", leakVisual)
	if err := os.Remove(filepath.Join(staged, filepath.FromSlash(leakVisual))); err != nil {
		t.Fatal(err)
	}
	run(t, staged, []decideCase{
		{"status --verb", sh("git status --verb"), git},
		{"status --verbo", sh("git status --verbo"), git},
		{"status --verb, PowerShell", ps("git status --verb"), git},
		{"commit --dry -v", sh("git commit --dry -v"), git},
		{"commit --dry-r -v", sh("git commit --dry-r -v"), git},
		{"commit --dry-run --verb", sh("git commit --dry-run --verb"), git},
		{"status --short", sh("git status --short"), ""},
		{"status --no-verb", sh("git status --verbose --no-verb"), ""},
		// -- as an option's value is no end of the options.
		{"-m --", sh("git commit --dry-run -m -- -v"), git},
		{"--message --", sh("git commit --dry-run --message -- -v"), git},
		{"--trailer --", sh("git commit --dry-run --trailer -- -v"), git},
		{"--template --", sh("git commit --dry-run --template -- -v"), git},
		{"-t --", sh("git commit --dry-run -t -- -v"), git},
		{"-m -- -p", sh("git commit -m -- -p"), git},
		{"-- after a message", sh("git commit --dry-run -m msg -- -v"), ""},
		// The short and porcelain formats print no diff with -v.
		{"case words echoed, then git diff --cached", sh("echo case x in; (git diff --cached)"), git},
		// A format negated, or -u's value, is no short format.
		{"status -v -ufalse", sh("git status -v -ufalse"), git},
		{"status -v -uno", sh("git status -v -uno"), git},
		{"status -s --no-short -v", sh("git status -s --no-short -v"), git},
		{"status --porcelain --no-porcelain -v", sh("git status --porcelain --no-porcelain -v"), git},
		{"status -v -z --no-null", sh("git status -v -z --no-null"), git},
		{"status --porcelain --no-po -v", sh("git status --porcelain --no-po -v"), git},
		{"commit -v --short --dry-run --no-short", sh("git commit -v --short --dry-run --no-short"), git},
		{"status --no-short --short -v", sh("git status --no-short --short -v"), ""},
		{"status --short -v, PowerShell", ps("git status --short -v"), ""},
		{"status -s --no-short -v, PowerShell", ps("git status -s --no-short -v"), git},
		{"status --short -v", sh("git status --short -v"), ""},
		{"status -s -v", sh("git status -sv"), ""},
		{"status --porcelain -v", sh("git status --porcelain -v"), ""},
		{"status --porcelain=v2 -vv", sh("git status --porcelain=v2 -vv"), ""},
		{"status --short --long -v", sh("git status --short --long -v"), git},
		{"commit --short -v", sh("git commit --short -v"), ""},
		{"commit --porcelain -v", sh("git commit --porcelain -v"), ""},
		{"commit --long -v", sh("git commit --long -v"), git},
		// A false value turns nothing on.
		{"-c commit.verbose=false", sh("git -c commit.verbose=false commit --dry-run"), ""},
		{"-c commit.verbose=0", sh("git -c commit.verbose=0 commit --dry-run"), ""},
		{"-c commit.verbose=true", sh("git -c commit.verbose=true commit --dry-run"), git},
		{"-c stash.showPatch=false", sh("git -c stash.showPatch=false stash show"), ""},
	})
}

// TestShellGitCommitVerboseConfig: commit.verbose turns on the diff of a
// dry run as -v does. The leak is committed, then deleted from the work
// tree and not committed: the diff shows it, the tree is clean.
func TestShellGitCommitVerboseConfig(t *testing.T) {
	root := newRepo(t, leakyFiles())
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(leakVisual))); err != nil {
		t.Fatal(err)
	}
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"commit -a --dry-run, no commit.verbose", sh("git commit -a --dry-run"), ""},
		{"commit -a --dry-run -v", sh("git commit -a --dry-run -v"), git},
	})
	for _, value := range []string{"true", "1", "2", "yes"} {
		gitIn(t, root, "config", "commit.verbose", value)
		run(t, root, []decideCase{
			{"commit -a --dry-run, commit.verbose " + value, sh("git commit -a --dry-run"), git},
			{"commit -a --dry-run --no-verbose, commit.verbose " + value, sh("git commit -a --dry-run --no-verbose"), ""},
		})
	}
	gitIn(t, root, "config", "commit.verbose", "false")
	run(t, root, []decideCase{{"commit -a --dry-run, commit.verbose false", sh("git commit -a --dry-run"), ""}})

	// A key set in the same line is read by git after the gate looked.
	ps := func(cmd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd} }
	run(t, root, []decideCase{
		{"commit.verbose set in the line", sh("git config commit.verbose true && git commit -a --dry-run"), git},
		{"commit.verbose set in the commit's own message substitution",
			sh(`git commit -a --dry-run -m "$(git config commit.verbose 1)"`), git},
		// Options that take a value, and read options in value position.
		{"--comment's value is --get", sh("git config --comment --get commit.verbose true; git commit -a --dry-run"), git},
		{"set --comment's value is -l", sh("git config set --comment -l commit.verbose true; git commit -a --dry-run"), git},
		{"--comment's value is a harmless key", sh("git config --comment user.name commit.verbose true && git commit -a --dry-run"), git},
		{"--comm abbreviated", sh("git config --comm --get commit.verbose true; git commit -a --dry-run"), git},
		{"--file's value", sh("git config --file .git/config commit.verbose true && git commit -a --dry-run"), git},
		// A write mode, whatever the keys.
		{"--rename-section", sh("git config color.verbose true && git config --rename-section color commit && git commit -a --dry-run"), git},
		{"--add of a harmless key", sh("git config --add user.name x && git status"), git},
		{"rename-section", sh("git config rename-section color commit && git status"), git},
		{"--edit", sh("git config --edit && git status"), git},
		{"a harmless key with a pattern", sh("git config user.name x y && git status"), git},
		// Reads.
		{"a read of one key", sh("git config commit.verbose && git commit -a --dry-run"), ""},
		{"--get-regexp", sh("git config --get-regexp 'commit.*' && git status"), ""},
		{"--list", sh("git config --list && git status"), ""},
		{"a harmless key with a --comment", sh("git config --comment 'a note' user.name x && git status"), ""},
		// The environment, set earlier in the line.
		{"export GIT_PAGER", sh("export GIT_PAGER=less; git log --oneline"), git},
		{"an assignment, then export", sh("GIT_CONFIG_COUNT=1; export GIT_CONFIG_COUNT; git status"), git},
		{"GIT_CONFIG_PARAMETERS assigned", sh("GIT_CONFIG_PARAMETERS=\"'commit.verbose'='1'\"; git commit -a --dry-run"), git},
		{"export GIT_CONFIG_GLOBAL", sh("export GIT_CONFIG_GLOBAL=x.cfg && git status"), git},
		{"export of an identity", sh("export GIT_AUTHOR_NAME=x; git commit -m msg"), ""},
		// The configuration written other than by git config.
		{"printf appending to .git/config", sh(`printf '[commit]\n\tverbose = true\n' >> .git/config && git commit -a --dry-run`), git},
		{"sed -i of .git/config", sh("sed -i 's/x/y/' .git/config && git status"), git},
		{"cp onto .git/config", sh("cp other.cfg .git/config && git status"), git},
		{"tee into .git/config", sh("tee -a .git/config < other.cfg && git status"), git},
		{"a write from inside the git directory", sh("cd .git && printf x >> config && cd .. && git status"), git},
		{"HOME assigned before git", sh("HOME=/tmp/h git commit -a --dry-run"), git},
		{"HOME exported", sh("export HOME=/tmp/h; git status"), git},
		{"XDG_CONFIG_HOME assigned", sh("XDG_CONFIG_HOME=/tmp/x; git commit -a --dry-run"), git},
		{"a read of .git/config", sh("cat .git/config && git status"), ""},
		{"commit.verbose set in the line, PowerShell", ps("git config commit.verbose true; git commit -a --dry-run"), git},
		{"commit.verbose unset in the line", sh("git config --unset commit.verbose && git commit -a --dry-run"), git},
		{"a key from a variable", sh(`git config "$K" true && git status`), git},
		{"an alias set in the line", sh("git config alias.st 'status -v' && git status"), git},
		{"an identity set in the line", sh("git config user.email pat@example.com && git commit -m msg"), ""},
		{"an identity set in the line, PowerShell", ps("git config user.name 'Example, Pat'; git commit -m msg"), ""},
		{"a key read in the line", sh("git config --get commit.verbose && git commit -a --dry-run"), ""},
		{"a key read with get", sh("git config get commit.verbose && git status"), ""},
		{"a key set, then push", sh("git config commit.verbose true && git push"), ""},
	})

	// A submodule's changes shown as a patch are files the name-only
	// check does not list.
	run(t, root, []decideCase{
		{"diff of a path away", sh("git diff -- README.md"), ""},
		{"diff --submodule=diff", sh("git diff --submodule=diff -- README.md"), git},
		{"diff --submodule=short", sh("git diff --submodule=short -- README.md"), ""},
	})
	gitIn(t, root, "config", "diff.submodule", "diff")
	run(t, root, []decideCase{
		{"diff, diff.submodule diff", sh("git diff -- README.md"), git},
		{"diff --submodule=log, diff.submodule diff", sh("git diff --submodule=log -- README.md"), ""},
		{"diff --stat, diff.submodule diff", sh("git diff --stat -- README.md"), ""},
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

// TestShellGitUnknownWords: an argument git is given that cannot be told
// could be any pathspec, revision, option or directory. Before "--" it
// could be an option that turns content on; after it, a pathspec. A
// subcommand that prints content is then denied without asking git; one
// that never does is allowed.
func TestShellGitUnknownWords(t *testing.T) {
	root := newRepo(t, cleanFiles())
	write(t, root, warnVisual, slicer("student", "full_name", "Doe, Jane"))
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"diff of a variable", sh("git diff -- $X"), git},
		{"diff of a quoted variable", sh(`git diff -- "$X"`), git},
		{"diff of a built name", sh("X=pb; git diff -- ${X}ip"), git},
		{"diff of a substitution", sh("git diff -- $(echo notes)"), git},
		{"diff in an unknown directory", sh("git -C $D diff"), git},
		{"show of a variable", sh("git show $REV"), git},
		{"log -p of a variable", sh("git log -p $REV"), git},
		{"grep under a variable", sh("git grep -e x -- $X"), git},
		{"blame of a variable", sh("git blame $F"), git},
		{"cat-file of a variable", sh("git cat-file -p $OBJ"), git},
		{"stash show -p of a variable", sh("git stash show -p $S"), git},
		{"unknown subcommand", sh("git $SUB"), git},
		{"unknown word before the subcommand", sh("git $OPT log --oneline"), git},
		{"unknown global option", sh("git --namespace=$N log --oneline"), git},
		// config --file reads any file as configuration.
		{"config --get of a variable", sh("git config --get $K"), git},
		// Before "--" a word that cannot be told could be -p, -v or the
		// like.
		{"log of a substitution", sh("git log $(echo -p)"), git},
		{"log --oneline of a variable", sh("git log --oneline $REV"), git},
		{"log of a variable", sh("git log $X"), git},
		{"whatchanged of a variable", sh("git whatchanged $X"), git},
		{"reflog of a variable", sh("git reflog $X"), git},
		{"diff --stat of a variable", sh("git diff --stat $X"), git},
		{"status of a variable", sh("git status $X"), git},
		{"commit of a variable", sh("git commit $X"), git},
		{"log -m of a variable", sh("git log -m $X"), git},
		{"log -m with a variable attached", sh("git log -m$X"), git},
		// A message is never an option.
		{"commit -m of a variable", sh(`git commit -m "$MSG"`), ""},
		{"commit -m of a substitution", sh("git commit -m \"$(cat <<'EOF'\nA message\nEOF\n)\""), ""},
		{"commit --message= of a variable", sh(`git commit --message="$MSG"`), ""},
		{"stash push -m of a variable", sh(`git stash push -m "$MSG"`), ""},
		// An expansion outside double quotes is split into words, which
		// could be options: such a message is not one.
		{"commit -m of an unquoted variable", sh(`git commit -m $X`), git},
		{"commit -am of an unquoted variable", sh(`git commit -am $X`), git},
		{"commit -m with an unquoted variable attached", sh(`git commit -m$X`), git},
		{"commit --message= of an unquoted variable", sh(`git commit --message=$X`), git},
		{"commit -m of an unquoted substitution", sh("git commit -m $(cat <<'EOF'\nA message\nEOF\n)"), git},
		{"commit -m of a backtick substitution unquoted", sh("git commit -m `cat msg.txt`"), git},
		{"commit -m with one unquoted part", sh(`git commit -m "$X"$Y`), git},
		{"commit -m of an unquoted parameter operator", sh(`git commit -m ${X:-x}`), git},
		{"commit -m of a positional parameter", sh(`git commit -m $1`), git},
		{"commit -m of a variable within text", sh(`git commit -m "fix: $X done"`), ""},
		{"commit -m of a literal", sh(`git commit -m 'literal'`), ""},
		// Some expansions split inside double quotes too: all positional
		// parameters, all array elements, all matching names.
		{"commit -m of quoted $@", sh(`set -- x --amend --dry-run -v; git commit -m "$@"`), git},
		{"commit -qm of a quoted array", sh(`a=(--dry-run -v); git commit -qm "${a[@]}"`), git},
		{"commit -m of quoted ${@:2}", sh(`git commit -m "${@:2}"`), git},
		{"commit -m of quoted ${!p@}", sh(`git commit -m "${!p@}"`), git},
		{"commit -m of a quoted array's keys", sh(`git commit -m "${!a[@]}"`), git},
		{"commit -m of quoted $*", sh(`git commit -m "$*"`), ""},
		{"commit -m of a quoted zsh array", sh(`git commit -m "$a[@]"`), git},
		{"commit -m of a quoted zsh subscript", sh(`git commit -m "$a[1]"`), git},
		{"commit -m of quoted ${*}", sh(`git commit -m "${*}"`), ""},
		// An unquoted glob expands to several words.
		{"commit -m of a glob", sh(`git commit -m *`), git},
		{"commit -m of a variable and a glob", sh(`git commit -m "$X"*`), git},
		{"commit -m of a name and a glob", sh(`git commit -m pbip*`), git},
		{"commit -m of a quoted glob", sh(`git commit -m "fix *"`), ""},
		{"commit -m of $\"...\"", sh(`git commit -m $"$X"`), ""},
		// -m clustered after short options that take no value.
		{"commit -am of a substitution", sh("git commit -am \"$(cat <<'EOF'\nA message\nEOF\n)\""), ""},
		{"commit -sm of a substitution", sh("git commit -sm \"$(cat <<'EOF'\nA message\nEOF\n)\""), ""},
		{"commit -asm of a variable", sh(`git commit -asm "$MSG"`), ""},
		{"commit -am with a variable attached", sh(`git commit -am"$MSG"`), ""},
		{"commit -a -s -m of a variable", sh(`git commit -a -s -m "$MSG"`), ""},
		{"commit --amend -m of a variable", sh(`git commit --amend -m "$MSG"`), ""},
		{"commit --no-verify --message of a variable", sh(`git commit --no-verify --message "$MSG"`), ""},
		{"commit --author apart, then -m", sh(`git commit --author "Example, Pat <pat@example.com>" -m "$MSG"`), ""},
		{"merge -nm of a variable", sh(`git merge -nm "$MSG" topic`), ""},
		{"stash -um of a variable", sh(`git stash -um "$MSG"`), ""},
		{"stash push -Sm of a variable", sh(`git stash push -Sm "$MSG"`), ""},
		{"notes add -fm of a variable", sh(`git notes add -fm "$MSG" HEAD`), ""},
		// m is the value of the letter before it: the next word is not a
		// message.
		{"commit -Fm of a variable", sh(`git commit -Fm "$X"`), git},
		{"commit -cm of a variable", sh(`git commit -cm "$X"`), git},
		{"commit -Cm of a variable", sh(`git commit -Cm "$X"`), git},
		{"commit -tm of a variable", sh(`git commit -tm "$X"`), git},
		{"commit -Sm of a variable", sh(`git commit -Sm "$X"`), git},
		{"commit -um of a variable", sh(`git commit -um "$X"`), git},
		{"commit -aFm with a variable attached", sh(`git commit -aFm"$X"`), git},
		{"merge -sm of a variable", sh(`git merge -sm "$X" topic`), git},
		{"merge -Xm of a variable", sh(`git merge -Xm "$X" topic`), git},
		{"merge -Sm of a variable", sh(`git merge -Sm "$X" topic`), git},
		// -m that is itself the value of the option before it.
		{"commit -F -m of a variable", sh(`git commit --dry-run -F -m "$X"`), git},
		{"commit --author -m of a variable", sh(`git commit --dry-run --author -m "$X"`), git},
		{"commit, an unknown letter before -m", sh(`git commit -Q -m "$X"`), git},
		{"commit, an abbreviated long option before -m", sh(`git commit --dry --auth -m "$X"`), git},
		// The cluster is still read for the options it holds.
		{"commit -pm of a substitution", sh("git commit -pm \"$(cat <<'EOF'\nA message\nEOF\n)\""), git},
		{"stash push -pm of a variable", sh(`git stash push -pm "$MSG"`), git},
		// Only stash push and notes add or append take a message.
		{"stash list -m of a variable", sh(`git stash list -m "$X"`), git},
		{"notes show -m of a variable", sh(`git notes show -m "$X"`), git},
		{"add of a variable", sh("git add $X"), git},
		{"stash of a variable", sh("git stash $X"), git},
		// After "--" it is a pathspec, never an option.
		{"log --oneline limited to a variable", sh("git log --oneline -- $X"), ""},
		{"diff --stat limited to a variable", sh("git diff --stat -- $X"), ""},
		{"status limited to a variable", sh("git status --short -- $X"), ""},
		{"add limited to a variable", sh("git add -- $X"), ""},
		{"status in an unknown directory", sh("git -C $D status"), ""},
		// Commands that print no file content whatever their options.
		{"branch of a variable", sh("git branch $B"), ""},
		{"fetch of a variable", sh("git fetch $REMOTE"), ""},
		{"push of a variable", sh("git push $REMOTE $B"), ""},
		{"tag -l of a variable", sh("git tag -l $T"), ""},
		{"remote of a variable", sh("git remote $X"), ""},
	})
}
