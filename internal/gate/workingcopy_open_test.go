package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyOpenersAbove checks that a program that opens a folder in a
// window is quiet above a working copy only given operands and no option:
// code --diff, open -a, open -f and the like can print or pipe content, and
// xdg-open runs whatever handler the system has.
func TestWorkingCopyOpenersAbove(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"code ~", "", wcShell("code ~"), ""},
		{"code the folder", "", wcShell("code " + bashPath(m.ctl.home)), ""},
		{"cursor ~", "", wcShell("cursor ~"), ""},
		{"open ~", "", wcShell("open ~"), ""},
		{"explorer ~", "", wcShell("explorer ~"), ""},
		{"explorer.exe ~", "", wcShell("explorer.exe ~"), ""},

		{"code --diff", "", wcShell("code --diff ~ ~/work"), wc},
		{"code -d", "", wcShell("code -d ~ ~/work"), wc},
		{"code --diff after the folder", "", wcShell("code ~ --diff"), wc},
		{"code -", "", wcShell("echo x | code - ~"), wc},
		{"code --goto", "", wcShell("code --goto ~"), wc},
		{"code --status", "", wcShell("code --status ~"), wc},
		{"code --install-extension", "", wcShell("code --install-extension ~"), wc},
		{"code --merge", "", wcShell("code --merge ~ ~ ~ ~"), wc},
		{"code an unknown word", "", wcShell("code $X ~"), wc},
		{"cursor --diff", "", wcShell("cursor --diff ~ ~/work"), wc},
		{"open -a", "", wcShell("open -a TextEdit ~"), wc},
		{"open -f", "", wcShell("open -f ~"), wc},
		{"open -t", "", wcShell("open -t ~"), wc},
		{"open --args", "", wcShell("open ~ --args x"), wc},
		{"xdg-open ~", "", wcShell("xdg-open ~"), wc},

		{"code in the copy", "", wcShell("code ~/work/copy"), wc},
		{"open in the copy", "", wcShell("open ~/work/copy"), wc},
	})
}
