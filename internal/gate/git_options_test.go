package gate

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// gitOptionsVersion is the git version gitLongOptions was written from.
// An older git lists fewer options; only a git this new or newer is held to
// list every option the tables have.
var gitOptionsVersion = [2]int{2, 53}

// TestGitLongOptionsMatchGit compares each list in gitLongOptions with the
// long options `git <subcommand> -h` prints. An option git lists that the
// table lacks is a failure on a git no newer than the tables: an
// abbreviation of it would not be expanded. On a newer git it is logged,
// for the tables to catch up, so that a CI runner's git upgrade does not
// fail unrelated changes. An option the table has that git does not list
// is a failure on a git at least as new as the tables: it can make an
// abbreviation git resolves look ambiguous. git config's table is checked
// by hand, since its -h lists its subcommands' options apart.
func TestGitLongOptionsMatchGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	init := exec.Command("git", "init", "-q", dir) //nolint:gosec // G204: the test runs git with fixed arguments
	init.Env = env
	if err := init.Run(); err != nil {
		t.Skip("git init failed")
	}
	strict := gitAtLeast(t, env, gitOptionsVersion)
	newer := gitAtLeast(t, env, [2]int{gitOptionsVersion[0], gitOptionsVersion[1] + 1})
	long := regexp.MustCompile(`--(\[no-\])?([a-z0-9][a-z0-9-]*)`)
	for key, opts := range gitLongOptions {
		if key == "config" {
			continue
		}
		cmd := exec.Command("git", append(strings.Fields(key), "-h")...) //nolint:gosec // G204: the subcommand comes from the table
		cmd.Dir, cmd.Env = dir, env
		out, _ := cmd.CombinedOutput() // -h exits 129
		listed := map[string]bool{}
		for _, m := range long.FindAllStringSubmatch(string(out), -1) {
			listed[m[2]] = true
		}
		if len(listed) == 0 {
			t.Errorf("git %s -h lists no long options", key)
			continue
		}
		table := map[string]bool{}
		for _, o := range opts {
			table[strings.TrimRight(o, "=?")] = true
		}
		for name := range listed {
			if !table[name] && !table[strings.TrimPrefix(name, "no-")] {
				if newer {
					t.Logf("git %s: --%s, new in this git, is not in the table", key, name)
				} else {
					t.Errorf("git %s: --%s is not in the table", key, name)
				}
			}
		}
		if !strict {
			continue
		}
		for name := range table {
			if !listed[name] {
				t.Errorf("git %s: the table has --%s, which git -h does not list", key, name)
			}
		}
	}
}

// gitAtLeast reports whether the git on PATH is at least version want.
func gitAtLeast(t *testing.T, env []string, want [2]int) bool {
	t.Helper()
	cmd := exec.Command("git", "version")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > want[0] || major == want[0] && minor >= want[1]
}
