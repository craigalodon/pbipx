package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopySymlinkText checks that the text of a symbolic link, which
// the system resolves against the link's folder, is placed against that
// folder as well as against the working directory.
func TestWorkingCopySymlinkText(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"relative text from elsewhere", "", wcShell("ln -s work/copy ~/L"), wc},
		{"then a read through it", "", wcShell("ln -s work/copy ~/L && cat ~/L/.pbi/cache.abf"), wc},
		{"link in the folder holding the copy", "", wcShell("ln -s copy ~/work/L"), wc},
		{"link into the folder holding the copy", "", wcShell("ln -s copy ~/work/"), wc},
		{"-t", "", wcShell("ln -st ~/work copy"), wc},
		{"-sfT", "", wcShell("ln -sfT copy ~/work/L"), wc},
		{"--symbolic", "", wcShell("ln --symbolic copy ~/work/L"), wc},
		{"--sym abbreviation", "", wcShell("ln --sym copy ~/work/L"), wc},
		{"after --", "", wcShell("ln -s -- copy ~/work/L"), wc},
		{"-r", "", wcShell("ln -sr copy ~/work/L"), wc},
		{"an option the table lacks", "", wcShell("ln -s --weird copy ~/work/L"), wc},
		{"a destination it cannot tell", "", wcShell(`ln -s work/copy "$D"`), wc},
		{"a dotted text", "", wcShell("ln -s ../work/copy ~/sub/L"), wc},
		{"text with a dot first", "", wcShell("ln -s ./copy ~/work/L"), wc},
		{"a folder above the copy", "", wcShell("ln -s work ~/L"), wc},
		{"a second source", "", wcShell("ln -s a copy ~/work/"), wc},
		{"from a folder with the copy's name elsewhere", m.outside, wcShell("ln -s copy ~/work/L"), wc},

		// Allowed.
		{"relative text elsewhere", "", wcShell("ln -s ../x ~/y"), ""},
		{"a plain link", "", wcShell("ln -s target L"), ""},
		{"a link in the home folder", "", wcShell("ln -s src/a ~/L2"), ""},
		{"an absolute text", "", wcShell("ln -s /usr/bin/env ~/L"), ""},
		{"a hard link", "", wcShell("ln f ~/g"), ""},
		{"a hard link of a relative path", "", wcShell("ln work/other ~/L"), ""},
		{"into a sibling folder", "", wcShell("ln -s a " + bashPath(m.sibling) + "/L"), ""},
	})
}
