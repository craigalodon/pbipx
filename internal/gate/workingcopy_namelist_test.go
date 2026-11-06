package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyNameLists checks that a program that reads file names from a
// list (a file the gate does not read) touches every working copy, and that a
// list of folders above a copy can still be made.
func TestWorkingCopyNameLists(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"xargs -0 -a", "", wcShell("find ~ -name '*.abf' -print0 > L; xargs -0 -a L cat"), wc},
		{"xargs -a", "", wcShell("xargs -a L cat"), wc},
		{"xargs -aL", "", wcShell("xargs -aL cat"), wc},
		{"xargs --arg-file", "", wcShell("xargs --arg-file=L cat"), wc},
		{"xargs --arg-file next", "", wcShell("xargs --arg-file L cat"), wc},
		{"xargs -a -d", "", wcShell(`xargs -a L -d '\n' head`), wc},
		{"xargs < L", "", wcShell("xargs cat < L"), wc},
		{"xargs -0 < L", "", wcShell("xargs -0 cat < L"), wc},
		{"cat L | xargs", "", wcShell("cat L | xargs cat"), wc},
		{"parallel -a", "", wcShell("parallel -a L cat"), wc},
		{"parallel ::::", "", wcShell("parallel cat :::: L"), wc},
		{"tar -T", "", wcShell("tar cf x -T L"), wc},
		{"tar -TL", "", wcShell("tar cf x -TL"), wc},
		{"tar cTf", "", wcShell("tar cTf L x"), wc},
		{"tar czTf", "", wcShell("tar -czT L -f x"), wc},
		{"tar --files-from", "", wcShell("tar cf x --files-from=L"), wc},
		{"tar --files-from next", "", wcShell("tar cf x --files-from L"), wc},
		{"tar --verbatim-files-from", "", wcShell("tar cf x --verbatim-files-from -T L"), wc},
		{"tar --files abbreviation", "", wcShell("tar cf x --files=L"), wc},
		{"bsdtar -T", "", wcShell("bsdtar -cf x -T L"), wc},
		{"wc --files0-from", "", wcShell("wc --files0-from=L"), wc},
		{"sort --files0-from", "", wcShell("sort --files0-from=L"), wc},
		{"du --files0-from", "", wcShell("du --files0-from=L"), wc},
		{"du --files0-from next word", "", wcShell("du --files0-from L"), wc},
		{"wc --files0 abbreviation", "", wcShell("wc --files0=L"), wc},
		{"rsync --files-from", "", wcShell("rsync -a --files-from=L src/ out/"), wc},
		{"cpio", "", wcShell("cpio -o < L > x"), wc},
		{"cpio -p", "", wcShell("cpio -pd out"), wc},
		{"zip -@", "", wcShell("zip x.zip -@ < L"), wc},
		{"zip -@ in a cluster", "", wcShell("zip -r@ x.zip < L"), wc},
		{"7z @list", "", wcShell("7z a x.7z @L"), wc},

		// Names that are no list, or a list of folders made, stay allowed.
		{"grep -f reads patterns", "", wcShell("grep -f pats file"), ""},
		{"listing a folder above a copy into a file", "", wcShell("find ~ -name '*.abf' -print0 > L"), ""},
		{"ls into a file", "", wcShell("ls ~ > L"), ""},
		{"tar x", "", wcShell("tar xzf a.tgz -C ~"), ""},
		{"tar --file", "", wcShell("tar --file=a.tar -x -C ~"), ""},
		{"tar cf of a name with a T in its path", "", wcShell("tar cf x.tar src"), ""},
		{"tar attached directory with a T", "", wcShell("tar xf a.tar -C$HOME"), ""},
		{"wc of a file", "", wcShell("wc -l notes.txt"), ""},
		{"sort of a file", "", wcShell("sort notes.txt"), ""},
		{"zip of a folder", "", wcShell("zip -r x.zip src"), ""},
		{"rsync of a folder", "", wcShell("rsync -a src/ out/"), ""},
		{"xargs from a producer", "", wcShell("git ls-files | xargs wc -l"), ""},
		{"xargs -d newline from a producer", "", wcShell(`git ls-files | xargs -d '\n' wc -l`), ""},
		{"xargs -0 from git -z", "", wcShell("git ls-files -z | xargs -0 wc -l"), ""},
	})
}
