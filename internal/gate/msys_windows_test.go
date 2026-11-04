//go:build windows

package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/gate"
)

// These tests run Git Bash's own cygpath, found as the gate finds it, so
// they need Git for Windows. They skip when it cannot be found.

// requireCygpath skips the test when the gate cannot place /tmp paths.
func requireCygpath(t *testing.T, cwd string) {
	t.Helper()
	d := gate.Decide(gate.Event{Tool: gate.Read, Path: "/tmp/pbipx-no-such-file", Cwd: cwd}, gate.Options{})
	if d.Reason == gate.ReasonInternalError {
		t.Skip("Git for Windows' cygpath.exe was not found")
	}
}

// tmpForm is the absolute Windows path p, which is under the temporary
// directory, as Git Bash writes it under /tmp, assuming /tmp is
// %TEMP%, as it is in Git for Windows. It skips the test when p is not
// under the temporary directory.
func tmpForm(t *testing.T, p string) string {
	t.Helper()
	rel, err := filepath.Rel(os.TempDir(), p)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Skip("the test directory is not under the temporary directory")
	}
	return "/tmp/" + filepath.ToSlash(rel)
}

// newRepoAt is newRepo in the directory root.
func newRepoAt(t *testing.T, root string) {
	t.Helper()
	isolateGit(t)
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	write(t, root, ".pbipx.toml", defaultPolicy(t))
	for name, content := range leakyFiles() {
		write(t, root, name, content)
	}
	if err := corpus.GitSetup(root, nil, nil); err != nil {
		t.Fatal(err)
	}
}

// TestCygpathPlacesTmp checks /tmp paths against a real cygpath: the
// repository is in the temporary directory, which Git Bash mounts at /tmp.
func TestCygpathPlacesTmp(t *testing.T) {
	root := newRepo(t, leakyFiles())
	requireCygpath(t, root)
	base := tmpForm(t, root)
	finding := gate.ReasonFinding
	run(t, root, []decideCase{
		// A glob keeps the line from spelling the root's name.
		{"cat a leak", sh("cat " + base + "/p*/B.Report/" + leakInReport), finding},
		{"cd into the root", sh("cd " + base + "/pbip && cat B.Report/" + leakInReport), finding},
		{"cat a file outside the roots", sh("cat " + base + "/README.md"), ""},
		{"cat a file that does not exist", sh("cat " + base + "/notes/no-such-file"), ""},
		{"git -C the repository", sh("git -C " + base + " log -p"), gate.ReasonGitContent},
		{"sh -c", sh("sh -c 'cat " + base + "/p*/B.Report/" + leakInReport + "'"), finding},
		{"/usr is not the working drive's folder", sh("cat /usr/share/pbipx-no-such-file"), ""},
		{"cygdrive is a folder under the install, not a drive", sh("cat /cygdrive/c/pbipx-no-such-file"), ""},
		{"/dev/null", sh("cat README.md 2>/dev/null"), ""},
		{"read a leak", gate.Event{Tool: gate.Read, Path: base + "/" + leakVisual}, finding},
		{"read a file outside the roots", gate.Event{Tool: gate.Read, Path: base + "/README.md"}, ""},
		{"search the root", gate.Event{Tool: gate.Search, Path: base + "/pbip"}, finding},
		{"write settings", gate.Event{Tool: gate.Write, Path: base + "/.claude/settings.json"}, gate.ReasonAgentSettings},
	})
}

// TestCygpathPlacesANonASCIIDirectory checks that a directory whose name is
// not ASCII is placed as it is spelled, in UTF-8.
func TestCygpathPlacesANonASCIIDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "répertoire-ünï-日本")
	newRepoAt(t, root)
	requireCygpath(t, root)
	base := tmpForm(t, root)
	finding := gate.ReasonFinding
	run(t, root, []decideCase{
		{"cat a leak", sh("cat " + base + "/p*/B.Report/" + leakInReport), finding},
		{"cat a leak, quoted", sh(`cat "` + base + `"/p*/B.Report/` + leakInReport), finding},
		{"cat a file outside the roots", sh("cat " + base + "/README.md"), ""},
		{"read a leak", gate.Event{Tool: gate.Read, Path: base + "/" + leakVisual}, finding},
	})
}

// TestProcSelfCwdIsUnknown checks the paths Git Bash's procfs leads into
// the working directory by: measured on Git Bash, "cat /proc/self/cwd/x"
// reads x in the working directory. They are unknown, whatever their case,
// and cygpath is not asked about them.
func TestProcSelfCwdIsUnknown(t *testing.T) {
	root := newRepo(t, leakyFiles())
	finding, gitContent, internal := gate.ReasonFinding, gate.ReasonGitContent, gate.ReasonInternalError
	var cases []decideCase
	for _, p := range []string{"/proc/self/cwd", "/proc/self/root", "/proc/12/cwd", "/PROC/Self/CWD"} {
		cases = append(cases,
			decideCase{"cat " + p, sh("cat " + p + "/README.md"), finding},
			decideCase{"redirect from " + p, sh("wc -l < " + p + "/README.md"), finding},
			decideCase{"cd " + p, sh("cd " + p + " && cat README.md"), finding},
			decideCase{"git -C " + p, sh("git -C " + p + " log -p"), gitContent},
			decideCase{"read " + p, gate.Event{Tool: gate.Read, Path: p + "/README.md"}, internal},
			decideCase{"search " + p, gate.Event{Tool: gate.Search, Path: p}, internal},
		)
	}
	cases = append(cases,
		decideCase{"the standard descriptors are no file", sh("cat /proc/self/fd/0 /dev/null"), ""},
	)
	run(t, root, cases)
}
