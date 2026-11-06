package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyTarMode checks that the directory tar is told to work in is
// quiet above a working copy only when tar is proven to extract: one mode,
// x, parsed as tar parses it. Anything else reads the directory, or is not
// understood.
func TestWorkingCopyTarMode(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		// Extracting into the folder.
		{"tar xzf", "", wcShell("tar xzf a.tgz -C ~"), ""},
		{"tar -xzf", "", wcShell("tar -xzf a.tgz -C ~"), ""},
		{"tar -x -f", "", wcShell("tar -x -f a.tar -C ~"), ""},
		{"tar -C first", "", wcShell("tar -C ~ -xf a.tar"), ""},
		{"tar --extract", "", wcShell("tar --extract --file=a.tar --directory=$HOME"), ""},
		{"tar --extract abbreviated", "", wcShell("tar --extr -f a.tar --directory $HOME"), ""},
		{"tar --get", "", wcShell("tar --get -f a.tar -C ~"), ""},
		{"tar -fx.tar names the archive", "", wcShell("tar -fx.tar -x -C ~"), ""},
		{"tar xCf, the values in order", "", wcShell("tar xCf ~ a.tar"), ""},
		{"tar strip-components", "", wcShell("tar -xf a.tar --strip-components=1 -C ~"), ""},
		{"tar x -C twice", "", wcShell("tar xf a.tar -C ~ -C /tmp/pbipx-d"), ""},
		{"tar -v -xf", "", wcShell("tar -v -xf a.tar -C ~"), ""},
		{"tar archive name that cannot be told", "", wcShell(`tar xf "$A" -C ~`), wc},
		{"tar -C attached to the next word", "", wcShell("tar xf a.tar -C$HOME"), ""},

		// Another mode, or one that is not understood: a reader.
		{"tar -fx.tar -c", "", wcShell("tar -fx.tar -c -C ~ ."), wc},
		{"tar -cf", "", wcShell("tar -cf x.tar -C ~ ."), wc},
		{"tar czf", "", wcShell("tar czf x.tgz -C ~ ."), wc},
		{"tar --create", "", wcShell("tar --create --file=x.tar -C ~ ."), wc},
		{"tar --creat", "", wcShell("tar --creat -f x.tar -C ~ ."), wc},
		{"tar -x -c", "", wcShell("tar -x -c -f x.tar -C ~ ."), wc},
		{"tar xcf", "", wcShell("tar xcf x.tar -C ~ ."), wc},
		{"tar -xf -r", "", wcShell("tar -xf a.tar -r -C ~ ."), wc},
		{"tar -xf -u", "", wcShell("tar -xf a.tar -u -C ~ ."), wc},
		{"tar -xf -A", "", wcShell("tar -xf a.tar -A -C ~ b.tar"), wc},
		{"tar --append", "", wcShell("tar --extract --append -f a.tar -C ~ ."), wc},
		{"tar --update", "", wcShell("tar --get --update -f a.tar -C ~ ."), wc},
		{"tar --catenate", "", wcShell("tar --catenate -f a.tar -C ~ b.tar"), wc},
		{"tar -tf", "", wcShell("tar -tf a.tar -C ~"), wc},
		{"tar -d", "", wcShell("tar -df a.tar -C ~"), wc},
		{"tar --diff", "", wcShell("tar --diff -f a.tar -C ~"), wc},
		{"tar --delete", "", wcShell("tar --delete -f a.tar -C ~ x"), wc},
		{"tar no mode", "", wcShell("tar -f a.tar -C ~ ."), wc},
		{"tar an unknown long option", "", wcShell("tar xf a.tar --frobnicate -C ~"), wc},
		{"tar a command option", "", wcShell("tar xf a.tar --to-command=cat -C ~"), wc},
		{"tar an ambiguous abbreviation", "", wcShell("tar --ex -f a.tar -C ~"), wc},
		{"tar an unknown short option", "", wcShell("tar -xQf a.tar -C ~"), wc},
		{"tar -I", "", wcShell("tar -xf a.tar -I zstd -C ~"), wc},
		{"tar a word that cannot be told", "", wcShell(`tar xf a.tar $X -C ~`), wc},
		{"tar members are not the directory", "", wcShell("tar xf a.tar -C /tmp/pbipx-d ~"), wc},
		{"tar a member above", "", wcShell("tar xf a.tar -C /tmp/pbipx-d ~/work"), wc},

		{"tar -xCf, C takes the rest of the word", "", wcShell("tar -xCf ~ a.tar"), wc},

		// In the copy: denied whatever the mode.
		{"tar x -C the copy", "", wcShell("tar xzf a.tgz -C ~/work/copy"), wc},
	})
}
