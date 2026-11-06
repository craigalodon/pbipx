package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyCopierDestinations checks that a word of cp, mv, rsync,
// install or tee is quiet above a working copy only where it is the
// destination, by position and not by text, and that an option the gate does
// not fully understand makes no word of the command quiet.
func TestWorkingCopyCopierDestinations(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		// The destination is quiet, however -t is spelled.
		{"cp into ~", "", wcShell("cp f ~/"), ""},
		{"cp -t", "", wcShell("cp -t ~ f"), ""},
		{"cp -rt", "", wcShell("cp -rt ~ f"), ""},
		{"cp -vrt", "", wcShell("cp -vrt ~ f"), ""},
		{"cp -nt", "", wcShell("cp -nt ~ f"), ""},
		{"cp -t attached", "", wcShell("cp -t$HOME f"), ""},
		{"cp -rt attached", "", wcShell("cp -rt$HOME f"), ""},
		{"cp --target-directory", "", wcShell("cp --target-directory ~ f"), ""},
		{"cp --target-directory=", "", wcShell("cp --target-directory=$HOME f"), ""},
		{"cp --target", "", wcShell("cp --target ~ f"), ""},
		{"cp --target=", "", wcShell("cp --target=$HOME f"), ""},
		{"cp --target-dir", "", wcShell("cp --target-dir ~ f"), ""},
		{"cp -T", "", wcShell("cp -rT f ~"), ""},
		{"cp -- ", "", wcShell("cp -r -- f ~"), ""},
		{"cp -S value", "", wcShell("cp -S .bak f ~/"), ""},
		{"cp --backup=numbered", "", wcShell("cp --backup=numbered f ~/"), ""},
		{"mv into ~", "", wcShell("mv f ~/"), ""},
		{"mv -t", "", wcShell("mv -t ~ f"), ""},
		{"mv -vt", "", wcShell("mv -vt ~ f"), ""},
		{"mv --target", "", wcShell("mv --target ~ f"), ""},
		{"install -t", "", wcShell("install -t ~ f"), ""},
		{"install -Dt", "", wcShell("install -Dt ~ f"), ""},
		{"install -m", "", wcShell("install -m 644 f ~/"), ""},
		{"install -m attached", "", wcShell("install -m644 f ~/"), ""},
		{"install -d", "", wcShell("install -d ~"), ""},
		{"rsync", "", wcShell("rsync -a f ~/"), ""},
		{"rsync -av", "", wcShell("rsync -av f ~/"), ""},
		{"rsync --delete", "", wcShell("rsync -a --delete f ~/"), ""},
		{"tee", "", wcShell("echo x | tee ~"), ""},
		{"tee -a", "", wcShell("echo x | tee -a ~"), ""},

		// The source is not, even when it reads like the destination.
		{"cp -rt, the source is ~", "", wcShell("cp -rt /tmp/d ~"), wc},
		{"cp -vrt, the source is ~", "", wcShell("cp -vrt /tmp/d ~"), wc},
		{"cp -nt, the source is ~", "", wcShell("cp -nt /tmp/d ~"), wc},
		{"cp -t, the source is ~", "", wcShell("cp -t /tmp/d ~"), wc},
		{"cp --target, the source is ~", "", wcShell("cp --target /tmp/d ~"), wc},
		{"cp --target-dir, the source is ~", "", wcShell("cp --target-dir /tmp/d ~"), wc},
		{"cp --target-dir=, the source is ~", "", wcShell("cp --target-dir=/tmp/d ~"), wc},
		{"cp -r ~ ~", "", wcShell("cp -r ~ ~"), wc},
		{"cp -r ~ ~/", "", wcShell("cp -r ~ ~/"), wc},
		{"cp the same text twice", "", wcShell("cp -r ~/ ~"), wc},
		{"cp the source first", "", wcShell("cp -r ~ /tmp/d"), wc},
		{"mv -vt, the source is ~", "", wcShell("mv -vt /tmp/d ~"), wc},
		{"mv --target, the source is ~", "", wcShell("mv --target /tmp/d ~"), wc},
		{"install -t, the source is ~", "", wcShell("install -t /tmp/d ~"), wc},
		{"install -Dt, the source is ~", "", wcShell("install -Dt /tmp/d ~"), wc},
		{"rsync ~ ~", "", wcShell("rsync -a ~ ~"), wc},
		{"rsync from ~", "", wcShell("rsync -a ~ /tmp/d"), wc},

		// An option the gate does not fully understand: no word is quiet.
		{"cp an unknown option", "", wcShell("cp --frobnicate f ~/"), wc},
		{"cp an unknown short option", "", wcShell("cp -Q f ~/"), wc},
		{"cp an unknown letter in a cluster", "", wcShell("cp -rQt ~ f"), wc},
		{"cp an ambiguous abbreviation", "", wcShell("cp --no f ~/"), wc},
		{"cp a word that cannot be told", "", wcShell("cp $X ~/"), wc},
		{"cp the value of -S missing", "", wcShell("cp f -S"), ""},
		{"mv an unknown option", "", wcShell("mv --frobnicate f ~/"), wc},
		{"install an unknown option", "", wcShell("install --frobnicate f ~/"), wc},
		{"install an unknown short option", "", wcShell("install -Q f ~/"), wc},
		{"rsync an unknown option", "", wcShell("rsync --frobnicate f ~/"), wc},
		{"tee an unknown option", "", wcShell("echo x | tee --frobnicate ~"), wc},
		{"tee -X", "", wcShell("echo x | tee -X ~"), wc},

		// Inside a copy denies whatever the position.
		{"cp into the copy", "", wcShell("cp -t ~/work/copy f"), wc},
		{"cp from the copy", "", wcShell("cp -t /tmp/d ~/work/copy/x.txt"), wc},
	})
}
