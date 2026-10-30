package gate_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// gitBashPath is the absolute Windows path p as Git Bash spells it:
// /c/Users/... .
func gitBashPath(p string) string {
	return "/" + strings.ToLower(p[:1]) + filepath.ToSlash(p[2:])
}

// TestGitBashPathsWindows runs the gate with the platform's own reading of
// Git Bash paths, on real paths.
func TestGitBashPathsWindows(t *testing.T) {
	t.Cleanup(gate.SetHostMSYS())
	root := newRepo(t, cleanFiles())
	write(t, root, warnVisual, slicer("student", "full_name", "Doe, Jane"))
	if filepath.VolumeName(root) == "" || len(filepath.VolumeName(root)) != 2 {
		t.Skip("the temporary directory is not on a drive")
	}
	r := gitBashPath(root)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	read := func(p string) gate.Event { return gate.Event{Tool: gate.Read, Path: p} }
	upper := "/" + strings.ToUpper(r[1:2]) + r[2:]
	run(t, root, []decideCase{
		{"cat a leaky file by drive", sh("cat " + r + "/" + warnVisual), gate.ReasonFinding},
		{"cat a leaky file by a glob", sh("cat " + r + "/pbi*/A.Report/definition/pages/0000/visuals/0001/visual.json"), gate.ReasonFinding},
		{"upper-case drive letter", sh("cat " + upper + "/pbi*/A.Report/definition/report.json"), gate.ReasonFinding},
		{"cd by drive", sh("cd " + r + "/pbi* && cat A.Report/definition/report.json"), gate.ReasonFinding},
		{"git diff of the root", sh("git diff -- " + r + "/pbip"), gate.ReasonGitContent},
		{"git -C by drive", sh("git -C " + r + " diff"), gate.ReasonGitContent},
		{"git diff limited away", sh("git diff -- " + r + "/notes"), ""},
		{"cat a clean file", sh("cat " + r + "/README.md"), ""},
		{"cat a clean file, Windows form", sh("cat " + shellPath(root) + "/README.md"), ""},
		{"cat a clean file to /dev/null", sh("cat " + r + "/README.md > /dev/null 2>&1"), ""},
		{"read a leaky file in Git Bash form", read(r + "/" + warnVisual), gate.ReasonFinding},
		{"read a clean file in Git Bash form", read(r + "/README.md"), ""},
		{"read a clean file", read(filepath.Join(root, "README.md")), ""},
	})

	// /tmp is the user's temporary directory in Git Bash, where the
	// repository is: placed by cygpath, when there is one.
	cyg := gate.Cygpath()
	if cyg == "" {
		t.Log("no cygpath: /tmp is not placed")
		return
	}
	out, err := exec.Command(cyg, "-w", "/tmp").Output() //nolint:gosec // G204: the cygpath the gate found, with a fixed argument
	if err != nil {
		t.Fatalf("cygpath: %v", err)
	}
	tmp, err1 := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	resolved, err2 := filepath.EvalSymlinks(root)
	if err1 != nil || err2 != nil {
		t.Skip("the temporary directories cannot be resolved")
	}
	rel, err := filepath.Rel(tmp, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Logf("the repository is not under Git Bash's /tmp")
		return
	}
	viaTmp := "/tmp/" + filepath.ToSlash(rel)
	run(t, root, []decideCase{
		{"cat through /tmp", sh("cat " + viaTmp + "/pbi*/A.Report/definition/report.json"), gate.ReasonFinding},
		{"clean file through /tmp", sh("cat " + viaTmp + "/README.md"), ""},
	})
}
