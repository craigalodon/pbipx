package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyTildePrefixEverywhere checks that a tilde prefix other than
// a plain ~ (~+, ~-, ~N, ~+N, ~-N, ~user) is an expansion the gate cannot tell
// wherever bash expands it, as a redirect target, a command word or a script
// operand, and that the same text is a literal where bash does not expand it.
func TestWorkingCopyTildePrefixEverywhere(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"a redirect target", "", wcShell("cd ~/work; tr a b < ~+/copy/model.txt"), wc},
		{"read from it", "", wcShell("cd ~/work; read x < ~+/copy/model.txt"), wc},
		{"a substitution", "", wcShell("cd ~/work; echo $(< ~+/copy/model.txt)"), wc},
		{"exec", "", wcShell("cd ~/work; exec 3< ~+/copy/model.txt"), wc},
		{"a command word", "", wcShell("cd ~/work; ~+/copy/run.sh"), wc},
		{"sh", "", wcShell("cd ~/work; sh ~+/copy/run.sh"), wc},
		{"env", "", wcShell("cd ~/work; env ~+/copy/run.sh"), wc},
		{"~- as a target", "", wcShell("cd ~/work; cd ~/proj; tr a b < ~-/copy/model.txt"), wc},
		{"~1 after pushd", "", wcShell("pushd ~/proj; tr a b < ~1/copy/model.txt"), wc},
		{"~+1", "", wcShell("pushd ~/proj; tr a b < ~+1/copy/model.txt"), wc},
		{"~-1", "", wcShell("pushd ~/proj; tr a b < ~-1/copy/model.txt"), wc},
		{"a user in a redirect", "", wcShell("tr a b < ~name/work/copy/model.txt"), wc},
		{"a user as a command", "", wcShell("~name/work/copy/run.sh"), wc},
		{"a user as a script", "", wcShell("sh ~name/work/copy/run.sh"), wc},
		{"root", "", wcShell("tr a b < ~root/x"), wc},
		{"after = in an assignment", "", wcShell("cd ~/work; P=~+/copy/x; cat $P"), wc},
		{"after : in an assignment", "", wcShell("cd ~/work; P=a:~+/copy; cat $P"), wc},
		{"an unknown redirect target", "", wcShell(`tr a b < "$X"`), wc},
		{"a redirect target with a glob-free substitution", "", wcShell("tr a b < $(echo x)"), wc},

		{"in an adopted repository", m.adopted, wcShell("tr a b < ~+/.pbi/cache.abf"), wc},
		{"~user in an adopted repository", m.adopted, wcShell("tr a b < ~name/x/run.sh"), wc},

		// Literal.
		{"in the middle of a word", "", wcShell("tr a b < a~+/x"), ""},
		{"quoted", "", wcShell(`tr a b < "~+/x"`), ""},
		{"single quoted", "", wcShell("tr a b < '~name/x'"), ""},
		{"escaped", "", wcShell(`tr a b < \~+/x`), ""},
		{"~+x", "", wcShell("tr a b < ~+x/y"), ""},
		{"a plain ~", "", wcShell("tr a b < ~/nothing.txt"), ""},
		{"a word that is only ~+ expands", "", wcShell("echo a ~+"), wc},
		{"after = outside an assignment", "", wcShell("echo --opt=~+/x"), ""},
	})
}
