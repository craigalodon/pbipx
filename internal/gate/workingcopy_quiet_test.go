package gate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyAboveIsQuiet checks that an ordinary command that names a
// folder above a working copy, such as the home folder, is not denied when
// the program is known to read nothing under it, and is when it could.
func TestWorkingCopyAboveIsQuiet(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	home, work := bashPath(m.ctl.home), bashPath(filepath.Join(m.ctl.home, "work"))
	m.run(t, []wcCase{
		// Programs known to read nothing under their words.
		{"echo ~", "", wcShell("echo ~"), ""},
		{"echo $HOME quoted", "", wcShell(`echo "$HOME"`), ""},
		{"export", "", wcShell("export X=$HOME"), ""},
		{"test -d ~", "", wcShell("test -d ~"), ""},
		{"[ -d ~ ]", "", wcShell("[ -d ~ ]"), ""},
		{"dirname", "", wcShell("dirname ~"), ""},
		{"basename", "", wcShell("basename $HOME"), ""},
		{"printf", "", wcShell("printf %s ~"), ""},
		{"echo the folder above", "", wcShell("echo " + work), ""},
		{"ls ~", "", wcShell("ls ~"), ""},
		{"mkdir below the folder above", "", wcShell("mkdir -p " + work + "/other"), ""},
		{"rm of a sibling", "", wcShell("rm " + bashPath(m.sibling) + "/x.txt"), ""},

		// Destinations.
		{"cp into ~", "", wcShell("cp f ~/"), ""},
		{"cp -t ~", "", wcShell("cp -t ~ f"), ""},
		{"mv into ~", "", wcShell("mv f ~/"), ""},
		{"rsync into ~", "", wcShell("rsync -a f ~/"), ""},
		{"tar x -C ~", "", wcShell("tar xzf a.tgz -C ~"), ""},
		{"tar -x -C ~", "", wcShell("tar -x -f a.tar -C " + home), ""},
		{"unzip -d ~", "", wcShell("unzip a.zip -d ~"), ""},
		{"wget -P ~", "", wcShell("wget -P ~ http://example.com/f"), ""},
		{"git -C ~", "", wcShell("git -C ~ status"), ""},
		{"code ~", "", wcShell("code ~"), ""},
		{"echo to a file in ~", "", wcShell("echo hi > ~/note.txt"), ""},

		// It could read under the folder: still denied.
		{"grep -r ~", "", wcShell("grep -r X ~"), wc},
		{"rg from above", m.ctl.home, wcShell("rg X"), wc},
		{"cp -r ~ as a source", "", wcShell("cp -r ~/ x"), wc},
		{"cp ~ as a source", "", wcShell("cp -r " + home + " x"), wc},
		{"tar c -C ~", "", wcShell("tar czf x.tgz -C ~ ."), wc},
		{"rsync from ~", "", wcShell("rsync -a ~/ x/"), wc},
		{"an unknown program", "", wcShell("frobnicate ~"), wc},
		{"cat ~", "", wcShell("cat ~"), wc},
		{"find ~ -exec", "", wcShell("find ~ -exec cat {} +"), wc},
		{"zip -r", "", wcShell("zip -r x.zip ~"), wc},
		{"grep -r in the folder above", m.ctl.home, wcShell("grep -r X ."), wc},

		// A word inside the copy denies for every program.
		{"echo a path in it", "", wcShell("echo ~/work/copy/x"), wc},
		{"test -d it", "", wcShell("test -d ~/work/copy"), wc},
		{"export it", "", wcShell("export X=" + work + "/copy"), wc},
		{"basename of a path in it", "", wcShell("basename " + work + "/copy/x"), wc},
		{"cp into it", "", wcShell("cp f ~/work/copy/"), wc},
		{"mv into it", "", wcShell("mv f " + work + "/copy"), wc},
		{"tar x -C it", "", wcShell("tar xzf a.tgz -C " + work + "/copy"), wc},
		{"unzip -d it", "", wcShell("unzip a.zip -d " + work + "/copy"), wc},
		{"wget -P it", "", wcShell("wget -P " + work + "/copy http://example.com/f"), wc},
		{"git -C it", "", wcShell("git -C " + work + "/copy status"), wc},
		{"code it", "", wcShell("code " + work + "/copy"), wc},
	})
}

// TestWorkingCopyAttachedShortOptionValues checks that a path attached to a
// short option (-C<dir>, -I<dir>/x, -o<file>) is placed for working copies.
func TestWorkingCopyAttachedShortOptionValues(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	copyDir, sib := bashPath(m.wc), bashPath(m.sibling)
	m.run(t, []wcCase{
		{"sort -o", "", wcShell("sort -o" + copyDir + "/f x"), wc},
		{"make -C", "", wcShell("make -C" + copyDir), wc},
		{"cc -I", "", wcShell("cc -I" + copyDir + "/x main.c"), wc},
		{"tar -C", "", wcShell("tar xf a.tar -C" + copyDir), wc},
		{"relative from the folder above", filepath.Dir(m.wc), wcShell("make -Ccopy"), wc},
		{"-I of a sibling", "", wcShell("cc -I" + sib + "/x main.c"), ""},
		{"make -C of a sibling", "", wcShell("make -C" + sib), ""},
		{"a flag cluster", "", wcShell("ls -la"), ""},
		{"-n5", "", wcShell("head -n5 " + bashPath(filepath.Join(m.outside, "notes.txt"))), ""},
	})
}

// TestWorkingCopyBrokenRegistryMessage checks that a registry error tells a
// person what to do, names the entry file, and prints no content of it.
func TestWorkingCopyBrokenRegistryMessage(t *testing.T) {
	m := newWCMachine(t)
	name := strings.Repeat("c", 64) + ".json"
	write(t, filepath.Join(m.ctl.store, "stages"), name, `{"version":1,"path":"/home/someone/SECRET-FOLDER"`)
	d := m.decide(wcRead(filepath.Join(m.outside, "notes.txt")), "")
	check(t, d, gate.ReasonInternalError)
	for _, want := range []string{name, "remove that file"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("the message lacks %q: %s", want, d.Message)
		}
	}
	for _, leak := range []string{"SECRET-FOLDER", m.ctl.store, m.wc} {
		if strings.Contains(d.Message, leak) {
			t.Errorf("the message holds %q: %s", leak, d.Message)
		}
	}
}

// TestWorkingCopyTooManyEntries checks that a registry past the read cap
// denies every call, with a message that says why and what to do.
func TestWorkingCopyTooManyEntries(t *testing.T) {
	m := newWCMachine(t)
	for i := range approval.MaxWorkingCopiesRead {
		name := fmt.Sprintf("%064x.json", i+1)
		write(t, filepath.Join(m.ctl.store, "stages"), name, `{"version":1,"path":`+quote(filepath.Join(m.outside, "nowhere"))+`}`)
	}
	d := m.decide(wcRead(filepath.Join(m.outside, "notes.txt")), "")
	check(t, d, gate.ReasonInternalError)
	if !strings.Contains(d.Message, "registered") || !strings.Contains(d.Message, "remove") {
		t.Errorf("the message does not say why or what to do: %s", d.Message)
	}
}

// TestWorkingCopyHandEditedEntryStillDenies checks that an entry the store
// would refuse to add, written by hand, protects what it names, and that the
// message stays sensible.
func TestWorkingCopyHandEditedEntryStillDenies(t *testing.T) {
	for name, path := range map[string]func(m wcMachine) string{
		"the home folder":    func(m wcMachine) string { return m.ctl.home },
		"a parent of home":   func(m wcMachine) string { return filepath.Dir(m.ctl.home) },
		"a folder in a tree": func(m wcMachine) string { return m.adopted },
	} {
		t.Run(name, func(t *testing.T) {
			m := newWCMachine(t)
			if err := os.RemoveAll(filepath.Join(m.ctl.store, "stages")); err != nil {
				t.Fatal(err)
			}
			p := path(m)
			body := `{"version":1,"path":` + quote(p) + `}`
			write(t, filepath.Join(m.ctl.store, "stages"), strings.Repeat("e", 64)+".json", body)
			d := m.decide(wcRead(filepath.Join(p, "notes.txt")), m.outside)
			check(t, d, gate.ReasonWorkingCopy)
			if !strings.Contains(d.Message, "Power BI Desktop working copy") || !strings.HasSuffix(d.Message, "Power BI Desktop.") {
				t.Errorf("the message is not the working-copy message: %s", d.Message)
			}
			if strings.Contains(d.Message, p) {
				t.Errorf("the message prints the registered path")
			}
		})
	}
}

func quote(s string) string {
	b := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + b + `"`
}
