package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyMoveOptions checks that an option of cp, mv or rsync that
// moves or reads a path beside the destination leaves the command with no
// quiet word, so a folder above a copy denies, and that the options the
// tables can vouch for do not.
func TestWorkingCopyMoveOptions(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	home := bashPath(m.ctl.home)
	m.run(t, []wcCase{
		// mv --exchange swaps the two operands: both are moved.
		{"mv -T --exchange", "", wcShell("mv -T --exchange " + home + "/b " + home + "/work"), wc},
		{"mv -T --exch", "", wcShell("mv -T --exch " + home + "/b " + home + "/work"), wc},
		{"mv --exchange, folder as the source", "", wcShell("mv --exchange " + home + "/work " + home + "/b"), wc},
		{"mv --exchange into ~", "", wcShell("mv --exchange a ~"), wc},
		{"mv --exc", "", wcShell("mv --exc a ~"), wc},
		{"mv --exchange=", "", wcShell("mv --exchange=x a ~"), wc},
		{"mv --e ambiguous", "", wcShell("mv --e a ~"), wc},

		// cp options that remove or back up what is at the destination.
		{"cp --remove-destination", "", wcShell("cp --remove-destination f ~/"), wc},
		{"cp --remove abbreviation", "", wcShell("cp --remove f ~/"), wc},
		{"cp -b", "", wcShell("cp -b f ~/"), wc},
		{"cp -bt", "", wcShell("cp -bt ~ f"), wc},
		{"cp --backup", "", wcShell("cp --backup f ~/"), wc},
		{"cp --backup=numbered", "", wcShell("cp --backup=numbered f ~/"), wc},

		// rsync options that name a third path.
		{"rsync --link-dest", "", wcShell("rsync -a --link-dest=" + home + "/work a " + home + "/out/"), wc},
		{"rsync --link-dest next word", "", wcShell("rsync -a --link-dest " + home + "/work a " + home + "/out/"), wc},
		{"rsync --compare-dest", "", wcShell("rsync -a --compare-dest=" + home + "/work a ~/out/"), wc},
		{"rsync --copy-dest", "", wcShell("rsync -a --copy-dest=" + home + "/work a ~/out/"), wc},
		{"rsync --backup-dir", "", wcShell("rsync -a --backup --backup-dir=" + home + "/work a ~/"), wc},
		{"rsync --link-dest of a plain folder, into ~", "", wcShell("rsync -a --link-dest=prev a ~/"), wc},

		// Allowed: options the tables vouch for.
		{"mv", "", wcShell("mv f ~/"), ""},
		{"mv -t", "", wcShell("mv -t ~ f"), ""},
		{"mv -T", "", wcShell("mv -T f ~/g"), ""},
		{"mv -nv", "", wcShell("mv -nv f ~/"), ""},
		{"mv -u", "", wcShell("mv -u f ~/"), ""},
		{"mv --backup", "", wcShell("mv --backup f ~/"), ""},
		{"mv -S", "", wcShell("mv -S .bak f ~/"), ""},
		{"cp -rt", "", wcShell("cp -rt ~ f"), ""},
		{"cp -a", "", wcShell("cp -a f ~/"), ""},
		{"cp -v", "", wcShell("cp -v f ~/"), ""},
		{"rsync -a", "", wcShell("rsync -a f ~/"), ""},
		{"rsync --backup", "", wcShell("rsync -a --backup f ~/"), ""},
		{"rsync --exclude", "", wcShell("rsync -a --exclude=x f ~/"), ""},
	})
}
