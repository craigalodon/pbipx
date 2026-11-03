package gate_test

import (
	"context"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/gate"
)

// A planted finding is in one of four places: in the working tree (and
// committed, HEAD adding it), in history only (HEAD deletes it), staged
// only (in the index and a stash, HEAD clean), or in HEAD with its fix
// staged (the working tree and the index clean, git diff empty). What is
// dangerous to run depends on which.
type findingAt int

const (
	inTree findingAt = iota
	inHistory
	inIndex
	inFixedHead
)

func (f findingAt) String() string {
	return [...]string{"in the working tree", "in history only", "staged only", "in HEAD, its fix staged"}[f]
}

// inWorktree reports whether the working tree holds the finding at f.
func (f findingAt) inWorktree() bool { return f == inTree || f == inIndex }

// sentinel is the planted value, which output is searched for.
const sentinel = "Doe, Jane"

// TestDifferentialBash runs generated command lines under real bash, with
// stub programs, and checks that whenever bash really runs a dangerous
// command (a recursive search of the working directory, a read of a
// protected file, git printing the finding) in a repository with a
// planted finding, the gate denies the line. It reads each line as bash
// does or fails closed; it catches the tokenizer mistakes the reviews
// found (a ) inside ${...} in $(...), a case pattern in $(...), a
// here-document delimiter quoted with $'...', << in arithmetic, an empty
// brace alternative, a CR in a word, a backslash-newline join, a split
// here-document operator, code bash evaluates later).
//
// Each line runs in a scratch copy of its repository. The git stub runs
// every git command for real there, changes included (a tag moved, a
// stash, a commit), and notes when its output holds the planted value;
// readers log their arguments and note when their input is a file that
// holds it (cat < pbip/x); make and npm rewrite the protected file, as a
// formatter would. The gate decides against the repository as it was.
//
// Each line runs in one of four repositories, by the generator: the
// finding in the working tree, in history only, staged only, or in HEAD
// with its fix staged.
// PBIPX_DIFF_LINES sets how many lines (default 300); PBIPX_DIFF_SEED the
// seed. A failure prints the line alone.
func TestDifferentialBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs bash on a Unix system")
	}
	lines := 300
	if n, err := strconv.Atoi(os.Getenv("PBIPX_DIFF_LINES")); err == nil && n > 0 {
		lines = n
	}
	seed := uint64(1)
	if n, err := strconv.ParseUint(os.Getenv("PBIPX_DIFF_SEED"), 10, 64); err == nil {
		seed = n
	}
	roots := differentialRepos(t)
	stubs, logFile := differentialStubs(t, bash)
	scratch := t.TempDir()
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: a seeded, repeatable test generator
	ran := 0
	for i := 0; i < lines; i++ {
		line := differentialLine(rng)
		at := findingAt(rng.IntN(len(roots)))
		root := roots[at]
		for _, opts := range [][]string{nil, {"-O", "expand_aliases"}} {
			if !bashRunsDanger(t, bash, opts, root, scratch, stubs, logFile, line, at) {
				continue
			}
			ran++
			d := gate.Decide(gate.Event{Tool: gate.Shell, Command: line, Cwd: root}, gate.Options{})
			if d.Allow {
				t.Errorf("bash runs a dangerous command in this line, the finding %s, and the gate allows it: %q", at, line)
			}
			break
		}
	}
	// The harness itself: some lines must really run a dangerous command.
	if ran < lines/10 {
		t.Errorf("bash ran a dangerous command in %d of %d lines: the stubs are not run", ran, lines)
	}
	t.Logf("bash ran a dangerous command in %d of %d lines", ran, lines)
}

// differentialRepos makes the four repositories, indexed by findingAt.
func differentialRepos(t *testing.T) []string {
	t.Helper()
	tree := newRepo(t, leakyFiles())
	history := newRepo(t, leakyFiles())
	if err := os.Remove(filepath.Join(history, filepath.FromSlash(leakVisual))); err != nil {
		t.Fatal(err)
	}
	gitIn(t, history, "commit", "--quiet", "--all", "--message", "Remove the slicer")
	staged := newRepo(t, cleanFiles())
	write(t, staged, leakVisual, slicer("student", "full_name", sentinel))
	gitIn(t, staged, "add", "--all")
	gitIn(t, staged, "stash", "--quiet")
	gitIn(t, staged, "stash", "apply", "--quiet", "--index")
	fixed := newRepo(t, leakyFiles())
	write(t, fixed, leakVisual, slicer("enrollment", "cohort_year", "2150"))
	gitIn(t, fixed, "add", "--all")
	return []string{inTree: tree, inHistory: history, inIndex: staged, inFixedHead: fixed}
}

// differentialStubs writes the stub programs into a directory of their own.
// Each logs its name and arguments to the run's log (PBIPX_DIFF_LOG; a
// new one each run). The readers (grep, cat, head ...) do nothing else,
// but note "stdin-leak" when their input is a file holding the planted
// value; git runs real git with its arguments and notes "git-leak" when
// its output holds it; make and npm reformat the protected file as it is, its
// content kept. sh, bash, env, xargs and a few wrappers are links
// to the real ones, so what they run is the stubs too.
func differentialStubs(t *testing.T, bash string) (dir, logFile string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("needs git")
	}
	realGrep, err := exec.LookPath("grep")
	if err != nil {
		t.Skip("needs grep")
	}
	dir = t.TempDir()
	logFile = filepath.Join(t.TempDir(), "log")
	logArgs := "{ printf '%s' \"${0##*/}\"; for a in \"$@\"; do printf '\\037%s' \"$a\"; done; printf '\\036'; } >> \"$PBIPX_DIFF_LOG\"\n"
	reader := "#!/bin/sh\n" + logArgs + "if [ -f /dev/stdin ] && '" + realGrep + "' -qF '" + sentinel + "' </dev/stdin; then\n" +
		"\tprintf 'stdin-leak\\036' >> \"$PBIPX_DIFF_LOG\"\nfi\n"
	for _, name := range []string{"grep", "egrep", "rg", "cat", "head", "tail", "sed", "jq", "python3", "ls", "wc", "sort"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(reader), 0o700); err != nil { //nolint:gosec // G306: a test's own stub
			t.Fatal(err)
		}
	}
	// git prints the names it lists (ls-files, --name-only), for a runner
	// to feed on: the content it prints goes to the leak note only.
	gitStub := "#!/bin/sh\n" + logArgs + `out=$('` + realGit + `' --no-pager -c core.pager=cat "$@" </dev/null 2>&1)
case " $* " in *" ls-files "*|*" --name-only "*) printf '%s\n' "$out" ;; esac
if printf '%s' "$out" | '` + realGrep + `' -qF '` + sentinel + `'; then
	printf 'git-leak\036' >> "$PBIPX_DIFF_LOG"
fi
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(gitStub), 0o700); err != nil { //nolint:gosec // G306: a test's own stub
		t.Fatal(err)
	}
	// A formatter: the protected file as it is, its content kept, laid
	// out anew on one line. It writes only what the file holds: it puts no
	// value there that was not, but what a stash or a checkout put back is
	// what it rewrites.
	rewrite := "#!/bin/sh\n" + logArgs + "f='" + leakVisual + "'\n" +
		"[ -f \"$f\" ] || exit 0\n" +
		"c=$(while IFS= read -r l || [ -n \"$l\" ]; do printf '%s ' \"$l\"; done < \"$f\")\n" +
		"printf '%s\\n' \"$c\" > \"$f\"\n"
	for _, name := range []string{"make", "npm"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(rewrite), 0o700); err != nil { //nolint:gosec // G306: a test's own stub
			t.Fatal(err)
		}
	}
	for _, name := range []string{"sh", "env", "xargs", "nohup", "timeout", "true", "false", "sleep", "mkdir", "find", "parallel", "cut"} {
		if real, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(real, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Symlink(bash, filepath.Join(dir, "bash")); err != nil {
		t.Fatal(err)
	}
	return dir, logFile
}

// differentialRuns counts the lines run, for their logs' and copies' names.
var differentialRuns int

// bashRunsDanger runs line under bash with opts, in a scratch copy of
// root under scratch, with only the stubs on PATH, and reports whether it
// ran a dangerous command.
func bashRunsDanger(t *testing.T, bash string, opts []string, root, scratch, stubs, logFile, line string, at findingAt) bool {
	t.Helper()
	// A log and a copy of the repository of its own for each run: a
	// process a line left running writes into its own run's.
	differentialRuns++
	run := strconv.Itoa(differentialRuns)
	logFile += "." + run
	if err := os.WriteFile(logFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(logFile) }()
	// Only git, make and npm change anything: a line with none of them
	// runs in the repository itself.
	copyDir := root
	if strings.Contains(line, "git") || strings.Contains(line, "make") || strings.Contains(line, "npm") {
		copyDir = filepath.Join(scratch, run)
		if out, err := exec.Command("cp", "-a", root, copyDir).CombinedOutput(); err != nil { //nolint:gosec // G204: the test's own paths
			t.Fatalf("cp: %v: %s", err, out)
		}
		defer func() { _ = os.RemoveAll(copyDir) }()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := append(append([]string{"--norc", "--noprofile"}, opts...), "-c", line)
	cmd := exec.CommandContext(ctx, bash, args...) //nolint:gosec // G204: the test runs bash on its own generated lines
	cmd.Dir = copyDir
	cmd.Env = []string{
		"PATH=" + stubs, "HOME=" + t.TempDir(), "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"PBIPX_DIFF_LOG=" + logFile,
	}
	_ = cmd.Run()                     // a line may fail; what it ran is in the log
	data, err := os.ReadFile(logFile) //nolint:gosec // G304: the test's own log
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range strings.Split(string(data), "\x1e") {
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x1f")
		if dangerous(f[0], f[1:], at) {
			return true
		}
	}
	return false
}

// dangerous reports whether running name with args reads the planted
// finding, where it is: git printing it (the git stub's "git-leak"), a
// reader given a file holding it as its input ("stdin-leak"), and, while
// it is in the working tree, a recursive grep, rg (which recurses by
// default), or a read of a file under the protected root.
func dangerous(name string, args []string, at findingAt) bool {
	has := func(want ...string) bool {
		for _, a := range args {
			for _, w := range want {
				if a == w || strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && len(w) == 2 && strings.Contains(a[1:], w[1:]) {
					return true
				}
			}
		}
		return false
	}
	switch name {
	case "git-leak", "stdin-leak":
		return true
	case "grep", "egrep":
		return at.inWorktree() && has("-r", "-R", "--recursive")
	case "rg":
		return at.inWorktree()
	case "cat", "head", "tail", "sed", "jq":
		return at.inWorktree() && slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(strings.TrimPrefix(a, "./"), "pbip/") })

	}
	return false
}

// The grammar: a payload, dangerous or not, inside up to three wrappers.
var (
	differentialPayloads = []string{
		"grep -r Doe .", "rg Doe", "cat " + leakVisual, "git --no-pager log -p", "git log -p -1",
		"{grep,} -r Doe .", "{rg,} Doe", "grep -n Doe README.md", "echo Doe", "git log --oneline -1",
		"cat README.md", "ls pbip",
		"git show", "git diff --cached", "git stash show -p", "git cat-file -p :" + leakVisual,
		"git show --stat", "git diff --stat", "git stash list",
		// A command word bash expands.
		"$(echo git) log -p", "${G:-git} show", "g=git; $g diff --cached",
		// xargs adding words git is given.
		"echo -p | xargs git log", "echo :" + leakVisual + " | xargs git cat-file -p",
		"echo origin | xargs git log --oneline -1",
		// What the index holds, and the working tree after a formatter.
		"git show :" + leakVisual, "git grep --cached Doe", "git status -v", "git diff-index -p --cached HEAD",
		"git grep -n Doe", "make fmt && git diff", "npm run fmt; git diff HEAD", "make && git diff --stat",
		"make && git diff --cached",
		"git diff-files -p", "make fmt && git diff-files -p", "make && git diff-index -p HEAD",
		"git stash && make fmt && git diff", "git stash; npm run fmt; git diff", "cat < " + leakVisual,
		"git tag -f t HEAD~1 && git diff t", "git add -A && git diff --cached",
		// The working-tree rule's proof: status ignores its pathspec, and
		// an option's value is no pathspec.
		"make fmt && git status -vv -- README.md", "make fmt && git diff -I x README.md",
		"make fmt && git diff --line-prefix=x README.md", "make fmt && git diff -- README.md",
		"git stash -u && npm run format && git diff",
		// Scripts fed to a shell through echo -e and printf escapes.
		`echo -e 'git log \x2dp' | bash`, `echo -e 'git log \055p' | bash`,
		`printf 'git log \x2dp\n' | bash`, `printf '%b\n' 'git log -\0160' | bash`,
		`echo -ne 'git log -\x70' | bash`, `echo -e 'echo \"; git log -p; echo \"' | bash`,
		`echo -e 'git log -p\c' | bash`, `printf '%s\n' 'git log --oneline -1' | bash`,
	}
	differentialWrappers = []string{
		"%s",
		"echo $(%s)",
		"echo \"$(%s)\"",
		"echo `%s`",
		"echo $(echo ${x/) #/}; %s)",
		"echo ${x:-$(echo ${y/)/}; %s)}",
		"echo $(case esac in a|esac) %s;; esac)",
		"case x in (esac|*) %s;; esac",
		"case x in x) %s;; esac",
		"cat <<$'E'\nx\nE\n%s",
		"cat <<$\"E\"\nx\nE\n%s",
		"cat <<'E'\n%s\nE",
		"cat <<E\nx\nE\n%s",
		"((x<<true))\n%s\ntrue",
		"echo $[1<<2]\n%s\n2]",
		"echo $((1<<2)); %s",
		"if true; then %s; fi",
		"{ %s; }",
		"( %s )",
		"true && %s",
		"false || %s",
		"! %s",
		"time %s",
		"time { %s; }",
		"time -p ! %s",
		"for i in 1; do %s; done",
		"f() { %s; }; f",
		"echo x | %s",
		"x=$(%s)",
		"bash -c '%s'",
		"sh -c \"%s\"",
		"eval '%s'",
		"alias c='%s'\nc",
		"echo case x in; (%s)",
		"%s {fd}>/dev/null",
		"echo '%s'",
		"# %s",
		": '%s'",
		// source and . of a process substitution.
		"source <(echo '%s')",
		". <(echo '%s')",
		// CR and backslash-newline.
		"true\r\n%s",
		"echo x\r; %s",
		"%s \\\n",
		"echo x;\\\n%s",
		"true &\\\n& %s",
		"ec\\\nho $(%s)",
		// A git command that moves a ref first.
		"git tag -f t HEAD; %s",
		"git fetch . HEAD:refs/heads/t && %s",
		// A here-document in $(...) with a line starting with the
		// delimiter (bash 5.2 ends it there).
		"x=$(cat <<'E'\nmsg\nE )\n%s\nE\n)",
		// Code bash evaluates later.
		"PS4='$(%s >&2)'; set -x; true",
		"v='a[$(%s)]'; echo $((v))",
		"test -v 'a[$(%s)]'",
		"BASH_ENV=<(echo '%s') bash -c true",
		// Operators split by a backslash-newline.
		"cat <\\\n<'E'\nx\\\nE\n%s\nE",
		"false |\\\n| %s",
		"echo x >\\\n> /dev/null; %s",
		"cat <\\\n(%s)",
		"echo $\\\n(%s)",
		// A continuation before a redirection, which joins no operator.
		"%s \\\n> /dev/null",
		// The payload fed to a shell through echo -e and printf %b.
		"echo -e \"%s\" | bash",
		"printf '%b\\n' \"%s\" | bash",
	}
)

// differentialLine builds one line from the grammar.
func differentialLine(rng *rand.Rand) string {
	line := differentialPayloads[rng.IntN(len(differentialPayloads))]
	for range rng.IntN(3) + 1 {
		w := differentialWrappers[rng.IntN(len(differentialWrappers))]
		if strings.Contains(w, "'") && strings.Contains(line, "'") {
			continue
		}
		if strings.Contains(w, "\"%s\"") && strings.Contains(line, "\"") {
			continue
		}
		line = strings.Replace(w, "%s", line, 1)
	}
	return line
}
