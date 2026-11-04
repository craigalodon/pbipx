//go:build !windows

package gate_test

import (
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// mountTable is a stand-in for Git Bash's mount table, as cygpath -w says
// it: each prefix is a directory somewhere else. Anything else cannot be
// placed. The root is never asked about.
type mountTable struct {
	mounts map[string]string
	runs   atomic.Int32
}

func (m *mountTable) place(arg string) (string, bool) {
	m.runs.Add(1)
	clean := path.Clean(arg)
	for prefix, dir := range m.mounts {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return dir + strings.TrimPrefix(clean, prefix), true
		}
	}
	return "", false
}

// useMounts turns the Git Bash reading on over the table.
func useMounts(t *testing.T, mounts map[string]string) *mountTable {
	t.Helper()
	m := &mountTable{mounts: mounts}
	t.Cleanup(gate.SetMSYS(true, m.place))
	return m
}

func TestMSYSMountedPaths(t *testing.T) {
	root := newRepo(t, leakyFiles())
	elsewhere := t.TempDir()
	usr := t.TempDir()
	useMounts(t, map[string]string{
		"/tmp/repo":        root,
		"/cygdrive/z/repo": root,
		"/tmp/elsewhere":   elsewhere,
		"/usr":             usr,
		// The protected root, mounted where its name is not in the line.
		"/mnt/data": root + "/pbip",
	})
	finding, gitContent := gate.ReasonFinding, gate.ReasonGitContent
	cat := func(p string) string { return "cat " + p }
	// The tools judge a path whether or not the line names the root.
	report := "/tmp/repo/pbip/B.Report/definition/pages/0000/visuals/0001/visual.json"
	// Through the mount, the line never spells the root's name.
	data := "/mnt/data/B.Report/definition/pages/0000/visuals/0001/visual.json"
	run(t, root, []decideCase{
		// A reader.
		{"cat a leak through a mount", sh(cat(data)), finding},
		{"cat a leak through a mount, quoted", sh(`cat "` + data + `"`), finding},
		{"cat a leak through a custom cygdrive prefix", sh(cat("/cygdrive/z/repo/p*/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"redirected in", sh("wc -l < " + data), finding},
		{"cat a file outside the roots", sh(cat("/tmp/repo/README.md")), ""},
		{"cat a note", sh(cat("/tmp/repo/notes/leak.md")), ""},
		{"cat a file in another mount", sh(cat("/tmp/elsewhere/x")), ""},
		{"a path with .. through the mount", sh(cat("/mnt/data/../notes/../pb*/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"a drive path with .. into the mount", sh(cat("/c/../mnt/data/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"a path with . in it", sh(cat("/mnt/./data/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"a missing file is still placed", sh(cat("/mnt/data/B.Report/no-such-file")), finding},
		{"the root's directory itself", sh(cat("/mnt/data")), finding},

		// Globs: the directory before one is placed, never the glob.
		{"glob below the mount", sh(cat("/mnt/data/B.Report/definition/pages/*/visuals/*/visual.json")), finding},
		{"glob over the root's name", sh(cat("/tmp/repo/pb*/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"glob right below the mount", sh(cat("/tmp/repo/*/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"? glob", sh(cat("/tmp/repo/pbi?/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"bracket glob", sh(cat("/tmp/repo/pbi[p]/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"** glob", sh(cat("/tmp/repo/**/visual.json")), finding},
		{"glob away from the roots", sh(cat("/tmp/repo/notes/*.md")), ""},
		{"glob in another mount", sh(cat("/tmp/elsewhere/*.md")), ""},
		{"glob at the top of the table", sh(cat("/*")), finding},
		{"glob with .. after it", sh(cat("/tmp/repo/notes/*/../../p*/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},

		// The value after =.
		{"--file=", sh("grep --file=" + data + " x"), finding},
		{"--file= away from the roots", sh("grep --file=/tmp/repo/README.md x"), ""},
		{"a bare =", sh("echo a=/tmp/repo/notes/leak.md"), ""},
		{"cat of a=path", sh(cat("a=" + data)), finding},
		{"cp --target-directory=", sh("cp --target-directory=/mnt/data /tmp/elsewhere/x"), finding},

		// cd.
		{"cd into a root", sh("cd /mnt/data && cat B.Report/" + leakInReport), finding},
		{"cd into a root, quoted", sh(`cd "/mnt/data" && cat B.Report/` + leakInReport), finding},
		{"cd in a subshell", sh("(cd /mnt/data; cat B.Report/" + leakInReport + ")"), finding},
		{"pushd", sh("pushd /mnt/data >/dev/null && cat B.Report/" + leakInReport), finding},
		{"cd, then ..", sh("cd /tmp/repo/notes && cd ../p*/ && cat B.Report/" + leakInReport), finding},
		{"cd to a note directory", sh("cd /tmp/repo/notes && cat clean.md"), ""},
		{"cd to another mount", sh("cd /tmp/elsewhere && cat x"), ""},
		{"cd to an unplaceable path", sh("cd /tmp/nowhere && cat README.md"), finding},
		{"cd to the root", sh("cd / && cat README.md"), finding},

		// Searches and listers.
		{"rg in a root", sh("rg Doe /mnt/data"), finding},
		{"rg away from the roots", sh("rg Doe /tmp/repo/notes"), ""},
		{"grep -r in a root", sh("grep -r Doe /mnt/data"), finding},
		{"find in a root, reading", sh("find /mnt/data -name '*.json' -exec cat {} +"), finding},
		{"find away from the roots", sh("find /tmp/repo/notes -name '*.md' -exec cat {} +"), ""},
		{"fd in a root, running a reader", sh("fd -e json . /mnt/data -x cat"), finding},
		{"find piped to xargs", sh("find /mnt/data -name '*.json' | xargs cat"), finding},
		{"find at the top", sh("find / -name '*.json' -exec cat {} +"), finding},

		// Nested shells read the words as written, and place the paths in them.
		{"sh -c", sh("sh -c 'cat " + data + "'"), finding},
		{"bash -c with cd", sh(`bash -c "cd /mnt/data && cat B.Report/` + leakInReport + `"`), finding},
		{"sh -c with a drive-letter program", sh("sh -c '/c/tools/cat " + data + "'"), finding},
		{"sh -c with a mounted program", sh("sh -c '/usr/bin/cat " + data + "'"), finding},
		{"sh -c away from the roots", sh("sh -c 'cat /tmp/repo/README.md'"), ""},
		{"a here-document script", sh("sh <<'EOF'\ncat " + data + "\nEOF"), finding},
		{"command substitution", sh("echo $(cat " + data + ")"), finding},

		// git.
		{"git -C a root", sh("git -C /mnt/data log -p -- ."), gitContent},
		{"git -C the repository", sh("git -C /tmp/repo log -p"), gitContent},
		{"git -C, no content", sh("git -C /tmp/repo/notes status"), ""},
		{"git pathspec in a root", sh("git log -p -- /mnt/data/B.Report"), gitContent},
		{"git pathspec glob", sh("git log -p -- /tmp/repo/pb*"), gitContent},
		{"git given an unplaceable path", sh("git log -p -- /tmp/nowhere/x"), gitContent},
		{"git -C an unplaceable path", sh("git -C /tmp/nowhere log -p"), gitContent},

		// What cannot be placed.
		{"cat an unplaceable path", sh(cat("/tmp/nowhere/x")), finding},
		{"cat the root of the table", sh(cat("/")), finding},
		{"echo an unplaceable path", sh("echo /tmp/nowhere/x"), ""},
		{"mkdir an unplaceable path", sh("mkdir -p /tmp/nowhere/x"), ""},
		{"an unplaceable program is known by its name", sh("/opt/nowhere README.md"), ""},
		{"an unplaceable wrapper is not", sh("/opt/env README.md"), gitContent},
		{"ls the root of the table", sh("ls /"), ""},

		// Devices and process files stay as written.
		{"/dev/null", sh("cat README.md 2>/dev/null"), ""},
		{"cat /dev/null", sh(cat("/dev/null")), ""},
		{"source /dev/stdin", sh("source /dev/stdin <<'EOF'\ngit log -p\nEOF"), gitContent},
		{"a .. out of /dev", sh(cat("/dev/../mnt/data/B.Report/definition/pages/0000/visuals/0001/visual.json")), finding},
		{"/proc/self/fd/0", sh(cat("/proc/self/fd/0")), ""},
		{"/dev/fd/3", sh(cat("/dev/fd/3")), ""},
		{"/proc/12/fd/3", sh(cat("/proc/12/fd/3")), ""},
		{"/DEV/NULL", sh(cat("/DEV/NULL")), ""},
		// The rest of them can lead into the repository.
		{"/proc/self/cwd", sh(cat("/proc/self/cwd/README.md")), finding},
		{"/proc/self/root", sh(cat("/proc/self/root/tmp/repo/README.md")), finding},
		{"/PROC/self/cwd", sh(cat("/PROC/self/cwd/README.md")), finding},
		{"/proc/12/cwd", sh(cat("/proc/12/cwd/README.md")), finding},
		{"/proc with ..", sh(cat("/tmp/repo/../../proc/self/cwd/README.md")), finding},
		{"a drive path with .. into /proc", sh(cat("/c/../proc/self/cwd/README.md")), finding},
		{"/dev/sda", sh(cat("/dev/sda")), finding},
		{"cd /proc/self/cwd", sh("cd /proc/self/cwd && cat README.md"), finding},
		{"find /proc", sh("find /proc/self/cwd -name x -exec cat {} +"), finding},
		{"redirected in from /proc/self/cwd", sh("wc -l < /proc/self/cwd/README.md"), finding},
		{"redirected in from /proc/self/root", sh("sort < /proc/self/root/README.md"), finding},
		{"git given /proc/self/cwd", sh("git log -p -- /proc/self/cwd/README.md"), gitContent},
		{"git -C /proc/self/cwd", sh("git -C /proc/self/cwd log -p"), gitContent},
		{"git -C /proc/12/cwd", sh("git -C /proc/12/CWD log -p"), gitContent},
		{"echo /proc/self/cwd", sh("echo /proc/self/cwd"), ""},
		{"read /proc/self/cwd", gate.Event{Tool: gate.Read, Path: "/proc/self/cwd/README.md"}, gate.ReasonInternalError},
		{"search /proc/self/cwd", gate.Event{Tool: gate.Search, Path: "/proc/self/cwd"}, gate.ReasonInternalError},
		{"write /proc/self/cwd", gate.Event{Tool: gate.Write, Path: "/proc/self/cwd/.claude/settings.json"}, gate.ReasonInternalError},
		{"lsp /proc/self/root", gate.Event{Tool: gate.LSP, Paths: []string{"/PROC/self/root/x"}}, gate.ReasonInternalError},
		{"read /dev/sda", gate.Event{Tool: gate.Read, Path: "/dev/sda"}, gate.ReasonInternalError},
		{"read /dev/null", gate.Event{Tool: gate.Read, Path: "/dev/null"}, ""},

		// The file tools.
		{"read a leak", gate.Event{Tool: gate.Read, Path: report}, finding},
		{"read through a custom cygdrive prefix", gate.Event{Tool: gate.Read, Path: "/cygdrive/z/repo/" + leakVisual}, finding},
		{"read through a mount of the root", gate.Event{Tool: gate.Read, Path: data}, finding},
		{"read a file outside the roots", gate.Event{Tool: gate.Read, Path: "/tmp/repo/README.md"}, ""},
		{"read in another mount", gate.Event{Tool: gate.Read, Path: "/tmp/elsewhere/x"}, ""},
		{"read an unplaceable path", gate.Event{Tool: gate.Read, Path: "/tmp/nowhere/x"}, gate.ReasonInternalError},
		{"read the root of the table", gate.Event{Tool: gate.Read, Path: "/"}, gate.ReasonInternalError},
		{"read a drive path", gate.Event{Tool: gate.Read, Path: "/c/nowhere/x"}, ""},
		{"search a root", gate.Event{Tool: gate.Search, Path: "/tmp/repo/pbip"}, finding},
		{"search away from the roots", gate.Event{Tool: gate.Search, Path: "/tmp/repo/notes"}, ""},
		{"search an unplaceable path", gate.Event{Tool: gate.Search, Path: "/tmp/nowhere"}, gate.ReasonInternalError},
		{"write settings", gate.Event{Tool: gate.Write, Path: "/tmp/repo/.claude/settings.json"}, gate.ReasonAgentSettings},
		{"write a note", gate.Event{Tool: gate.Write, Path: "/tmp/repo/notes/new.md"}, ""},
		{"write an unplaceable path", gate.Event{Tool: gate.Write, Path: "/tmp/nowhere/x"}, gate.ReasonInternalError},
		{"edit a leak", gate.Event{Tool: gate.Edit, Path: report}, finding},
		{"edit settings", gate.Event{Tool: gate.Edit, Path: "/tmp/repo/.claude/settings.local.json"}, gate.ReasonAgentSettings},
		{"lsp a leak", gate.Event{Tool: gate.LSP, Paths: []string{"/tmp/repo/README.md", report}}, finding},
		{"lsp away from the roots", gate.Event{Tool: gate.LSP, Paths: []string{"/tmp/repo/README.md"}}, ""},
		{"lsp an unplaceable path", gate.Event{Tool: gate.LSP, Paths: []string{"/tmp/repo/README.md", "/tmp/nowhere/x"}}, gate.ReasonInternalError},
	})
}

// TestMSYSMountedPathsCleanRepository checks the same forms allow when the
// root holds nothing.
func TestMSYSMountedPathsCleanRepository(t *testing.T) {
	root := newRepo(t, cleanFiles())
	useMounts(t, map[string]string{"/tmp/repo": root})
	run(t, root, []decideCase{
		{"cat", sh("cat /tmp/repo/pbip/B.Report/definition/pages/0000/visuals/0001/visual.json"), ""},
		{"cd and cat", sh("cd /tmp/repo/pbip/A.Report && cat definition/report.json"), ""},
		{"glob", sh("cat /tmp/repo/pb*/A.Report/definition/report.json"), ""},
		{"read", gate.Event{Tool: gate.Read, Path: "/tmp/repo/pbip/A.Report/definition/report.json"}, ""},
	})
}

// TestMSYSReachFromOutside checks that a call from outside every
// repository finds an adopted one by the place Git Bash puts a path.
func TestMSYSReachFromOutside(t *testing.T) {
	a := newAdoption(t)
	useMounts(t, map[string]string{"/tmp/adopted": a.adopted, "/tmp/plain": a.plain, "/tmp/outside": a.outside, "/mnt/data": a.adopted + "/pbip"})
	report := "/tmp/adopted/" + leakVisual
	finding := gate.ReasonFinding
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		{"cat", gate.Event{Tool: gate.Shell, Command: "cat " + report}, finding},
		{"cat through a mount of the root", gate.Event{Tool: gate.Shell, Command: "cat /mnt/data/B.Report/" + leakInReport}, finding},
		{"cd to a mount of the root", gate.Event{Tool: gate.Shell, Command: "cd /mnt/data && cat B.Report/" + leakInReport}, finding},
		{"read through a mount of the root", gate.Event{Tool: gate.Read, Path: "/mnt/data/B.Report/" + leakInReport}, finding},
		{"cat, glob", gate.Event{Tool: gate.Shell, Command: "cat /tmp/adopted/pb*/B.Report/definition/pages/0000/visuals/0001/visual.json"}, finding},
		{"cd, then cat", gate.Event{Tool: gate.Shell, Command: "cd /tmp/adopted && cat " + leakVisual}, finding},
		{"find", gate.Event{Tool: gate.Shell, Command: "find /tmp/adopted -name '*.json' -exec cat {} +"}, finding},
		{"rg", gate.Event{Tool: gate.Shell, Command: "rg Doe /tmp/adopted"}, finding},
		{"sh -c", gate.Event{Tool: gate.Shell, Command: "sh -c 'cat " + report + "'"}, finding},
		{"git -C", gate.Event{Tool: gate.Shell, Command: "git -C /tmp/adopted log -p"}, gate.ReasonGitContent},
		{"git pathspec", gate.Event{Tool: gate.Shell, Command: "git -C /tmp/adopted log -p -- pbip"}, gate.ReasonGitContent},
		{"read", gate.Event{Tool: gate.Read, Path: report}, finding},
		{"search", gate.Event{Tool: gate.Search, Path: "/tmp/adopted/pbip"}, finding},
		{"lsp", gate.Event{Tool: gate.LSP, Paths: []string{report}}, finding},
		{"write settings", gate.Event{Tool: gate.Write, Path: "/tmp/adopted/.claude/settings.json"}, gate.ReasonAgentSettings},
		{"cat in a repository that has not adopted", gate.Event{Tool: gate.Shell, Command: "cat /tmp/plain/" + leakVisual}, ""},
		{"cat outside", gate.Event{Tool: gate.Shell, Command: "cat /tmp/outside/notes.txt"}, ""},
		{"read outside", gate.Event{Tool: gate.Read, Path: "/tmp/outside/notes.txt"}, ""},
		{"cat an unplaceable path", gate.Event{Tool: gate.Shell, Command: "cat /tmp/nowhere/x"}, finding},
		{"read an unplaceable path", gate.Event{Tool: gate.Read, Path: "/tmp/nowhere/x"}, gate.ReasonInternalError},
		{"echo an unplaceable path", gate.Event{Tool: gate.Shell, Command: "echo /tmp/nowhere/x"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := tc.ev
			ev.Cwd = a.outside
			check(t, a.decide(ev), tc.reason)
		})
	}
}

// TestMSYSNeverRunsForTheCommonForms pins that a drive path, a device, a
// Windows path and a relative one are never given to cygpath.
func TestMSYSNeverRunsForTheCommonForms(t *testing.T) {
	root := newRepo(t, leakyFiles())
	m := useMounts(t, map[string]string{})
	run(t, root, []decideCase{
		{"drive path", sh("cat /c/x"), ""},
		{"drive path, upper case", sh("cat /C/Users/me/x"), ""},
		{"drive root", sh("ls /c"), ""},
		{"drive glob", sh("cat /c/Users/*/x"), ""},
		{"cd to a drive path", sh("cd /c/x; cat y"), ""},
		{"git -C a drive path", sh("git -C /c/x status"), ""},
		{"drive path in sh -c", sh("sh -c 'cat /c/x'"), ""},
		{"--flag= a drive path", sh("grep --file=/c/x y"), ""},
		{"/dev/null", sh("cat /dev/null"), ""},
		{"2>/dev/null", sh("cat README.md 2>/dev/null"), ""},
		{"/dev/stdin", sh("cat /dev/stdin"), ""},
		{"/proc", sh("cat /proc/self/fd/0"), ""},
		{"Windows path", sh("cat C:/x"), ""},
		{"Windows path, quoted", sh(`cat "C:\x"`), ""},
		{"UNC path", sh("cat //server/share/x"), ""},
		{"relative", sh("cat notes/clean.md"), ""},
		{"read a drive path", gate.Event{Tool: gate.Read, Path: "/c/x"}, ""},
		{"read a Windows path", gate.Event{Tool: gate.Read, Path: "C:/x"}, ""},
		{"search a device", gate.Event{Tool: gate.Search, Path: "/dev/null"}, ""},
	})
	if n := m.runs.Load(); n != 0 {
		t.Errorf("cygpath ran %d times for forms that never need it", n)
	}
	// PowerShell words are never placed, whatever the gate makes of them.
	for _, c := range []string{"Get-Content /tmp/repo/" + leakVisual, "Get-Content /c/x", "cat /usr/bin/x"} {
		gate.Decide(gate.Event{Tool: gate.PowerShell, Command: c, Cwd: root}, gate.Options{})
	}
	if n := m.runs.Load(); n != 0 {
		t.Errorf("cygpath ran %d times for PowerShell", n)
	}
}

// TestMSYSOffWhenOffWindows pins that the reading is off elsewhere: nothing
// runs, and /tmp/x is the path it is.
func TestMSYSOffWhenOffWindows(t *testing.T) {
	root := newRepo(t, leakyFiles())
	m := &mountTable{mounts: map[string]string{"/tmp/repo": root, "/mnt/data": root + "/pbip"}}
	t.Cleanup(gate.SetMSYS(false, m.place))
	run(t, root, []decideCase{
		{"cat", sh("cat /mnt/data/B.Report/" + leakInReport), ""},
		{"cd", sh("cd /mnt/data && cat B.Report/" + leakInReport), ""},
		{"read", gate.Event{Tool: gate.Read, Path: "/tmp/repo/" + leakVisual}, ""},
		{"read the root", gate.Event{Tool: gate.Read, Path: "/"}, ""},
		{"an unplaceable path", sh("cat /tmp/nowhere/x"), ""},
		{"git -C", sh("git -C /mnt/data log -p -- ."), gate.ReasonGitContent},
	})
	if n := m.runs.Load(); n != 0 {
		t.Errorf("cygpath ran %d times off Windows", n)
	}
}

// TestMSYSRunBudgetInADecision checks the limit on runs: past it, a word is
// unknown, and a line of unplaceable words is judged as such.
func TestMSYSRunBudgetInADecision(t *testing.T) {
	root := newRepo(t, leakyFiles())
	m := useMounts(t, map[string]string{"/tmp/elsewhere": t.TempDir()})
	var words []string
	for i := range 40 {
		words = append(words, "/tmp/elsewhere/f"+strconv.Itoa(i))
	}
	run(t, root, []decideCase{
		{"a few words", sh("cat " + strings.Join(words[:10], " ")), ""},
		{"the same word again", sh("cat /tmp/elsewhere/a /tmp/elsewhere/a /tmp/elsewhere/a"), ""},
		{"more words than runs", sh("cat " + strings.Join(words, " ")), gate.ReasonFinding},
	})
	if m.runs.Load() == 0 {
		t.Error("cygpath never ran")
	}
}

// TestMSYSPatternsAreNotPaths checks that a word in a program's pattern or
// script slot is not placed, though it starts with a slash: the ordinary
// commands that hold one allow, and a quoted word a reader takes as a file
// is still a file.
func TestMSYSPatternsAreNotPaths(t *testing.T) {
	root := newRepo(t, leakyFiles())
	m := useMounts(t, map[string]string{})
	finding := gate.ReasonFinding
	run(t, root, []decideCase{
		{"sed address", sh(`sed '/^#.*/d' README.md`), ""},
		{"sed address with a backslash", sh(`sed '/\.md$/d' README.md`), ""},
		{"sed -n", sh(`sed -n '/a.*b/p' README.md`), ""},
		{"sed -e", sh(`sed -e '/x*/d' -e '/y\z/d' README.md`), ""},
		{"sed --expression=", sh(`sed --expression='/x*/d' README.md`), ""},
		{"sed -i", sh(`sed -i '/x*/d' notes/clean.md`), ""},
		{"awk", sh(`awk '/a.*b/' README.md`), ""},
		{"awk with -F and -v", sh(`awk -F/ -v re='/a*/' '/a*/{print}' README.md`), ""},
		{"grep", sh(`grep '/api/v[0-9]*' README.md`), ""},
		{"grep after --", sh(`grep -- '/[a-z]+/' README.md`), ""},
		{"grep -e", sh(`grep -e '/x*' README.md`), ""},
		{"grep -rn", sh(`grep -rn '/health' notes`), ""},
		{"grep --regexp=", sh(`grep --regexp='/x*' README.md`), ""},
		{"rg", sh(`rg '/x*' README.md`), ""},
		{"rg -e", sh(`rg -e '/x\y' README.md`), ""},
		{"jq", sh(`jq '/x*' notes/clean.md`), ""},
		{"find -name", sh(`find notes -name '/x*'`), ""},
		{"find -path", sh(`find notes -path '/api/*' -o -iname '/y*' -print`), ""},

		// A file slot stays a file.
		{"cat", sh(`cat '/tm*/x'`), finding},
		{"grep, the file", sh(`grep x '/tm*/x'`), finding},
		{"grep -e, the file", sh(`grep -e '/y*' '/tm*/x'`), finding},
		{"sed, the file", sh(`sed -n p '/tm*/x'`), finding},
		{"sed, the file after a script", sh(`sed '/x/d' '/tm*/x'`), finding},
		{"sed -e, the file", sh(`sed -e '/x/d' '/tm*/x'`), finding},
		{"sed -f, the script file", sh(`sed -f '/tm*/x' README.md`), finding},
		{"awk, the file", sh(`awk '/a/' '/tm*/x'`), finding},
		{"awk -f, the program file", sh(`awk -f '/tm*/x' README.md`), finding},
		{"rg, the file", sh(`rg x '/tm*/x'`), finding},
		{"find, the directory", sh(`find '/tm*/x' -exec cat {} +`), finding},
		{"jq, the file", sh(`jq . '/tm*/x'`), finding},
		{"a backslash in a file", sh(`cat '/tmp/a\b'`), finding},
	})
	if n := m.runs.Load(); n != 0 {
		t.Errorf("cygpath ran %d times", n)
	}
}

// TestMSYSNoCygpath checks what is denied, and why, when cygpath is not
// found: a file word is unknown, but a pattern and a program named in full
// are not, and the deny says what to do.
func TestMSYSNoCygpath(t *testing.T) {
	leaky := newRepo(t, leakyFiles())
	clean := newRepo(t, cleanFiles())
	t.Cleanup(gate.SetMSYS(true, nil))
	finding := gate.ReasonFinding
	cases := []decideCase{
		{"cat a mounted path", sh("cat /etc/hosts"), finding},
		{"cp to a mounted path", sh("cp README.md /tmp/b"), finding},
		{"cd to a mounted path", sh("cd /tmp && cat README.md"), finding},
		{"grep a pattern", sh("grep -rn '/health' notes"), ""},
		{"sed a script", sh("sed '/^#.*/d' README.md"), ""},
		{"a program named in full", sh("/opt/tool README.md"), ""},
		{"a program named in full, run on a file", sh("/opt/tool /etc/hosts"), finding},
		{"a drive path", sh("cat /c/x"), ""},
		{"a shell named in full stays unknown", sh("/usr/bin/bash README.md"), gate.ReasonGitContent},
		{"a wrapper named in full stays unknown", sh("/usr/bin/env python x.py"), gate.ReasonGitContent},
		{"read", gate.Event{Tool: gate.Read, Path: "/etc/hosts"}, gate.ReasonInternalError},
	}
	run(t, leaky, cases)
	// In a clean repository only what is unknown by its kind is denied.
	run(t, clean, []decideCase{
		{"cat a mounted path", sh("cat /etc/hosts"), ""},
		{"a program named in full", sh("/opt/tool README.md"), ""},
		{"a wrapper named in full stays unknown", sh("/usr/bin/env python x.py"), gate.ReasonGitContent},
	})
	for _, tc := range []gate.Event{sh("cat /etc/hosts"), sh("/usr/bin/env python x.py"), {Tool: gate.Read, Path: "/etc/hosts"}} {
		ev := tc
		ev.Cwd = leaky
		d := gate.Decide(ev, gate.Options{})
		if d.Allow || !strings.Contains(d.Message, "CLAUDE_CODE_GIT_BASH_PATH") {
			t.Errorf("the deny does not say cygpath was not found: %q", d.Message)
		}
	}
	// A deny for another reason, with nothing to place, says nothing of it.
	d := gate.Decide(gate.Event{Tool: gate.Shell, Command: "cat " + leakVisual, Cwd: leaky}, gate.Options{})
	if d.Allow || strings.Contains(d.Message, "CLAUDE_CODE_GIT_BASH_PATH") {
		t.Errorf("an unrelated deny blames cygpath: %q", d.Message)
	}
}

// TestMSYSPlacementFailureDoesNotBlame checks that a run that fails with
// cygpath found does not tell the person to look for it.
func TestMSYSPlacementFailureDoesNotBlame(t *testing.T) {
	leaky := newRepo(t, leakyFiles())
	useMounts(t, map[string]string{})
	d := gate.Decide(gate.Event{Tool: gate.Shell, Command: "cat /etc/hosts", Cwd: leaky}, gate.Options{})
	if d.Allow || strings.Contains(d.Message, "CLAUDE_CODE_GIT_BASH_PATH") {
		t.Errorf("allow %v message %q", d.Allow, d.Message)
	}
}
