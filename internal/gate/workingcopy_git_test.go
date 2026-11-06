package gate_test

import (
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyGitAbove checks that git, run in or pointed at a folder
// above a working copy, is allowed only for a read-only subcommand that
// opens no file under the folder, and denied for every other.
func TestWorkingCopyGitAbove(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	home := bashPath(m.ctl.home)
	work := bashPath(filepath.Dir(m.wc))
	sib := bashPath(m.sibling)
	var cases []wcCase
	for _, prog := range []string{"git", "hub"} {
		cases = append(cases, []wcCase{
			// Read-only and opening no file under the folder.
			{prog + " status", m.ctl.home, wcShell(prog + " status"), ""},
			{prog + " -C status", "", wcShell(prog + " -C ~ status"), ""},
			{prog + " status short", "", wcShell(prog + " -C " + home + " status -sb"), ""},
			{prog + " log oneline", "", wcShell(prog + " -C ~ log --oneline -n 3"), ""},
			{prog + " log stat", "", wcShell(prog + " -C ~ log --stat"), ""},
			{prog + " show no patch", "", wcShell(prog + " -C ~ show --no-patch HEAD"), ""},
			{prog + " show -s", "", wcShell(prog + " -C ~ show -s HEAD"), ""},
			{prog + " show stat", "", wcShell(prog + " -C ~ show --stat HEAD"), ""},
			{prog + " branch", "", wcShell(prog + " -C ~ branch"), ""},
			{prog + " branch --list", "", wcShell(prog + " -C ~ branch --list"), ""},
			{prog + " rev-parse", "", wcShell(prog + " -C ~ rev-parse --show-toplevel"), ""},
			{prog + " remote", "", wcShell(prog + " -C ~ remote -v"), ""},
			{prog + " tag list", "", wcShell(prog + " -C ~ tag"), ""},
			{prog + " from the work folder", m.ctl.home + "/work", wcShell(prog + " status"), ""},

			// Reads files below the folder, or could: denied.
			{prog + " grep --no-index", "", wcShell(prog + " -C ~ grep --no-index X ."), wc},
			{prog + " grep", "", wcShell(prog + " -C ~ grep X"), wc},
			{prog + " diff --no-index", "", wcShell(prog + " -C ~ diff --no-index a b"), wc},
			{prog + " diff --no-ind", "", wcShell(prog + " -C ~ diff --no-ind a b"), wc},
			{prog + " add", "", wcShell(prog + " -C ~ add -A"), wc},
			{prog + " add from above", m.ctl.home, wcShell(prog + " add ."), wc},
			{prog + " add from the work folder", m.ctl.home + "/work", wcShell(prog + " add -A"), wc},
			{prog + " hash-object", "", wcShell(prog + " -C ~ hash-object -w x"), wc},
			{prog + " archive", "", wcShell(prog + " -C ~ archive -o /tmp/x.tar HEAD"), wc},
			{prog + " clone", "", wcShell(prog + " clone ~ y"), wc},
			{prog + " clone work", "", wcShell(prog + " clone " + work + " y"), wc},
			{prog + " status -v", "", wcShell(prog + " -C ~ status -v"), wc},
			{prog + " status -sv", "", wcShell(prog + " -C ~ status -sv"), wc},
			{prog + " status --verbose", "", wcShell(prog + " -C ~ status --verbose"), wc},
			{prog + " log -p", "", wcShell(prog + " -C ~ log -p"), wc},
			{prog + " log --patch", "", wcShell(prog + " -C ~ log --patch"), wc},
			{prog + " log --pat", "", wcShell(prog + " -C ~ log --pat"), wc},
			{prog + " log -u", "", wcShell(prog + " -C ~ log -u"), wc},
			{prog + " log --no-index", "", wcShell(prog + " -C ~ log --no-index"), wc},
			{prog + " show default patch", "", wcShell(prog + " -C ~ show HEAD"), wc},
			{prog + " show --patch", "", wcShell(prog + " -C ~ show --stat --patch HEAD"), wc},
			{prog + " branch new", "", wcShell(prog + " -C ~ branch topic"), wc},
			{prog + " commit -a", "", wcShell(prog + " -C ~ commit -a -m x"), wc},
			{prog + " stash", "", wcShell(prog + " -C ~ stash"), wc},
			{prog + " unknown subcommand", "", wcShell(prog + " -C ~ frobnicate"), wc},
			{prog + " an alias", "", wcShell(prog + " -C ~ st"), wc},
			{prog + " a word that cannot be told", "", wcShell(prog + " -C ~ $X"), wc},
			{prog + " -c core.fsmonitor", "", wcShell(prog + " -C ~ -c core.fsmonitor=x status"), wc},
			{prog + " --work-tree", "", wcShell(prog + " --work-tree ~ status"), wc},
			{prog + " --work-tree=", "", wcShell(prog + " --work-tree=" + home + " add ."), wc},
			{prog + " --git-dir and --work-tree", "", wcShell(prog + " --git-dir=/x/.git --work-tree=~ add ."), wc},
			{prog + " in the copy", "", wcShell(prog + " -C " + bashPath(m.wc) + " status"), wc},
			{prog + " a path in the copy", m.ctl.home, wcShell(prog + " log -- work/copy/x"), wc},
			// A relative path is placed against the -C directory.
			{prog + " -C then relative", "", wcShell(prog + " -C " + sib + " add ../copy/x"), wc},
			{prog + " -C twice", m.outside, wcShell(prog + " -C " + sib + " -C .. add copy"), wc},
			{prog + " -C then up", m.outside, wcShell(prog + " -C " + sib + " -C ../.. add ."), wc},

			// Unrelated to the copy.
			{prog + " status in a repository", m.adopted, wcShell(prog + " status"), ""},
			{prog + " add elsewhere", "", wcShell(prog + " -C " + bashPath(m.outside) + " add notes.txt"), ""},
			{prog + " add from elsewhere", m.outside, wcShell(prog + " add notes.txt"), ""},
			{prog + " -C sibling add", "", wcShell(prog + " -C " + sib + " add ."), ""},
		}...)
	}
	m.run(t, cases)
}

// TestWorkingCopyPbipxAbove checks that pbipx names a folder above a
// working copy quietly only for the subcommands that read no operand.
func TestWorkingCopyPbipxAbove(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"pbipx version", "", wcShell("pbipx version"), ""},
		{"pbipx help scan", m.ctl.home, wcShell("pbipx help scan"), ""},
		{"pbipx scan ~", "", wcShell("pbipx scan ~"), wc},
		{"pbipx scan from above", m.ctl.home, wcShell("pbipx scan"), wc},
		{"pbipx scan dot from above", m.ctl.home, wcShell("pbipx scan ."), wc},
		{"pbipx explain ~", "", wcShell("pbipx explain ~"), wc},
		{"pbipx scan-text ~", "", wcShell("pbipx scan-text ~/work"), wc},
		{"pbipx init from above", m.ctl.home, wcShell("pbipx init"), wc},
		{"pbipx an option first", "", wcShell("pbipx --x ~"), wc},
		{"pbipx scan a file elsewhere", "", wcShell("pbipx scan " + bashPath(filepath.Join(m.outside, "notes.txt"))), ""},
	})
}
