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

// A planted finding is in one of three places: in the working tree (and
// committed, HEAD adding it), in history only (HEAD deletes it), or staged
// only (in the index and a stash, HEAD clean). What is dangerous to run
// depends on which.
type findingAt int

const (
	inTree findingAt = iota
	inHistory
	inIndex
)

func (f findingAt) String() string {
	return [...]string{"in the working tree", "in history only", "staged only"}[f]
}

// sentinel is the planted value, which git's output is searched for.
const sentinel = "Doe, Jane"

// TestDifferentialBash runs generated command lines under real bash, with
// stub programs that only log the arguments they are run with, and checks
// that whenever bash really runs a dangerous command (a recursive search
// of the working directory, a cat of a protected file, git printing the
// finding) in a repository with a planted finding, the gate denies the
// line. It reads each line as bash does or fails closed; it catches the
// tokenizer mistakes the reviews found (a ) inside ${...} in $(...), a
// case pattern in $(...), a here-document delimiter quoted with $'...',
// << in arithmetic, an empty brace alternative, a CR in a word, a
// backslash-newline join, code bash evaluates later).
//
// Whether a git command is dangerous is asked of git itself: the git stub
// runs a reading subcommand again with real git, when it is run, and
// notes when its output holds the planted value. The stub make (and npm)
// rewrites the protected file, as a formatter would, so the git commands
// after it see what it wrote; the file is put back after each line,
// before the gate decides.
//
// Each line runs in one of three repositories, by the generator: the
// finding in the working tree, in history only, or staged only.
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
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: a seeded, repeatable test generator
	ran := 0
	for i := 0; i < lines; i++ {
		line := differentialLine(rng)
		at := findingAt(rng.IntN(len(roots)))
		root := roots[at]
		for _, opts := range [][]string{nil, {"-O", "expand_aliases"}} {
			if !bashRunsDanger(t, bash, opts, root, stubs, logFile, line, at) {
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

// differentialRepos makes the three repositories, indexed by findingAt.
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
	return []string{inTree: tree, inHistory: history, inIndex: staged}
}

// differentialStubs writes the stub programs into a directory of their own:
// grep, rg, cat, git and other readers log their name and arguments to
// a log (logFile, a new one each run, named in PBIPX_DIFF_LOG) and do
// nothing else, but git, which runs a reading subcommand
// with real git too and logs "git-leak" when it prints the planted value;
// make and npm log and rewrite the protected file, the planted value in
// it; sh, bash, env, xargs and a few wrappers are links to the real ones,
// so what they run is the stubs too.
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
	script := "#!/bin/sh\n{ printf '%s' \"${0##*/}\"; for a in \"$@\"; do printf '\\037%s' \"$a\"; done; printf '\\036'; } >> \"$PBIPX_DIFF_LOG\"\n"
	for _, name := range []string{"grep", "egrep", "rg", "cat", "head", "tail", "sed", "find", "python3", "ls", "wc", "sort"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil { //nolint:gosec // G306: a test's own stub
			t.Fatal(err)
		}
	}
	gitStub := script + `sub=
for a in "$@"; do case $a in -*) ;; *) sub=$a; break ;; esac; done
run=
case $sub in
log|show|diff|grep|status|cat-file|diff-index|diff-tree|diff-files|blame|whatchanged) run=1 ;;
stash) case " $* " in *" show "*|*" list "*) run=1 ;; esac ;;
esac
if [ -n "$run" ] && '` + realGit + `' --no-pager -c core.pager=cat "$@" </dev/null 2>&1 | '` + realGrep + `' -qF '` + sentinel + `'; then
	printf 'git-leak\036' >> "$PBIPX_DIFF_LOG"
fi
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(gitStub), 0o700); err != nil { //nolint:gosec // G306: a test's own stub
		t.Fatal(err)
	}
	// A formatter: the slicer, its value kept, laid out anew.
	formatted := filepath.Join(t.TempDir(), "formatted.json")
	if err := os.WriteFile(formatted, []byte(strings.ReplaceAll(slicer("student", "full_name", sentinel), "\n", " ")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rewrite := script + "mkdir -p '" + path0(leakVisual) + "' && while IFS= read -r l; do printf '%s\\n' \"$l\"; done < '" +
		formatted + "' > '" + leakVisual + "'\n"
	for _, name := range []string{"make", "npm"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(rewrite), 0o700); err != nil { //nolint:gosec // G306: a test's own stub
			t.Fatal(err)
		}
	}
	for _, name := range []string{"sh", "env", "xargs", "nohup", "timeout", "true", "false", "sleep", "mkdir"} {
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

// differentialRuns counts the lines run, for their logs' names.
var differentialRuns int

// path0 is the directory of a slash-separated path.
func path0(p string) string { return p[:strings.LastIndexByte(p, '/')] }

// bashRunsDanger runs line under bash with opts, in root, with only the
// stubs on PATH, and reports whether it ran a dangerous command. The
// protected file is put back as it was before it returns.
func bashRunsDanger(t *testing.T, bash string, opts []string, root, stubs, logFile, line string, at findingAt) bool {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(leakVisual))
	before, readErr := os.ReadFile(file) //nolint:gosec // G304: the test's own fixture
	defer func() {
		if readErr != nil {
			_ = os.Remove(file)
			return
		}
		if err := os.WriteFile(file, before, 0o600); err != nil { //nolint:gosec // G703: the test's own fixture path
			t.Fatal(err)
		}
	}()
	// A log of its own for each run: a process a line left running writes
	// into its own run's.
	differentialRuns++
	logFile += "." + strconv.Itoa(differentialRuns)
	if err := os.WriteFile(logFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(logFile) }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := append(append([]string{"--norc", "--noprofile"}, opts...), "-c", line)
	cmd := exec.CommandContext(ctx, bash, args...) //nolint:gosec // G204: the test runs bash on its own generated lines
	cmd.Dir = root
	cmd.Env = []string{
		"PATH=" + stubs, "HOME=" + t.TempDir(), "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
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
// finding, where it is: git printing it (the git stub's "git-leak"), and,
// while it is in the working tree, a recursive grep, rg (which recurses
// by default), or cat of a file under the protected root.
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
	case "git-leak":
		return true
	case "grep", "egrep":
		return at != inHistory && has("-r", "-R", "--recursive")
	case "rg":
		return at != inHistory
	case "cat", "head", "tail", "sed":
		return at != inHistory && slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "pbip/") })
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
		"x='a[$(%s)]'; echo $((x))",
		"test -v 'a[$(%s)]'",
		"BASH_ENV=<(echo '%s') bash -c true",
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
