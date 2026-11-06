package gate_test

import "testing"

// TestWorkingCopyKeepsOrdinaryCallsAllowed checks that the calls a person
// makes every day, naming the home folder above a working copy, stay
// allowed from a folder and from a repository unrelated to the copy.
func TestWorkingCopyKeepsOrdinaryCallsAllowed(t *testing.T) {
	m := newWCMachine(t)
	var cases []wcCase
	for _, from := range []struct{ name, dir string }{{"outside", ""}, {"repository", m.adopted}} {
		for _, c := range []string{
			"echo ~", "printf %s ~", "test -d ~", "[ -d ~ ]", "dirname ~", "basename ~", "export X=$HOME",
			"cd ~", "ls ~", "cp f ~/", "mv f ~/", "tar xzf a.tgz -C ~", "unzip a.zip -d ~",
			// The documented listings.
			"ls -R ~", "ls -la ~/work", "du -a ~", "tree ~", "find ~ -name '*.json'",
		} {
			cases = append(cases, wcCase{from.name + ": " + c, from.dir, wcShell(c), ""})
		}
		cases = append(cases, wcCase{from.name + ": git status", from.dir, wcShell("git status"), ""})
	}
	m.run(t, cases)
}
