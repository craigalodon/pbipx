package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyTildePlusMinus checks that ~+, ~-, ~+N and ~-N, which bash
// expands from PWD, OLDPWD and the directory stack, are words the gate cannot
// tell, and that the same text inside a word or in quotes is a literal.
func TestWorkingCopyTildePlusMinus(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"~+", "", wcShell("cd ~/work; cat ~+/copy/model.txt"), wc},
		{"~-", "", wcShell("cd ~/work; cd ~/proj; cat ~-/copy/model.txt"), wc},
		{"~+ alone", "", wcShell("cd ~/work; cat ~+"), wc},
		{"~+1", "", wcShell("pushd ~/proj; cat ~+1/copy/model.txt"), wc},
		{"~-1", "", wcShell("pushd ~/proj; cat ~-1/copy/model.txt"), wc},
		{"fed through echo", "", wcShell("cd ~/work; echo ~+/copy/model.txt | xargs cat"), wc},
		{"in a path option", "", wcShell("cd ~/work; grep -r x ~+"), wc},
		{"cd to it", "", wcShell("cd ~/work; cd ~+/copy; cat model.txt"), wc},

		// Literal: inside a word, or quoted.
		{"inside a word", "", wcShell("cat a~+"), ""},
		{"quoted", "", wcShell(`cat "~+/x"`), ""},
		{"single quoted", "", wcShell("cat '~-/x'"), ""},
		{"a user-like name", "", wcShell("cat ~+x/y"), ""},
	})
}
