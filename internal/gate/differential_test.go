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
	write(t, staged, leakVisual, slicer("student", "full_name", "Doe, Jane"))
	gitIn(t, staged, "add", "--all")
	gitIn(t, staged, "stash", "--quiet")
	gitIn(t, staged, "stash", "apply", "--quiet", "--index")
	return []string{inTree: tree, inHistory: history, inIndex: staged}
}

// differentialStubs writes the stub programs into a directory of their own:
// grep, rg, cat, git and other readers log their name and arguments to
// logFile and do nothing else; sh, bash, env, xargs and a few wrappers are
// links to the real ones, so what they run is the stubs too.
func differentialStubs(t *testing.T, bash string) (dir, logFile string) {
	t.Helper()
	dir = t.TempDir()
	logFile = filepath.Join(t.TempDir(), "log")
	script := "#!/bin/sh\n{ printf '%s' \"${0##*/}\"; for a in \"$@\"; do printf '\\037%s' \"$a\"; done; printf '\\036'; } >> '" +
		logFile + "'\n"
	for _, name := range []string{"grep", "egrep", "rg", "cat", "git", "head", "tail", "sed", "find", "python3", "ls", "wc", "sort"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil { //nolint:gosec // G306: a test's own stub
			t.Fatal(err)
		}
	}
	for _, name := range []string{"sh", "env", "xargs", "nohup", "timeout", "true", "false", "sleep"} {
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

// bashRunsDanger runs line under bash with opts, in root, with only the
// stubs on PATH, and reports whether it ran a dangerous command.
func bashRunsDanger(t *testing.T, bash string, opts []string, root, stubs, logFile, line string, at findingAt) bool {
	t.Helper()
	if err := os.WriteFile(logFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := append(append([]string{"--norc", "--noprofile"}, opts...), "-c", line)
	cmd := exec.CommandContext(ctx, bash, args...) //nolint:gosec // G204: the test runs bash on its own generated lines
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + stubs, "HOME=" + t.TempDir(), "LC_ALL=C"}
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
// finding, where it is: a recursive grep, rg (which recurses by default),
// or cat of a file under the protected root while it is in the working
// tree; git log -p and git show while history holds it; git diff --cached,
// git stash show -p and git cat-file -p of its index entry while it is
// staged only (the index has it in the tree repository too).
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
	case "grep", "egrep":
		return at != inHistory && has("-r", "-R", "--recursive")
	case "rg":
		return at != inHistory
	case "cat", "head", "tail", "sed":
		return at != inHistory && slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "pbip/") })
	case "git":
		sub := ""
		if i := slices.IndexFunc(args, func(a string) bool { return !strings.HasPrefix(a, "-") }); i >= 0 {
			sub = args[i]
		}
		switch sub {
		case "log":
			return at != inIndex && has("-p", "--patch")
		case "show":
			return at != inIndex && !has("-s", "--no-patch", "--stat", "--oneline", "--name-only")
		case "diff":
			return at == inIndex && has("--cached", "--staged")
		case "stash":
			return at == inIndex && slices.Contains(args, "show") && has("-p", "--patch")
		case "cat-file":
			return at != inHistory && has("-p") && slices.Contains(args, ":"+leakVisual)
		}
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
