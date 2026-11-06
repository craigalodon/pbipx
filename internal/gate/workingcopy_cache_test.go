package gate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyCacheByName checks the backstop for a copy the registry no
// longer finds (its folder was moved): while any entry exists, stale or not,
// a Read, a Grep path or a reader's word naming a path that ends in
// .pbi/cache.abf denies with working-copy.
func TestWorkingCopyCacheByName(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	moved := m.wc + "-moved"
	if err := os.Rename(m.wc, moved); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(moved, wcCache)
	other := filepath.Join(m.outside, "proj", "Example.SemanticModel", ".pbi", "cache.abf")
	write(t, m.outside, "proj/Example.SemanticModel/.pbi/cache.abf", "x")
	write(t, m.outside, "proj/Example.SemanticModel/.pbi/cache.txt", "x")
	write(t, m.outside, "proj/cache.abf", "x")
	m.run(t, []wcCase{
		{"Read of the moved copy's cache", "", wcRead(cache), wc},
		{"Read of a cache in any folder", "", wcRead(other), wc},
		{"Grep path", "", wcSearch(cache), wc},
		{"cat", "", wcShell("cat " + bashPath(cache)), wc},
		{"cat relative", m.outside, wcShell("cat proj/Example.SemanticModel/.pbi/cache.abf"), wc},
		{"cat relative after cd", "", wcShell("cd " + bashPath(filepath.Join(m.outside, "proj")) + " && cat Example.SemanticModel/.pbi/cache.abf"), wc},
		{"cp out of it", "", wcShell("cp " + bashPath(cache) + " /tmp/x"), wc},
		{"strings", "", wcShell("strings " + bashPath(cache)), wc},
		{"an input redirection", "", wcShell("wc -c < " + bashPath(cache)), wc},
		{"a dotted spelling", "", wcShell("cat " + bashPath(filepath.Dir(cache)) + "/../.pbi/./cache.abf"), wc},

		// Not the cache, or not a read of it.
		{"the cache's folder", "", wcShell("ls " + bashPath(filepath.Dir(other))), ""},
		{"another file in .pbi", "", wcRead(filepath.Join(filepath.Dir(other), "cache.txt")), ""},
		{"a cache.abf outside .pbi", "", wcRead(filepath.Join(m.outside, "proj", "cache.abf")), ""},
		{"echo of the path", "", wcShell("echo " + bashPath(other)), ""},
		{"test -e", "", wcShell("test -e " + bashPath(other)), ""},
		{"rm", "", wcShell("rm " + bashPath(other)), ""},
		{"a name that only ends alike", "", wcShell("cat " + bashPath(other) + ".bak"), ""},
		{"a Write of it", "", wcWrite(other), ""},
	})

	// With no entry at all the rule is off.
	if err := os.RemoveAll(filepath.Join(m.ctl.store, "stages")); err != nil {
		t.Fatal(err)
	}
	m.run(t, []wcCase{
		{"no entry: Read", "", wcRead(other), ""},
		{"no entry: cat", "", wcShell("cat " + bashPath(other)), ""},
	})
}
