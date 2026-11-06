package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyCDPATH checks that a line that names CDPATH, in any form,
// leaves the target of a later relative cd unknown, as a CDPATH in the
// gate's own environment does: cd takes a bare name from a folder the gate
// cannot tell.
func TestWorkingCopyCDPATH(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"export", "", wcShell("export CDPATH=$HOME/work; cd copy; cat model.txt"), wc},
		{"export, braces", "", wcShell("export CDPATH=${HOME}/work; cd copy; cat model.txt"), wc},
		{"a prefix assignment", "", wcShell("CDPATH=$HOME/work cd copy; ls; cat x"), wc},
		{"a prefix assignment, then cat", "", wcShell("CDPATH=$HOME/work cd copy && cat x"), wc},
		{"an assignment", "", wcShell("CDPATH=~/work; cd copy; cat x"), wc},
		{"declare", "", wcShell("declare -x CDPATH=~/work; cd copy && cat x"), wc},
		{"typeset", "", wcShell("typeset -x CDPATH=~/work; cd copy && cat x"), wc},
		{"read", "", wcShell("read CDPATH <<< x; cd copy && cat x"), wc},
		{"printf -v", "", wcShell("printf -v CDPATH %s ~/work; cd copy && cat x"), wc},
		{"env", "", wcShell("env CDPATH=~/work bash -c 'cd copy; cat x'"), wc},
		{"a nested line", "", wcShell("bash -c 'export CDPATH=~/work; cd copy; cat x'"), wc},
		{"&&", "", wcShell("export CDPATH=~/work && cd copy && cat x"), wc},
		{"cd -P", "", wcShell("export CDPATH=~/work; cd -P copy; cat x"), wc},
		{"pushd", "", wcShell("export CDPATH=~/work; pushd copy; cat x"), wc},
		{"eval", "", wcShell(`eval "export CDPATH=~/work"; cd copy; cat x`), wc},
		{"source", "", wcShell("source env.sh; cd copy; cat x"), wc},
		{"dot command", "", wcShell(". ./env.sh; cd copy; cat x"), wc},
		{"dot command after a semicolon", "", wcShell("true; . ./env.sh; cd copy; cat x"), wc},
		{"unset first", "", wcShell("unset CDPATH; export CDPATH=~/work; cd copy; cat x"), wc},
		{"from a folder", m.outside, wcShell("export CDPATH=~/work; cd copy; cat model.txt"), wc},

		// Allowed.
		{"no CDPATH", "", wcShell("cd sub; cat x"), ""},
		{"a dot path", "", wcShell("export CDPATH=~/work; cd ./sub; cat x"), ""},
		{"a dotdot path", "", wcShell("export CDPATH=~/work; cd ../sub; cat x"), ""},
		{"an absolute path", "", wcShell("export CDPATH=~/work; cd /tmp; cat x"), ""},
		{"a reference only", "", wcShell("echo $CDPATH; cd sub; cat x"), ""},
		{"a braced reference", "", wcShell("echo ${CDPATH}; cd sub; cat x"), ""},
		{"a dot in a path", "", wcShell("git add .; cd sub; cat x"), ""},
		{"export of another name", "", wcShell("export OTHER=~/work; cd sub; cat x"), ""},
	})
}
