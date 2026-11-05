//go:build !windows

package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyMSYSPaths checks that on Windows, where Claude Code runs
// shell commands in Git Bash, a working copy named by a path in Git Bash
// form that is not a drive path (/tmp/x, a mount) is placed and denied.
func TestWorkingCopyMSYSPaths(t *testing.T) {
	m := newWCMachine(t)
	elsewhere := t.TempDir()
	useMounts(t, map[string]string{
		"/tmp/desk":  m.wc,
		"/tmp/sib":   m.sibling,
		"/tmp/other": elsewhere,
		"/tmp/home":  m.ctl.home,
	})
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"cat through a mount", "", wcShell("cat /tmp/desk/" + wcCache), wc},
		{"cat through a mount, quoted", "", wcShell(`cat "/tmp/desk/` + wcCache + `"`), wc},
		{"cat through a mount, with ..", "", wcShell("cat /tmp/other/../desk/x.txt"), wc},
		{"cat through a mount, by glob", "", wcShell("cat /tmp/desk/*/.pbi/*.abf"), wc},
		{"redirect into a mount", "", wcShell("echo hi > /tmp/desk/new.json"), wc},
		{"redirect out of a mount", "", wcShell("cat < /tmp/desk/x.txt"), wc},
		{"cd into a mount", "", wcShell("cd /tmp/desk && ls"), wc},
		{"ls of a mount", "", wcShell("ls /tmp/desk"), wc},
		{"grep -r of a mount above it", "", wcShell("grep -r Doe /tmp/home"), wc},
		{"read through a mount", "", wcRead("/tmp/desk/x.txt"), wc},
		{"write through a mount", "", wcWrite("/tmp/desk/new.json"), wc},
		{"search through a mount", "", wcSearch("/tmp/desk"), wc},
		{"search of a mount above it", "", wcSearch("/tmp/home"), wc},
		{"lsp through a mount", "", wcLSP("/tmp/desk/x.txt"), wc},
		{"read of the sibling through a mount", "", wcRead("/tmp/sib/x.txt"), ""},
		{"cat of the sibling through a mount", "", wcShell("cat /tmp/sib/x.txt"), ""},
		{"cat of a file in another mount", "", wcShell("cat /tmp/other/x"), ""},
		{"ls of a mount above it", "", wcShell("ls /tmp/home"), ""},
		{"a path no mount places", "", wcRead("/nowhere/x"), gate.ReasonInternalError},
		{"cat of a path no mount places", "", wcShell("cat /nowhere/x"), wc},
	})
}
