//go:build windows

package gate_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// gitBashForm is the absolute Windows path p as Git Bash writes it:
// "C:\x\y" is "/c/x/y".
func gitBashForm(p string) string {
	vol := filepath.VolumeName(p)
	return "/" + strings.ToLower(strings.TrimSuffix(vol, ":")) + filepath.ToSlash(p[len(vol):])
}

// TestWindowsPathForms checks that every form a Windows path takes in an
// event or a Git Bash command line reaches the same file: "C:\x" quoted,
// "C:/x", Git Bash's "/c/x", and "\x" rooted on the working directory's
// drive.
func TestWindowsPathForms(t *testing.T) {
	root := newRepo(t, leakyFiles())
	t.Setenv("HOME", gitBashForm(filepath.Join(root, "notes")))
	leak := "pbip/B.Report/" + leakInReport
	native := filepath.Join(root, filepath.FromSlash(leak))
	vol := filepath.VolumeName(root)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd, Cwd: "notes"} }
	run(t, root, []decideCase{
		{"quoted backslashes", sh(`cat "` + native + `"`), gate.ReasonFinding},
		{"forward slashes", sh("cat " + bashPath(native)), gate.ReasonFinding},
		{"Git Bash drive", sh("cat " + gitBashForm(native)), gate.ReasonFinding},
		{"Git Bash drive, then ..", sh("cat " + gitBashForm(filepath.Join(root, "notes")) + "/../" + leak), gate.ReasonFinding},
		{"cd to a Git Bash drive", sh("cd " + gitBashForm(root) + " && cat " + leak), gate.ReasonFinding},
		{"~ in Git Bash form", sh("cat ~/../" + leak), gate.ReasonFinding},
		{"a Git Bash drive away from the roots", sh("cat " + gitBashForm(filepath.Join(root, "README.md"))), ""},
		{"git -C a Git Bash drive", sh("git -C " + gitBashForm(root) + " log -p"), gate.ReasonGitContent},
		{"git -C a Git Bash drive away from the roots", sh("git -C " + gitBashForm(filepath.Join(root, "notes")) + " log -p -- ."), ""},

		{"read rooted on the drive", gate.Event{Tool: gate.Read, Path: native[len(vol):]}, gate.ReasonFinding},
		{"read with forward slashes", gate.Event{Tool: gate.Read, Path: filepath.ToSlash(native)}, gate.ReasonFinding},
		{"read in Git Bash form", gate.Event{Tool: gate.Read, Path: gitBashForm(native)}, gate.ReasonFinding},
		{"write settings in Git Bash form", gate.Event{Tool: gate.Write, Path: gitBashForm(filepath.Join(root, ".claude", "settings.json"))}, gate.ReasonAgentSettings},
	})
}
