package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// msysRepo is a repository at drives/c/work/repo, with the Git Bash
// reading on and drive c at drives/c, so /c/work/repo names it as Git Bash
// on Windows would. Its protected tree is committed clean, then the
// tracked warnVisual is changed to leak, so git diff and the disk both
// hold the value.
func msysRepo(t *testing.T, mount func(string) (string, bool)) (root, drives string) {
	t.Helper()
	drives = t.TempDir()
	root = filepath.Join(drives, "c", "work", "repo")
	newRepoAt(t, root, cleanFiles())
	write(t, root, warnVisual, slicer("student", "full_name", "Doe, Jane"))
	t.Cleanup(gate.SetMSYS(drives, mount))
	return root, drives
}

// The changed visual by a glob that does not spell the root's name, so
// only placing the word reaches it.
const leakGlob = "/c/work/repo/pbi*/A.Report/definition/pages/0000/visuals/0001/visual.json"

func TestShellGitBashPaths(t *testing.T) {
	root, drives := msysRepo(t, nil)
	if err := os.MkdirAll(filepath.Join(drives, "c", "work", "home"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "/c/work/home")
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	shIn := func(dir, cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd, Cwd: dir} }
	run(t, root, []decideCase{
		{"cat a leaky file by drive", sh("cat /c/work/repo/" + warnVisual), gate.ReasonFinding},
		{"cat a leaky file by a glob", sh("cat " + leakGlob), gate.ReasonFinding},
		{"upper-case drive letter", sh("cat /C/work/repo/pbi*/A.Report/definition/report.json"), gate.ReasonFinding},
		{"cygdrive", sh("cat /cygdrive/c/work/repo/pbi*/A.Report/definition/report.json"), gate.ReasonFinding},
		{"from outside the root", shIn("notes", "cat "+leakGlob), gate.ReasonFinding},
		{"redirected in", sh("wc -l < " + leakGlob), gate.ReasonFinding},
		{"after =", sh("jq . --rawfile=/c/work/repo/pbi*/A.Report"), gate.ReasonFinding},
		{"cd by drive", sh("cd /c/work/repo/pbi* && cat A.Report/definition/report.json"), gate.ReasonFinding},
		{"home in Git Bash form", sh("cat ~/../repo/pbi*/A.Report/definition/report.json"), gate.ReasonFinding},
		{"git diff of the root", sh("git diff -- /c/work/repo/pbip"), gate.ReasonGitContent},
		{"git diff of the repository", sh("git diff -- /c/work/repo"), gate.ReasonGitContent},
		{"git -C by drive", shIn("notes", "git -C /c/work/repo diff"), gate.ReasonGitContent},
		{"git diff limited away", sh("git diff -- /c/work/repo/notes"), ""},
		{"cat a file outside the roots", sh("cat /c/work/repo/README.md"), ""},
		{"cat outside the repository", sh("cat /c/work/elsewhere.md"), ""},
		{"/dev/null", sh("cat /c/work/repo/README.md 2>/dev/null"), ""},
		{"source /dev/stdin", sh("source /dev/stdin <<'EOF'\ngit log -p\nEOF"), gate.ReasonGitContent},
		// Off the drives, with no cygpath: the word could be anything.
		{"unplaceable read", sh("cat /etc/x"), gate.ReasonFinding},
		{"unplaceable git pathspec", sh("git diff -- /etc/x"), gate.ReasonGitContent},
		{"unplaceable cd", sh("cd /etc && cat x"), gate.ReasonFinding},
	})
}

func TestShellGitBashMount(t *testing.T) {
	// /tmp is a mount of the directory the repository is in, placed as
	// cygpath places it; nothing else is.
	tmp := ""
	mount := func(p string) (string, bool) {
		if p != "/tmp" && !strings.HasPrefix(p, "/tmp/") {
			return "", false
		}
		return tmp + filepath.FromSlash(p[len("/tmp"):]), true
	}
	root, _ := msysRepo(t, mount)
	tmp = filepath.Dir(root)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	run(t, root, []decideCase{
		{"cat through a mount", sh("cat /tmp/repo/pbi*/A.Report/definition/report.json"), gate.ReasonFinding},
		{"glob in the mount's name", sh("cat /tm*/repo/pbi*/A.Report/definition/report.json"), gate.ReasonFinding},
		{"clean file through a mount", sh("cat /tmp/repo/README.md"), ""},
	})
}

func TestReadGitBashPaths(t *testing.T) {
	root, _ := msysRepo(t, nil)
	read := func(p string) gate.Event { return gate.Event{Tool: gate.Read, Path: p} }
	grep := func(p string) gate.Event { return gate.Event{Tool: gate.Search, Path: p} }
	run(t, root, []decideCase{
		{"read a leaky file", read("/c/work/repo/" + warnVisual), gate.ReasonFinding},
		{"read a clean file", read("/c/work/repo/README.md"), ""},
		{"search the root", grep("/c/work/repo/pbip"), gate.ReasonFinding},
		{"search outside the roots", grep("/c/work/repo/sql"), ""},
	})
	// A working directory in Git Bash form.
	d := gate.Decide(gate.Event{Tool: gate.Read, Path: warnVisual, Cwd: "/c/work/repo"}, gate.Options{})
	check(t, d, gate.ReasonFinding)
}
