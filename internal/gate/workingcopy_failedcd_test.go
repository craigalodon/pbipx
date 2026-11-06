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

		// A cd after || is skipped when the left side succeeds.
		{"cd || cd && cat", h, wcShell("cd work || cd nosuch && cat copy/model.txt"), wc},
		{"cd || cd /tmp && cat", h, wcShell("cd work || cd /tmp && cat copy/model.txt"), wc},
		{"test || cd && cat", h, wcShell("test -d work || cd /tmp && cat work/copy/model.txt"), wc},
		{"[ ] || cd && cat", h, wcShell("[ -d work ] || cd /tmp && cat work/copy/model.txt"), wc},
		{"true || cd && cat", h, wcShell("true || cd nosuch && cat work/copy/model.txt"), wc},
		{"cd && cd || cd && cat", h, wcShell("cd work && cd . || cd /tmp && cat copy/model.txt"), wc},
		{"cd || pushd && cat", h, wcShell("cd work || pushd /tmp && cat copy/model.txt"), wc},
		{"true || (cd) && cat", h, wcShell("true || (cd /tmp) && cat work/copy/model.txt"), wc},

		// A subshell's cd does not carry out of it.
		{"(cd) && cat", h, wcShell("(cd work) && cat work/copy/model.txt"), wc},
		{"(cd && ls) && cat", h, wcShell("(cd work && ls) && cat work/copy/model.txt"), wc},
		{"(cd; cd) && cat", h, wcShell("(cd work; cd other) && cat work/copy/model.txt"), wc},
		{"(pushd) && cat", h, wcShell("(pushd work) && cat work/copy/model.txt"), wc},
		{"( (cd) ) && cat", h, wcShell("( (cd work) ) && cat work/copy/model.txt"), wc},
		{"(cd) && ls && cat", h, wcShell("(cd work) && ls && cat work/copy/model.txt"), wc},
		{"<(cd) && cat", h, wcShell("cat <(cd work) && cat work/copy/model.txt"), wc},
		{"cd && (cd) && cat", h, wcShell("cd work && (cd other) && cat copy/model.txt"), wc},

		// Unchanged.
		{"cd || a, from outside", "", wcShell("cd sub || true; cat model.txt"), ""},
		{"cd && cd && a", h, wcShell("cd nosuch && cd deeper && cat work/copy/model.txt"), ""},
		{"cd && a", h, wcShell("cd nosuch && cat work/copy/model.txt"), ""},
		{"cd && a && b", h, wcShell("cd nosuch && true && cat work/copy/model.txt"), ""},
		{"cd, from outside", "", wcShell("cd sub; cat model.txt"), ""},
		{"cd && a, from outside", "", wcShell("cd sub && cat model.txt"), ""},
		{"cd .. then cat", m.outside, wcShell("cd ..; cat notes.txt"), ""},
	})
}
