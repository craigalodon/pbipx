package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyMoveAndLinkAbove checks that mv and ln of a folder above a
// working copy as a source deny: moving the folder moves the copy out of its
// registered path, and a link to it reads through to the copy. Their
// destinations stay quiet.
func TestWorkingCopyMoveAndLinkAbove(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		// A source above the copy.
		{"mv ~", "", wcShell("mv ~ /tmp/pbipx-elsewhere"), wc},
		{"mv ~/work", "", wcShell("mv ~/work ~/work2"), wc},
		{"mv a rename of the parent", "", wcShell("mv -T ~/work ~/renamed"), wc},
		{"mv -t", "", wcShell("mv -t /tmp/pbipx-d ~/work"), wc},
		{"mv -f", "", wcShell("mv -f ~/work /tmp/pbipx-d"), wc},
		{"mv then read", "", wcShell("mv ~/work /tmp/pbipx-d && ls /tmp/pbipx-d"), wc},
		{"mv a relative parent", m.ctl.home, wcShell("mv work work2"), wc},
		{"mv .", m.ctl.home + "/work", wcShell("mv . /tmp/pbipx-d"), wc},
		{"mv from a unknown word", "", wcShell("mv $X /tmp/pbipx-d"), wc},
		{"mv to a destination that cannot be told", "", wcShell("mv f $D"), wc},
		{"ln from a word that cannot be told", "", wcShell("ln -s $X /tmp/pbipx-L"), wc},
		{"ln -s ~", "", wcShell("ln -s ~ /tmp/pbipx-L"), wc},
		{"ln -s ~/work", "", wcShell("ln -s ~/work /tmp/pbipx-L"), wc},
		{"ln -sf", "", wcShell("ln -sf ~/work /tmp/pbipx-L"), wc},
		{"ln then read through the link", "", wcShell("ln -s ~/work /tmp/pbipx-L && cat /tmp/pbipx-L/copy/x.txt"), wc},
		{"ln then list through the link", "", wcShell("ln -s ~/work /tmp/pbipx-L; ls /tmp/pbipx-L/copy"), wc},
		{"ln hard", "", wcShell("ln ~/work /tmp/pbipx-L"), wc},
		{"ln -t", "", wcShell("ln -s -t /tmp/pbipx-d ~/work"), wc},

		// A destination above it.
		{"mv f ~", "", wcShell("mv f ~/"), ""},
		{"mv f ~/work", "", wcShell("mv f ~/work/"), ""},
		{"mv -t ~", "", wcShell("mv -t ~ f"), ""},
		{"ln -s f ~", "", wcShell("ln -s f ~/"), ""},
		{"ln -s f ~/work", "", wcShell("ln -s f ~/work/L"), ""},
		{"ln -s -t ~", "", wcShell("ln -s -t ~ f"), ""},

		// In the copy: denied whatever the position.
		{"mv into the copy", "", wcShell("mv f ~/work/copy/"), wc},
		{"mv out of the copy", "", wcShell("mv ~/work/copy/x.txt /tmp/pbipx-d"), wc},
		{"ln to the copy", "", wcShell("ln -s ~/work/copy /tmp/pbipx-L"), wc},
		{"ln in the copy", "", wcShell("ln -s f ~/work/copy/L"), wc},

		// A sibling is not the folder.
		{"mv a sibling", "", wcShell("mv " + bashPath(m.sibling) + " /tmp/pbipx-d"), ""},
		{"ln a sibling", "", wcShell("ln -s " + bashPath(m.sibling) + " /tmp/pbipx-L"), ""},
	})
}
