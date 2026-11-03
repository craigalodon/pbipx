package gate_test

import (
	"context"
	"math/rand/v2"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestPrintedMatchesBash checks the gate's reading of what echo -e and
// printf print against bash's own output: whenever the gate claims to
// know the text (ok), it must be exactly what bash prints, or the gate
// could feed a shell something other than what it read. Where the gate is
// unsure (ok false), nothing is claimed. The arguments carry the escapes
// the reviews turned up (quotes, hex, octal, \c, \0NNN) and random ones.
func TestPrintedMatchesBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs bash on a Unix system")
	}
	seed := uint64(1)
	if n, err := strconv.ParseUint(os.Getenv("PBIPX_DIFF_SEED"), 10, 64); err == nil {
		seed = n
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: a seeded, repeatable test generator
	pieces := []string{`a`, `-p`, `\"`, `\'`, `\\`, `\n`, `\t`, `\x2d`, `\055`, `\0160`, `\c`, `\e`, `\z`, `;`, ` `, `%s`, `%b`, `%%`, `%d`}
	cases := [][]string{
		{"echo", "-e", `echo \"; cmd; echo \"`},
		{"echo", "-e", `git log \x2dp`},
		{"echo", "-ne", `x\055p`},
		{"printf", `%b\n`, `echo \"; cmd`},
		{"printf", `%b\n`, `x-\0160`},
		{"printf", `a\x2db\n`},
		{"printf", `echo \"hi\"\n`},
	}
	for i := 0; i < 2000; i++ {
		name := "echo"
		var args []string
		if rng.IntN(2) == 0 {
			if rng.IntN(2) == 0 {
				args = append(args, "-e")
			}
			if rng.IntN(2) == 0 {
				args = append(args, "-ne")
			}
		} else {
			name = "printf"
		}
		n := rng.IntN(3) + 1
		for j := 0; j < n; j++ {
			var w strings.Builder
			for k := rng.IntN(4); k >= 0; k-- {
				w.WriteString(pieces[rng.IntN(len(pieces))])
			}
			args = append(args, w.String())
		}
		cases = append(cases, append([]string{name}, args...))
	}
	// The shells whose echo the gate may be reading, and which of them it
	// claims to read exactly: bash alone when it knows the shell is bash;
	// with mayEscape, bash with xpg_echo and, for an echo with no option
	// (sh's echo has no -e), the system sh as well, and zsh when there is
	// one.
	type shell struct {
		name string
		argv []string
		echo bool // only for echo with no option
	}
	shells := []shell{{"bash", []string{bash, "--norc", "--noprofile", "-c"}, false}}
	xpg := shell{"bash xpg_echo", []string{bash, "--norc", "--noprofile", "-O", "xpg_echo", "-c"}, false}
	others := []shell{xpg}
	if sh, err := exec.LookPath("sh"); err == nil {
		others = append(others, shell{"sh", []string{sh, "-c"}, true})
	}
	if zsh, err := exec.LookPath("zsh"); err == nil {
		others = append(others, shell{"zsh", []string{zsh, "-f", "-c"}, true})
	}
	for _, c := range cases {
		name, args := c[0], c[1:]
		plain := name == "echo" && (len(args) == 0 || !strings.HasPrefix(args[0], "-"))
		for _, mayEscape := range []bool{false, true} {
			got, ok := gate.Printed(name, args, mayEscape)
			if !ok {
				continue
			}
			against := shells
			if mayEscape {
				against = slices.Concat(shells, others)
			}
			for _, sh := range against {
				if sh.echo && !plain && name == "echo" {
					continue
				}
				if sh.echo && name != "echo" {
					continue // printf is compared with bash's
				}
				want, runOK := shellPrints(t, sh.argv, name, args)
				if runOK && got != want {
					t.Errorf("%s %q (mayEscape %v): gate read %q, %s prints %q", name, args, mayEscape, got, sh.name, want)
				}
			}
		}
	}
}

// shellPrints runs name with args under the shell argv (ending in -c) and
// returns what it printed. runOK is false when the shell itself failed (a
// bad format), so the line is skipped.
func shellPrints(t *testing.T, argv []string, name string, args []string) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], append(slices.Concat(argv[1:], []string{name + ` "$@"`, "sh"}), args...)...) //nolint:gosec // G204: the test's own words
	cmd.Env = []string{"LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}
