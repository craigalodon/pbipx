package gate_test

import (
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyFedProgramNames checks that a program a runner feeds (xargs,
// parallel) gets words the gate cannot see, so with a copy registered it
// touches every copy, unless the pipeline's producer is provably confined to
// places that are not, hold or lie inside one.
func TestWorkingCopyFedProgramNames(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	out := bashPath(filepath.Join(m.outside, "out"))
	m.run(t, []wcCase{
		// Names a producer prints that hold a copy.
		{"xargs cp -rt", "", wcShell("echo ~ | xargs cp -rt " + out), wc},
		{"xargs mv -t", "", wcShell("echo ~ | xargs mv -t " + out), wc},
		{"xargs -n1 mv -t", "", wcShell("echo ~ | xargs -n1 mv -t " + out), wc},
		{"xargs -r mv -t", "", wcShell("echo ~ | xargs -r mv -t " + out), wc},
		{"xargs -0 mv -t", "", wcShell("echo ~ | xargs -0 mv -t " + out), wc},
		{"xargs ln -st", "", wcShell("echo ~ | xargs ln -st " + out), wc},
		{"xargs install -t", "", wcShell("echo ~ | xargs install -t " + out), wc},
		{"parallel cp -rt", "", wcShell("echo ~ | parallel cp -rt " + out), wc},
		{"env xargs", "", wcShell("echo ~ | env xargs cp -rt " + out), wc},
		{"sudo xargs", "", wcShell("echo ~ | sudo xargs cp -rt " + out), wc},
		{"xargs cat", "", wcShell("echo ~ | xargs cat"), wc},
		{"printf", "", wcShell("printf '%s\\n' ~ | xargs cat"), wc},
		{"echo of the folder holding it", "", wcShell("echo ~/work | xargs cp -rt " + out), wc},
		{"echo of a path in it", "", wcShell("echo ~/work/copy/x | xargs cat"), wc},
		{"echo, two words", "", wcShell("echo a ~ | xargs cat"), wc},
		{"through sort", "", wcShell("echo ~ | sort | xargs cat"), wc},
		{"through head and uniq", "", wcShell("echo ~ | uniq | head -n1 | xargs cat"), wc},
		{"through grep", "", wcShell("echo ~ | grep home | xargs cat"), wc},
		{"nested in sh -c", "", wcShell("sh -c 'echo ~ | xargs cat'"), wc},
		{"a placeholder", "", wcShell("echo ~ | xargs -I{} cp -r {} " + out), wc},
		{"ls of a folder holding it", "", wcShell("ls ~/work | xargs cat"), wc},
		{"find of a folder holding it", "", wcShell("find ~ -name '*.abf' | xargs cat"), wc},
		{"git ls-files from above it", m.ctl.home, wcShell("git ls-files | xargs cat"), wc},
		{"git -C above it", "", wcShell("git -C ~ ls-files | xargs cat"), wc},
		{"echo with a quote xargs reads", "", wcShell(`echo "'` + bashPath(m.wc) + `'" | xargs cat`), wc},
		{"echo with a space", "", wcShell(`echo "a ` + bashPath(m.wc) + `" | xargs cat`), wc},
		{"echo a word it cannot tell", "", wcShell(`echo "$X" | xargs cat`), wc},
		{"echo a glob", "", wcShell(`echo ~/* | xargs cat`), wc},

		// A producer that is not provably confined.
		{"a file on stdin", "", wcShell("xargs cat < L"), wc},
		{"no pipe", "", wcShell("xargs cat"), wc},
		{"cat of a list", "", wcShell("cat L | xargs cat"), wc},
		{"a here-string", "", wcShell("xargs cat <<< x"), wc},
		{"cut", "", wcShell("echo x | cut -d/ -f1 | xargs cat"), wc},
		{"cut after git", "", wcShell("git ls-files | cut -c4- | xargs cat"), wc},
		{"sed", "", wcShell("git ls-files | sed -n p | xargs cat"), wc},
		{"awk", "", wcShell("git ls-files | awk '{print}' | xargs cat"), wc},
		{"grep -o", "", wcShell("git ls-files | grep -o 'x.*' | xargs cat"), wc},
		{"grep -r", "", wcShell("echo a | grep -r a | xargs cat"), wc},
		{"sort of a file", "", wcShell("git ls-files | sort L | xargs cat"), wc},
		{"head -c", "", wcShell("git ls-files | head -c 5 | xargs cat"), wc},
		{"head of a file", "", wcShell("git ls-files | head L | xargs cat"), wc},
		{"an input redirect on a filter", "", wcShell("git ls-files | sort < L | xargs cat"), wc},
		{"an input redirect on the runner", "", wcShell("git ls-files | xargs cat < L"), wc},
		{"a group", "", wcShell("{ cat L; git ls-files; } | xargs cat"), wc},
		{"find -printf", "", wcShell("find src -printf '%h\\n' | xargs cat"), wc},
		{"find -L", "", wcShell("find -L src | xargs cat"), wc},
		{"find -exec", "", wcShell("find src -exec cat {} ';' | xargs cat"), wc},
		{"fd -x", "", wcShell("fd -x cat | xargs cat"), wc},
		{"ls -R", "", wcShell("ls -R src | xargs cat"), wc},
		{"git diff with a patch", "", wcShell("git diff | xargs cat"), wc},
		{"git show", "", wcShell("git show | xargs cat"), wc},
		{"git with an alias", "", wcShell("git -c alias.x=y ls-files | xargs cat"), wc},
		{"a program", "", wcShell("frobnicate | xargs cat"), wc},
		{"a second runner", "", wcShell("echo a | xargs -n1 echo | xargs cat"), wc},
		{"parallel :::: file", "", wcShell("echo a | parallel cat :::: L"), wc},
		{"parallel -a file", "", wcShell("echo a | parallel -a L cat"), wc},
		{"xargs -a file", "", wcShell("echo a | xargs -a L cat"), wc},

		// find -exec and a runner give a copier no quiet word.
		{"cp -t into the folder above, fed", "", wcShell("echo a | xargs cp -t ~ "), wc},
		{"mv into ~ under xargs", "", wcShell("git ls-files | xargs mv -t ~"), wc},
		{"cp into ~ under xargs", "", wcShell("git ls-files | xargs cp -t ~"), wc},
		{"cp into ~ under find -exec +", "", wcShell("find src -exec cp -t ~ {} +"), wc},
		{"mv into ~ under find -exec", "", wcShell("find src -exec mv {} ~ ';'"), wc},

		// Allowed: a confined producer, with a copy registered elsewhere.
		{"git ls-files, gofmt", "", wcShell("git ls-files | xargs gofmt -l"), ""},
		{"git ls-files, outside", "", wcShell("git ls-files | xargs gofmt -l"), ""},
		{"git diff --name-only", "", wcShell("git diff --name-only | xargs gofmt -l"), ""},
		{"git status --porcelain", "", wcShell("git status --porcelain | xargs gofmt -l"), ""},
		{"git ls-files, sorted", "", wcShell("git ls-files | sort | uniq | head -n 20 | xargs wc -l"), ""},
		{"git ls-files, grep", "", wcShell("git ls-files | grep '[.]go$' | xargs gofmt -l"), ""},
		{"git ls-files, grep -v -e", "", wcShell("git ls-files | grep -v -e vendor | xargs gofmt -l"), ""},
		{"git ls-files, tail", "", wcShell("git ls-files | tail -n 3 | xargs gofmt -l"), ""},
		{"git ls-files, xargs -0", "", wcShell("git ls-files -z | xargs -0 gofmt -l"), ""},
		{"git ls-files, a placeholder", "", wcShell("git ls-files | xargs -I{} gofmt -l {}"), ""},
		{"find src", "", wcShell("find src -name '*.go' | xargs wc -l"), ""},
		{"find of the outside folder", "", wcShell("find " + bashPath(m.outside) + " -name '*.txt' -print0 | xargs -0 wc -l"), ""},
		{"fd", "", wcShell("fd -e go | xargs wc -l"), ""},
		{"ls of the outside folder", m.outside, wcShell("ls | head | xargs wc -l"), ""},
		{"ls src", "", wcShell("ls src | xargs wc -l"), ""},
		{"printf", "", wcShell("printf 'a\\n' | xargs echo"), ""},
		{"echo of a sibling", "", wcShell("echo " + bashPath(m.sibling) + " | xargs ls"), ""},
		{"echo plain words", "", wcShell("echo a b c | xargs -n1 wc -c"), ""},
		{"parallel with a literal source", "", wcShell("parallel cat ::: a b"), ""},
		{"a plain pipe", "", wcShell("ls | head"), ""},
		{"grep -f", "", wcShell("grep -f pats file"), ""},
		{"a build", "", wcShell("go build ./..."), ""},
		{"a test", "", wcShell("go test ./..."), ""},
		{"cp into ~, no runner", "", wcShell("cp f ~/"), ""},
		{"find -exec ;", "", wcShell("find src -exec cat {} ';'"), ""},
	})
}
