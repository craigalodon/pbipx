package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyFedBySearch checks rg -l, rg --files and grep -rl as
// producers of names for a runner: confined by an option table, with their
// paths placed as find's are.
func TestWorkingCopyFedBySearch(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	home := m.ctl.home
	m.run(t, []wcCase{
		// Allowed: a folder with no copy in or above it.
		{"rg -l", "", wcShell("rg -l TODO src | xargs wc -l"), ""},
		{"rg -l, default folder", "", wcShell("rg -l TODO | xargs wc -l"), ""},
		{"rg --files-with-matches", "", wcShell("rg --files-with-matches TODO src | xargs wc -l"), ""},
		{"rg --files", "", wcShell("rg --files src | xargs wc -l"), ""},
		{"rg --files, default", "", wcShell("rg --files | xargs wc -l"), ""},
		{"rg -il", "", wcShell("rg -il todo src | xargs wc -l"), ""},
		{"rg -l -g -t", "", wcShell("rg -l -g '*.go' -t go TODO src | xargs wc -l"), ""},
		{"rg -l -e", "", wcShell("rg -l -e TODO -e FIXME src | xargs wc -l"), ""},
		{"rg -l -F -w -S --hidden", "", wcShell("rg -l -F -w -S --hidden TODO src | xargs wc -l"), ""},
		{"rg -l -0 xargs -0", "", wcShell("rg -l -0 TODO src | xargs -0 wc -l"), ""},
		{"rg -l of a sibling", "", wcShell("rg -l x " + bashPath(m.sibling) + " | xargs wc -l"), ""},
		{"grep -rl", "", wcShell("grep -rl TODO src | xargs wc -l"), ""},
		{"grep -r -l", "", wcShell("grep -r -l TODO src | xargs wc -l"), ""},
		{"grep -lr", "", wcShell("grep -lr TODO src | xargs wc -l"), ""},
		{"grep -rli", "", wcShell("grep -rli todo src | xargs wc -l"), ""},
		{"grep -rl default folder", "", wcShell("grep -rl TODO | xargs wc -l"), ""},
		{"grep -rl --include", "", wcShell("grep -rl --include='*.go' --exclude-dir=vendor TODO src | xargs wc -l"), ""},
		{"grep -rlF -e", "", wcShell("grep -rlF -e TODO src | xargs wc -l"), ""},
		{"grep -rlwE", "", wcShell("grep -rlwE 'a|b' src | xargs wc -l"), ""},
		{"through sort", "", wcShell("rg -l TODO src | sort | xargs wc -l"), ""},

		// Denied: a copy in, at or above the folder.
		{"rg -l of the home folder", "", wcShell("rg -l x ~ | xargs cat"), wc},
		{"rg -l of the folder holding the copy", "", wcShell("rg -l x ~/work | xargs cat"), wc},
		{"rg -l of the copy", "", wcShell("rg -l x " + bashPath(m.wc) + " | xargs cat"), wc},
		{"rg -l from above a copy", home, wcShell("rg -l x | xargs cat"), wc},
		{"rg --files from above a copy", home, wcShell("rg --files | xargs cat"), wc},
		{"grep -rl of the home folder", "", wcShell("grep -rl x ~ | xargs cat"), wc},
		{"grep -rl from above a copy", home, wcShell("grep -rl x | xargs cat"), wc},
		{"grep -r -l from above a copy", home, wcShell("grep -r -l x . | xargs cat"), wc},
		{"rg -l, a pattern in -e, a path after", "", wcShell("rg -l -e x ~ | xargs cat"), wc},

		// Denied: an option the table lacks, or no -l.
		{"rg -L -l", "", wcShell("rg -L -l x src | xargs cat"), wc},
		{"rg --follow -l", "", wcShell("rg --follow -l x src | xargs cat"), wc},
		{"rg --pre", "", wcShell("rg --pre cat -l x src | xargs cat"), wc},
		{"rg --pre=", "", wcShell("rg --pre=cat -l x src | xargs cat"), wc},
		{"rg -z", "", wcShell("rg -z -l x src | xargs cat"), wc},
		{"rg --search-zip", "", wcShell("rg --search-zip -l x src | xargs cat"), wc},
		{"rg without -l", "", wcShell("rg x src | xargs cat"), wc},
		{"rg --files-without-match", "", wcShell("rg --files-without-match x src | xargs cat"), wc},
		{"rg -c", "", wcShell("rg -c x src | xargs cat"), wc},
		{"rg --json", "", wcShell("rg --json -l x src | xargs cat"), wc},
		{"grep -Rl", "", wcShell("grep -Rl x src | xargs cat"), wc},
		{"grep -l without -r", "", wcShell("grep -l x f | xargs cat"), wc},
		{"grep -r without -l", "", wcShell("grep -r x src | xargs cat"), wc},
		{"grep -rL", "", wcShell("grep -rL x src | xargs cat"), wc},
		{"grep -rlf", "", wcShell("grep -rlf pats src | xargs cat"), wc},
		{"grep --exclude-from", "", wcShell("grep -rl --exclude-from=x y src | xargs cat"), wc},
		{"grep -rl, an unknown option", "", wcShell("grep -rl --weird x src | xargs cat"), wc},
		{"grep -rl, a word it cannot tell", "", wcShell(`grep -rl x "$D" | xargs cat`), wc},
	})
}
