package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyFailedCd checks that a cd the line does not chain with &&
// leaves the working directory possibly the old one (the cd may fail), for
// what follows.
func TestWorkingCopyFailedCd(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	h := m.ctl.home
	m.run(t, []wcCase{
		{"cd; cat", h, wcShell("cd nosuch; cat work/copy/model.txt"), wc},
		{"cd; rg -l | xargs", h, wcShell("cd nosuch; rg -l x | xargs cat"), wc},
		{"cd newline cat", h, wcShell("cd nosuch\ncat work/copy/model.txt"), wc},
		{"pushd; cat", h, wcShell("pushd nosuch; cat work/copy/model.txt"), wc},
		{"cd ||", h, wcShell("cd a || cd b; cat work/copy/model.txt"), wc},
		{"cd a || cd b chain of three", h, wcShell("cd a/b || cd c; cat ../../work/copy/model.txt"), wc},
		{"cd && a; b", h, wcShell("cd nosuch && true; cat work/copy/model.txt"), wc},
		{"cd && cd; b", h, wcShell("cd nosuch && cd deeper; cat work/copy/model.txt"), wc},
		{"cd in the middle", h, wcShell("true; cd nosuch; cat work/copy/model.txt"), wc},
		{"cd with a dotted target", h + "/work", wcShell("cd ./nosuch; cat copy/model.txt"), wc},

		// Unchanged.
		{"cd && a", h, wcShell("cd nosuch && cat work/copy/model.txt"), ""},
		{"cd && a && b", h, wcShell("cd nosuch && true && cat work/copy/model.txt"), ""},
		{"cd, from outside", "", wcShell("cd sub; cat model.txt"), ""},
		{"cd && a, from outside", "", wcShell("cd sub && cat model.txt"), ""},
		{"cd .. then cat", m.outside, wcShell("cd ..; cat notes.txt"), ""},
	})
}
