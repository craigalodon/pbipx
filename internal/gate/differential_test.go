package gate_test

import (
	"context"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestDifferentialBash runs generated command lines under real bash, with
// stub programs that only log the arguments they are run with, and checks
// that whenever bash really runs a dangerous command (a recursive search
// of the working directory, a cat of a protected file, git log -p) in a
// repository with a planted finding, the gate denies the line. It reads
// each line as bash does or fails closed; it catches the tokenizer
// mistakes the reviews found (a ) inside ${...} in $(...), a case
// pattern in $(...), a here-document delimiter quoted with $'...', << in
// arithmetic, an empty brace alternative).
//
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
	root := newRepo(t, leakyFiles())
	stubs, logFile := differentialStubs(t, bash)
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: a seeded, repeatable test generator
	ran := 0
	for i := 0; i < lines; i++ {
		line := differentialLine(rng)
		for _, opts := range [][]string{nil, {"-O", "expand_aliases"}} {
			if !bashRunsDanger(t, bash, opts, root, stubs, logFile, line) {
				continue
			}
			ran++
			d := gate.Decide(gate.Event{Tool: gate.Shell, Command: line, Cwd: root}, gate.Options{})
			if d.Allow {
				t.Errorf("bash runs a dangerous command in this line, and the gate allows it: %q", line)
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
func bashRunsDanger(t *testing.T, bash string, opts []string, root, stubs, logFile, line string) bool {
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
		if dangerous(f[0], f[1:]) {
			return true
		}
	}
	return false
}

// dangerous reports whether running name with args reads the planted
// finding: a recursive grep, rg (which recurses by default), cat of a
// file under the protected root, or git log -p.
func dangerous(name string, args []string) bool {
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
		return has("-r", "-R", "--recursive")
	case "rg":
		return true
	case "cat", "head", "tail", "sed":
		for _, a := range args {
			if strings.HasPrefix(a, "pbip/") {
				return true
			}
		}
	case "git":
		return len(args) > 0 && hasWord(args, "log") && has("-p", "--patch")
	}
	return false
}

func hasWord(args []string, w string) bool {
	for _, a := range args {
		if a == w {
			return true
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
	}
)

// differentialLine builds one line from the grammar.
func differentialLine(rng *rand.Rand) string {
	line := differentialPayloads[rng.IntN(len(differentialPayloads))]
	for range rng.IntN(3) + 1 {
		w := differentialWrappers[rng.IntN(len(differentialWrappers))]
		if strings.Contains(w, "'%s'") && strings.Contains(line, "'") {
			continue
		}
		if strings.Contains(w, "\"%s\"") && strings.Contains(line, "\"") {
			continue
		}
		line = strings.Replace(w, "%s", line, 1)
	}
	return line
}
