package gate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopySymlinkTextDot checks a link text that ends in /. (or /.//.),
// and a link made through a folder that is itself a symlink.
func TestWorkingCopySymlinkTextDot(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	h := m.ctl.home
	if err := os.MkdirAll(filepath.Join(h, "a", "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h, "a", "b"), filepath.Join(h, "dl")); err != nil {
		t.Fatal(err)
	}
	m.run(t, []wcCase{
		{"text ending in /.", "", wcShell("ln -s ../../work/copy/. ~/a/b/L"), wc},
		{"text ending in /.//.", "", wcShell("ln -s ../../work/copy/.//. ~/a/b/L"), wc},
		{"through a symlinked folder", "", wcShell("ln -s ../../work/copy/. ~/dl/L"), wc},
		{"through a symlinked folder, plain", "", wcShell("ln -s ../../work/copy ~/dl/L"), wc},
		{"from the folder", filepath.Join(h, "a", "b"), wcShell("ln -s ../../work/copy/. L"), wc},
		{"cp -rL over such a destination", "", wcShell("ln -s ../../work/copy/. ~/a/b/L && cp -rL ~/a/b/L out"), wc},
		{"grep -R over it", "", wcShell("ln -s ../../work/copy/. ~/a/b/L && grep -R x ~/a/b"), wc},
		{"rg -L over it", "", wcShell("ln -s ../../work/copy/. ~/a/b/L && rg -L x ~/a/b"), wc},
		{"a text that stays out", "", wcShell("ln -s ../../other/. ~/a/b/L"), ""},
	})
}
