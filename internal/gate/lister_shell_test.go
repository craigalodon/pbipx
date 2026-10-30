package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellListers checks that a command that only lists names is allowed
// over a protected root whatever the tree holds, and that one that reads
// content, or a listing fed to a reader, is still judged.
func TestShellListers(t *testing.T) {
	runBoth(t, []both{
		{"ls -R", sh("ls -R pbip"), "", ""},
		{"ls -la", sh("ls -la pbip/B.Report"), "", ""},
		{"ls from inside a root", shIn("pbip/A.Report", "ls -R"), "", ""},
		{"ls of a glob", sh("ls -d p*"), "", ""},
		{"tree", sh("tree -a pbip"), "", ""},
		{"du", sh("du -a pbip"), "", ""},
		{"stat", sh("stat pbip/B.Report/" + leakInReport), "", ""},
		{"find -name", sh("find pbip -name '*.json' -type f"), "", ""},
		{"find -print0", sh("find pbip -type f -print0"), "", ""},
		{"find -printf names and sizes", sh(`find pbip -printf '%p %s\n'`), "", ""},
		{"find -newer -ls", sh("find pbip -newermt 2020-01-01 -ls"), "", ""},
		{"a listing, then a read away", sh("ls -R pbip; cat README.md"), "", ""},
		{"a listing in a substitution", sh("echo $(ls pbip)"), "", ""},

		{"a listing, then a read of the root", sh("ls pbip && cat pbip/B.Report/" + leakInReport), "", gate.ReasonFinding},
		{"a listing fed to xargs cat", sh("ls pbip | xargs cat"), "", gate.ReasonFinding},
		{"find -exec", sh("find pbip -exec cat {} +"), "", gate.ReasonFinding},
		{"find -printf with an unknown directive", sh(`find pbip -printf '%Z\n'`), "", gate.ReasonFinding},
		{"find -fprint", sh("find pbip -fprint /tmp/x"), "", gate.ReasonFinding},
		{"find -delete", sh("find pbip -name x -delete"), "", gate.ReasonFinding},
		{"tree --fromfile", sh("tree --fromfile pbip/B.Report/" + leakInReport), "", gate.ReasonFinding},
		{"tree -o", sh("tree -o /tmp/t pbip"), "", gate.ReasonFinding},
		{"du --files0-from", sh("du --files0-from=pbip/B.Report/" + leakInReport), "", gate.ReasonFinding},
		{"file reads content", sh("file pbip/B.Report/" + leakInReport), "", gate.ReasonFinding},
		{"wc reads content", sh("wc -l pbip/B.Report/" + leakInReport), "", gate.ReasonFinding},
		{"a local-state directory", sh("ls pbip/A.Report/.pbi/"), gate.ReasonLocalState, gate.ReasonLocalState},
	})
}
