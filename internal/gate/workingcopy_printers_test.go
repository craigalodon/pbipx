package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyFedPrinters checks that a runner whose program only prints
// its arguments gets no feed spot, whatever the input, while the words and
// redirections in the line are placed as usual.
func TestWorkingCopyFedPrinters(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	inCopy := bashPath(m.wc) + "/x.txt"
	m.run(t, []wcCase{
		{"xargs, no program", "", wcShell("cat files.txt | xargs"), ""},
		{"xargs echo", "", wcShell("git log --oneline | head | xargs echo"), ""},
		{"xargs /bin/echo", "", wcShell("cat files.txt | xargs /bin/echo"), ""},
		{"xargs printf", "", wcShell("cat files.txt | xargs printf '%s\\n'"), ""},
		{"xargs -n1 echo", "", wcShell("cat files.txt | xargs -n1 echo"), ""},
		{"xargs -I echo", "", wcShell("cat files.txt | xargs -I{} echo {}"), ""},
		{"echo of the home folder", "", wcShell("echo ~ | xargs echo"), ""},
		{"xargs echo on a file", "", wcShell("xargs echo < notes.txt"), ""},
		{"xargs -a echo", "", wcShell("xargs -a L echo"), ""},
		{"parallel echo stays a feed (its options are not read apart)", "", wcShell("cat files.txt | parallel echo"), wc},
		{"env echo", "", wcShell("cat files.txt | xargs env echo"), ""},

		// Words and redirections are placed as usual.
		{"a redirection from a copy", "", wcShell("xargs echo < " + inCopy), wc},
		{"a word in a copy", "", wcShell("cat files.txt | xargs echo " + inCopy), wc},
		{"a redirection into a copy", "", wcShell("cat files.txt | xargs echo > " + inCopy), wc},
		{"a word above a copy that is read", "", wcShell("cat ~ | xargs echo"), wc},

		// Not only printers.
		{"parallel with no program", "", wcShell("cat files.txt | parallel"), wc},
		{"xargs cat", "", wcShell("cat files.txt | xargs cat"), wc},
		{"xargs env cat", "", wcShell("cat files.txt | xargs env cat"), wc},
		{"xargs echo then cat", "", wcShell("cat files.txt | xargs echo | xargs cat"), wc},
		{"xargs sh -c", "", wcShell("cat files.txt | xargs sh -c 'echo $0'"), wc},
		{"an option it cannot read", "", wcShell("cat files.txt | xargs --weird echo"), wc},
		{"a program it cannot tell", "", wcShell(`cat files.txt | xargs "$P"`), wc},
	})
}
