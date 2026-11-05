package gate_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/gate"
)

// wcMachine is a machine with a registered working copy for Power BI
// Desktop outside every repository, in the home directory, an adopted
// repository, and a directory outside every repository. The working copy
// holds a data cache planted with a value no scan would flag.
type wcMachine struct {
	ctl     controlDirs
	store   *approval.Store
	adopted string
	outside string
	// wc is the working copy; sibling a directory beside it whose name
	// starts with the working copy's.
	wc, sibling string
}

const (
	wcReport = "Example.Report/definition/report.json"
	wcCache  = "Example.SemanticModel/.pbi/cache.abf"
)

func newWCMachine(t *testing.T) wcMachine {
	t.Helper()
	m := wcMachine{ctl: newControlDirs(t)}
	m.store = &approval.Store{Dir: m.ctl.store}
	m.adopted = newRepo(t, leakyFiles())
	approveIn(t, m.store, m.adopted)
	m.outside = t.TempDir()
	write(t, m.outside, "notes.txt", "Nothing personal here.\n")
	m.wc = filepath.Join(m.ctl.home, "work", "copy")
	m.sibling = m.wc + "-other"
	for _, dir := range []string{m.wc, m.sibling} {
		write(t, dir, wcReport, "{}")
		write(t, dir, wcCache, "Doe, Jane\n")
		write(t, dir, ".pbipx-stage.json", "{}")
		write(t, dir, "x.txt", "plain\n")
	}
	if err := m.store.AddWorkingCopy(approval.WorkingCopy{Path: m.wc}); err != nil {
		t.Fatal(err)
	}
	return m
}

// decide decides ev from cwd, which is the directory outside every
// repository when empty.
func (m wcMachine) decide(ev gate.Event, cwd string) gate.Decision {
	if cwd == "" {
		cwd = m.outside
	}
	ev.Cwd = cwd
	return gate.Decide(ev, adoptionOptions(m.store))
}

// wcCase is a call, the directory it is made from ("" for one outside
// every repository) and the reason it is denied with ("" for an allow).
type wcCase struct {
	name, cwd string
	ev        gate.Event
	reason    string
}

func (m wcMachine) run(t *testing.T, cases []wcCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check(t, m.decide(tc.ev, tc.cwd), tc.reason)
		})
	}
}

func wcRead(p string) gate.Event   { return gate.Event{Tool: gate.Read, Path: p} }
func wcWrite(p string) gate.Event  { return gate.Event{Tool: gate.Write, Path: p} }
func wcEdit(p string) gate.Event   { return gate.Event{Tool: gate.Edit, Path: p} }
func wcSearch(p string) gate.Event { return gate.Event{Tool: gate.Search, Path: p} }
func wcLSP(p ...string) gate.Event { return gate.Event{Tool: gate.LSP, Paths: p} }
func wcShell(c string) gate.Event  { return gate.Event{Tool: gate.Shell, Command: c} }
func wcPS(c string) gate.Event     { return gate.Event{Tool: gate.PowerShell, Command: c} }

// TestWorkingCopyEverySink checks that every tool that can reach a path
// under a registered working copy is denied, from outside it and from
// inside it, with no scanning.
func TestWorkingCopyEverySink(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	in := func(rel string) string { return filepath.Join(m.wc, filepath.FromSlash(rel)) }
	bp := func(rel string) string { return bashPath(in(rel)) }
	m.run(t, []wcCase{
		// Read, Edit, Write.
		{"read of the cache", "", wcRead(in(wcCache)), wc},
		{"read of a clean report file", "", wcRead(in(wcReport)), wc},
		{"read of the manifest", "", wcRead(in(".pbipx-stage.json")), wc},
		{"read of the folder itself", "", wcRead(m.wc), wc},
		{"read of a missing file in it", "", wcRead(in("no-such-file")), wc},
		{"edit of a file in it", "", wcEdit(in(wcReport)), wc},
		{"write of a new file in it", "", wcWrite(in("new.json")), wc},
		{"write of a new file in a new folder in it", "", wcWrite(in("a/b/c.json")), wc},
		{"relative read from inside", m.wc, wcRead(wcReport), wc},
		{"relative read of the cache from inside", m.wc, wcRead(wcCache), wc},
		{"relative write from inside", m.wc, wcWrite("new.json"), wc},
		{"read from inside a folder of it", in("Example.Report"), wcRead("definition/report.json"), wc},
		{"read of a clean file outside, from inside", m.wc, wcRead(filepath.Join(m.outside, "notes.txt")), wc},
		{"any other tool, from inside", m.wc, gate.Event{Tool: gate.Other}, wc},

		// Search.
		{"search of the working copy", "", wcSearch(m.wc), wc},
		{"search of a folder in it", "", wcSearch(in("Example.SemanticModel")), wc},
		{"search of a file in it", "", wcSearch(in(wcCache)), wc},
		{"search of the directory holding it", "", wcSearch(filepath.Dir(m.wc)), wc},
		{"search of the home directory", "", wcSearch(m.ctl.home), wc},
		{"search of the home directory by ~", "", wcSearch("~"), wc},
		{"search with no path from inside", m.wc, wcSearch(""), wc},
		{"search with no path from the directory holding it", filepath.Dir(m.wc), wcSearch(""), wc},
		{"search of a sibling", "", wcSearch(m.sibling), ""},
		{"search of the outside directory", "", wcSearch(m.outside), ""},

		// LSP.
		{"lsp on a file in it", "", wcLSP(in(wcReport)), wc},
		{"lsp on the working copy", "", wcLSP(m.wc), wc},
		{"lsp on a directory holding it", "", wcLSP(filepath.Dir(m.wc)), wc},
		{"lsp on one file in it among others", "", wcLSP(filepath.Join(m.outside, "notes.txt"), in(wcReport)), wc},
		{"lsp with no path from inside", m.wc, wcLSP(), wc},
		{"lsp on a file outside", "", wcLSP(filepath.Join(m.outside, "notes.txt")), ""},
		{"lsp with no path from outside", "", wcLSP(), ""},

		// Bash words.
		{"cat of the cache", "", wcShell("cat " + bp(wcCache)), wc},
		{"cat of a clean file", "", wcShell("cat " + bp(wcReport)), wc},
		{"cat of the manifest, quoted", "", wcShell(`cat "` + bp(".pbipx-stage.json") + `"`), wc},
		{"head of the cache", "", wcShell("head -c 10 " + bp(wcCache)), wc},
		{"ls of the working copy", "", wcShell("ls " + bashPath(m.wc)), wc},
		{"ls -la of a folder in it", "", wcShell("ls -la " + bp("Example.SemanticModel")), wc},
		{"stat of a file in it", "", wcShell("stat " + bp(wcCache)), wc},
		{"du of it", "", wcShell("du -sh " + bashPath(m.wc)), wc},
		{"find in it", "", wcShell("find " + bashPath(m.wc) + " -name '*.abf'"), wc},
		{"test -e of a file in it", "", wcShell("test -e " + bp(wcCache)), wc},
		{"cp out of it", "", wcShell("cp " + bp(wcCache) + " " + m.outside), wc},
		{"cp into it", "", wcShell("cp " + filepath.Join(m.outside, "notes.txt") + " " + bashPath(m.wc)), wc},
		{"mv out of it", "", wcShell("mv " + bp(wcReport) + " " + m.outside), wc},
		{"rm in it", "", wcShell("rm " + bp(wcReport)), wc},
		{"touch in it", "", wcShell("touch " + bp("new.json")), wc},
		{"mkdir in it", "", wcShell("mkdir -p " + bp("sub/dir")), wc},
		{"git -C it", "", wcShell("git -C " + bashPath(m.wc) + " status"), wc},
		{"a pipe from it", "", wcShell("cat " + bp(wcCache) + " | wc -c"), wc},
		{"a command substitution reading it", "", wcShell("echo $(cat " + bp(wcCache) + ")"), wc},
		{"a second command", "", wcShell("echo hi; cat " + bp(wcCache)), wc},
		{"a subshell", "", wcShell("(cat " + bp(wcCache) + ")"), wc},
		{"a here-document run by bash", "", wcShell("bash <<EOF\ncat " + bp(wcCache) + "\nEOF"), wc},
		{"python naming it", "", wcShell(`python3 -c "print(open('` + bp(wcCache) + `').read())"`), wc},
		{"sh -c", "", wcShell(`sh -c 'cat ` + bp(wcCache) + `'`), wc},
		{"xargs after find of it", "", wcShell("find " + bashPath(m.wc) + " -type f | xargs cat"), wc},
		{"grep -r of it", "", wcShell("grep -r Doe " + bashPath(m.wc)), wc},
		{"grep -r of the directory holding it", "", wcShell("grep -r Doe " + bashPath(filepath.Dir(m.wc))), wc},
		{"rg of the home directory", "", wcShell("rg Doe " + bashPath(m.ctl.home)), wc},
		{"grep -r in the directory holding it", filepath.Dir(m.wc), wcShell("grep -r Doe ."), wc},
		{"rg with no path in the directory holding it", filepath.Dir(m.wc), wcShell("rg Doe"), wc},
		{"cp -r of the directory holding it", "", wcShell("cp -r " + bashPath(filepath.Dir(m.wc)) + " " + m.outside), wc},
		{"tar of the directory holding it", "", wcShell("tar cf x.tar " + bashPath(filepath.Dir(m.wc))), wc},

		// Redirections.
		{"redirect into it", "", wcShell("echo hi > " + bp("new.json")), wc},
		{"append into it", "", wcShell("echo hi >> " + bp(wcReport)), wc},
		{"redirect out of it", "", wcShell("cat < " + bp(wcCache)), wc},
		{"redirect of stderr into it", "", wcShell("ls /nonexistent 2> " + bp("err.txt")), wc},
		{"redirect after a clean command", "", wcShell("echo hi > " + bp("x.txt")), wc},
		{"tee into it", "", wcShell("echo hi | tee " + bp("x.txt")), wc},
		{"dd of it", "", wcShell("dd if=" + bp(wcCache) + " of=/dev/null"), wc},

		// A working directory inside it.
		{"cwd inside, ls", m.wc, wcShell("ls"), wc},
		{"cwd inside, pwd", m.wc, wcShell("pwd"), wc},
		{"cwd inside, echo", m.wc, wcShell("echo hi"), wc},
		{"cwd in a folder of it, cat", in("Example.Report"), wcShell("cat definition/report.json"), wc},
		{"cwd inside, cat of a file outside", m.wc, wcShell("cat " + filepath.Join(m.outside, "notes.txt")), wc},
		{"cd into it, then cat", "", wcShell("cd " + bashPath(m.wc) + " && cat " + wcCache), wc},
		{"cd into it, then ls", "", wcShell("cd " + bashPath(m.wc) + " && ls"), wc},
		{"cd into it alone", "", wcShell("cd " + bashPath(m.wc)), wc},
		{"cd into a folder of it", "", wcShell("cd " + bp("Example.Report") + " && pwd"), wc},
		{"pushd into it", "", wcShell("pushd " + bashPath(m.wc) + " && ls"), wc},
		{"a subshell cd", "", wcShell("(cd " + bashPath(m.wc) + " && ls)"), wc},
		{"cd .. into it", filepath.Dir(m.wc), wcShell("cd copy && ls"), wc},
		{"cd relative from the directory holding it", filepath.Dir(m.wc), wcShell("cd ./copy && cat " + wcReport), wc},

		// Words that cannot be placed.
		{"cat of a variable", "", wcShell(`cat "$X"`), wc},
		{"cat of a command substitution", "", wcShell("cat $(echo x)"), wc},
		{"cat of a path built at run time", "", wcShell(`cat "$HOME/$(echo work)/copy/x.txt"`), wc},
		{"cd to a variable", "", wcShell(`cd "$D" && ls`), wc},
		{"eval", "", wcShell(`eval "cat $X"`), wc},

		// PowerShell.
		{"Get-Content of the cache", "", wcPS("Get-Content '" + in(wcCache) + "'"), wc},
		{"Get-Content with a forward slashes path", "", wcPS("Get-Content " + bp(wcReport)), wc},
		{"Get-ChildItem of it", "", wcPS("Get-ChildItem " + m.wc), wc},
		{"Get-ChildItem -Recurse of the directory holding it", "", wcPS("Get-ChildItem -Recurse " + filepath.Dir(m.wc)), wc},
		{"Set-Content into it", "", wcPS("Set-Content -Path " + in("new.json") + " -Value x"), wc},
		{"a path built from a variable", "", wcPS(`Get-Content "$base/x.txt"`), wc},
		{"Set-Location into it", "", wcPS("Set-Location " + m.wc + "; Get-ChildItem"), wc},
		{"cwd inside", m.wc, wcPS("Get-Date"), wc},
		{"Get-Content of a file outside", "", wcPS("Get-Content " + filepath.Join(m.outside, "notes.txt")), ""},
		{"Get-Date from outside", "", wcPS("Get-Date"), ""},
		{"Get-ChildItem of a sibling", "", wcPS("Get-ChildItem " + m.sibling), ""},
		{"Get-Date from the directory holding it", filepath.Dir(m.wc), wcPS("Get-Date"), ""},

		// Not under it.
		{"read of a file outside", "", wcRead(filepath.Join(m.outside, "notes.txt")), ""},
		{"read of a file in a sibling whose name starts with its", "", wcRead(filepath.Join(m.sibling, "x.txt")), ""},
		{"cat of a file in the sibling", "", wcShell("cat " + bashPath(filepath.Join(m.sibling, "x.txt"))), ""},
		{"cat of a file outside", "", wcShell("cat " + filepath.Join(m.outside, "notes.txt")), ""},
		{"ls outside", "", wcShell("ls -la"), ""},
		{"echo from outside", "", wcShell("echo hi"), ""},
		{"echo $X from outside", "", wcShell("echo $X"), ""},
		{"write outside", "", wcWrite(filepath.Join(m.outside, "out.txt")), ""},
		{"redirect outside", "", wcShell("echo hi > out.txt"), ""},
		{"cd to the directory holding it", "", wcShell("cd " + bashPath(filepath.Dir(m.wc)) + " && pwd"), ""},
		{"cd to the sibling", "", wcShell("cd " + bashPath(m.sibling) + " && ls"), ""},
		{"git status in an adopted repository", m.adopted, wcShell("git status"), ""},
		{"read of a clean note in an adopted repository", m.adopted, wcRead(cleanNote), ""},
		{"commit message that names no path", m.adopted, wcShell(`git commit -m "message"`), ""},
		{"ls of the directory holding it", "", wcShell("ls " + bashPath(filepath.Dir(m.wc))), ""},
		{"ls -R of the directory holding it", "", wcShell("ls -R " + bashPath(filepath.Dir(m.wc))), ""},
		{"cd .. out of the home, no read", "", wcShell("cd " + bashPath(m.ctl.home) + " && pwd"), ""},
		{"a glob that matches only the sibling", "", wcShell("cat " + bashPath(m.wc) + "-o*/x.txt"), ""},
	})
}

// TestWorkingCopySpellings checks that a path under a working copy is
// found however it is written: through a symbolic link, with .., by a
// glob, from the home directory, and in another case on a file system
// that folds case.
func TestWorkingCopySpellings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges")
	}
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	in := func(rel string) string { return filepath.Join(m.wc, filepath.FromSlash(rel)) }
	link := filepath.Join(m.outside, "link")
	if err := os.Symlink(m.wc, link); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(m.outside, "cache-link")
	if err := os.Symlink(in(wcCache), fileLink); err != nil {
		t.Fatal(err)
	}
	// A link in the directory above, to the directory above the working copy.
	upLink := filepath.Join(m.outside, "up")
	if err := os.Symlink(filepath.Dir(m.wc), upLink); err != nil {
		t.Fatal(err)
	}
	// A link into the sibling stays allowed.
	sibLink := filepath.Join(m.outside, "sib")
	if err := os.Symlink(m.sibling, sibLink); err != nil {
		t.Fatal(err)
	}
	m.run(t, []wcCase{
		{"read through a link to the folder", "", wcRead(filepath.Join(link, wcCache)), wc},
		{"read of a link to a file in it", "", wcRead(fileLink), wc},
		{"write through a link to the folder", "", wcWrite(filepath.Join(link, "new.json")), wc},
		{"search through a link", "", wcSearch(link), wc},
		{"search through a link to the directory holding it", "", wcSearch(upLink), wc},
		{"read through a link to the directory holding it", "", wcRead(filepath.Join(upLink, "copy", wcReport)), wc},
		{"cat through a link", "", wcShell("cat " + bashPath(filepath.Join(link, wcCache))), wc},
		{"cat of a link to a file in it", "", wcShell("cat " + bashPath(fileLink)), wc},
		{"cd through a link", "", wcShell("cd " + bashPath(link) + " && ls"), wc},
		{"redirect through a link", "", wcShell("echo hi > " + bashPath(filepath.Join(link, "new.json"))), wc},
		{"cwd through a link", link, wcShell("ls"), wc},
		{"PowerShell through a link", "", wcPS("Get-Content " + filepath.Join(link, wcCache)), wc},
		{"read through a link to the sibling", "", wcRead(filepath.Join(sibLink, "x.txt")), ""},
		{"cat through a link to the sibling", "", wcShell("cat " + bashPath(filepath.Join(sibLink, "x.txt"))), ""},

		// ..
		{"read with ..", "", wcRead(filepath.Join(m.sibling, "..", "copy", wcReport)), wc},
		{"read with .. back out and in", "", wcRead(filepath.Join(in("Example.Report"), "..", "..", "copy", "x.txt")), wc},
		{"relative read with ..", filepath.Join(m.sibling, "Example.Report"), wcRead("../../copy/x.txt"), wc},
		{"cat with ..", "", wcShell("cat " + bashPath(m.sibling) + "/../copy/x.txt"), wc},
		{"cd with ..", filepath.Join(m.sibling, "Example.Report"), wcShell("cd ../../copy && ls"), wc},
		{"cat with a ..-spelled sibling", "", wcShell("cat " + bashPath(m.sibling) + "/../copy-other/x.txt"), ""},
		{"read of the sibling with ..", "", wcRead(filepath.Join(m.wc, "..", "copy-other", "x.txt")), ""},
		{"a dot segment", "", wcShell("cat " + bashPath(m.wc) + "/./x.txt"), wc},
		{"doubled separators", "", wcShell("cat " + bashPath(m.wc) + "//x.txt"), wc},
		{"a trailing separator", "", wcRead(m.wc + string(filepath.Separator)), wc},

		// Globs.
		{"glob over the last segment", "", wcShell("cat " + bashPath(filepath.Dir(m.wc)) + "/cop?/x.txt"), wc},
		{"star glob over the last segment", "", wcShell("cat " + bashPath(filepath.Dir(m.wc)) + "/co*/x.txt"), wc},
		{"bracket glob", "", wcShell("cat " + bashPath(filepath.Dir(m.wc)) + "/cop[y]/x.txt"), wc},
		{"glob inside it", "", wcShell("cat " + bashPath(m.wc) + "/*/*/*.json"), wc},
		{"glob of the cache by extension", "", wcShell("cat " + bashPath(m.wc) + "/*/.pbi/*.abf"), wc},
		{"glob over the home directory", "", wcShell("cat " + bashPath(m.ctl.home) + "/w*/c*/x.txt"), wc},
		{"glob from the directory holding it", filepath.Dir(m.wc), wcShell("cat co*/x.txt"), wc},
		{"brace expansion", "", wcShell("cat " + bashPath(filepath.Dir(m.wc)) + "/{copy,nothing}/x.txt"), wc},
		{"glob that matches both", "", wcShell("cat " + bashPath(filepath.Dir(m.wc)) + "/copy*/x.txt"), wc},

		// ~ and $HOME.
		{"read by ~", "", wcRead("~/work/copy/" + wcReport), wc},
		{"cat by ~", "", wcShell("cat ~/work/copy/" + wcCache), wc},
		{"cat by $HOME", "", wcShell("cat $HOME/work/copy/" + wcCache), wc},
		{"cat by ${HOME}", "", wcShell("cat ${HOME}/work/copy/" + wcCache), wc},
		{"cat by quoted $HOME", "", wcShell(`cat "$HOME/work/copy/` + wcCache + `"`), wc},
		{"cd by ~", "", wcShell("cd ~/work/copy && ls"), wc},
		{"redirect by ~", "", wcShell("echo hi > ~/work/copy/new.json"), wc},
		{"ls by $HOME", "", wcShell("ls $HOME/work/copy"), wc},
		{"cat by ~ of the sibling", "", wcShell("cat ~/work/copy-other/x.txt"), ""},
		{"read by ~ of the sibling", "", wcRead("~/work/copy-other/x.txt"), ""},
		{"PowerShell by ~", "", wcPS("Get-Content ~/work/copy/" + wcCache), wc},
		{"grep -r of ~", "", wcShell("grep -r Doe ~"), wc},
		{"grep -r of ~/work", "", wcShell("grep -r Doe ~/work"), wc},
	})
}

// TestWorkingCopyFoldsCase checks that on a file system that ignores case,
// a path in another case is a path under the working copy. Where it does
// not, the other case is another path.
func TestWorkingCopyFoldsCase(t *testing.T) {
	m := newWCMachine(t)
	upper := strings.ToUpper(m.wc)
	lower := strings.ToLower(m.wc)
	wc := gate.ReasonWorkingCopy
	for _, fold := range []bool{true, false} {
		name := "case-sensitive"
		reason := ""
		if fold {
			name, reason = "case-folding", wc
		}
		t.Run(name, func(t *testing.T) {
			defer gate.SetFoldCase(fold)()
			m.run(t, []wcCase{
				{"read in upper case", "", wcRead(filepath.Join(upper, wcReport)), reason},
				{"read in lower case", "", wcRead(filepath.Join(lower, strings.ToLower(wcReport))), reason},
				{"cat in upper case", "", wcShell("cat " + bashPath(upper) + "/x.txt"), reason},
				{"cd in upper case", "", wcShell("cd " + bashPath(upper) + " && ls"), reason},
				{"search above in upper case", "", wcSearch(strings.ToUpper(filepath.Dir(m.wc))), reason},
				{"write in upper case", "", wcWrite(filepath.Join(upper, "new.json")), reason},
				{"the exact case is denied either way", "", wcRead(filepath.Join(m.wc, wcReport)), wc},
				{"a sibling in upper case is not it", "", wcRead(filepath.Join(strings.ToUpper(m.sibling), "x.txt")), ""},
			})
		})
	}
}

// TestWorkingCopyFromAnywhere checks that the deny does not depend on the
// working directory: adopted or not, inside a repository or not.
func TestWorkingCopyFromAnywhere(t *testing.T) {
	m := newWCMachine(t)
	plain := notAdoptedRepo(t, leakyFiles())
	wc := gate.ReasonWorkingCopy
	cache := filepath.Join(m.wc, filepath.FromSlash(wcCache))
	for name, cwd := range map[string]string{
		"an adopted repository": m.adopted, "a repository that did not adopt": plain, "a directory outside every repository": m.outside,
		"the home directory": m.ctl.home, "the working copy": m.wc,
	} {
		t.Run(name, func(t *testing.T) {
			m.run(t, []wcCase{
				{"read", cwd, wcRead(cache), wc},
				{"cat", cwd, wcShell("cat " + bashPath(cache)), wc},
				{"write", cwd, wcWrite(cache), wc},
				{"search", cwd, wcSearch(m.wc), wc},
				{"powershell", cwd, wcPS("Get-Content " + cache), wc},
			})
		})
	}
}

// TestWorkingCopyBeforeScanning checks the working copy is denied as such,
// not judged by a policy: a nested repository inside it, adopted or not,
// with a clean file or a leak, is still a working copy.
func TestWorkingCopyBeforeScanning(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	nested := filepath.Join(m.wc, "nested")
	write(t, nested, ".pbipx.toml", defaultPolicy(t))
	for name, content := range leakyFiles() {
		write(t, nested, name, content)
	}
	if err := corpus.GitSetup(nested, nil, nil); err != nil {
		t.Fatal(err)
	}
	approveIn(t, m.store, nested)
	plainNested := filepath.Join(m.wc, "plain-nested")
	for name, content := range cleanFiles() {
		write(t, plainNested, name, content)
	}
	if err := corpus.GitSetup(plainNested, nil, nil); err != nil {
		t.Fatal(err)
	}
	m.run(t, []wcCase{
		{"read of a clean file in an adopted nested repository", "", wcRead(filepath.Join(nested, cleanNote)), wc},
		{"read of a leak in an adopted nested repository", "", wcRead(filepath.Join(nested, leakVisual)), wc},
		{"read from inside the nested repository", nested, wcRead(cleanNote), wc},
		{"cat from inside the nested repository", nested, wcShell("cat " + cleanNote), wc},
		{"git status in the nested repository", nested, wcShell("git status"), wc},
		{"git log -p in the nested repository", nested, wcShell("git log -p"), wc},
		{"read in a nested repository that did not adopt", "", wcRead(filepath.Join(plainNested, cleanNote)), wc},
		{"git -C the nested repository", "", wcShell("git -C " + bashPath(nested) + " status"), wc},
		{"git-dir inside it", "", wcShell("git --git-dir=" + bashPath(filepath.Join(nested, ".git")) + " log"), wc},
	})
}

// TestWorkingCopyNeedsNoAdoption checks a machine with no adopted
// repository at all still protects a working copy.
func TestWorkingCopyNeedsNoAdoption(t *testing.T) {
	ctl := newControlDirs(t)
	store := &approval.Store{Dir: ctl.store}
	wcDir := filepath.Join(ctl.home, "copy")
	write(t, wcDir, "x.txt", "plain\n")
	if err := store.AddWorkingCopy(approval.WorkingCopy{Path: wcDir}); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for _, tc := range []struct {
		ev     gate.Event
		reason string
	}{
		{wcRead(filepath.Join(wcDir, "x.txt")), gate.ReasonWorkingCopy},
		{wcShell("cat " + bashPath(filepath.Join(wcDir, "x.txt"))), gate.ReasonWorkingCopy},
		{wcRead(filepath.Join(outside, "y.txt")), ""},
	} {
		tc.ev.Cwd = outside
		check(t, gate.Decide(tc.ev, adoptionOptions(store)), tc.reason)
	}
}

// TestWorkingCopyStaleEntry checks that an entry whose folder is gone is
// skipped, and that the entry protects the folder again if it returns.
func TestWorkingCopyStaleEntry(t *testing.T) {
	m := newWCMachine(t)
	gone := filepath.Join(m.ctl.home, "gone")
	if err := m.store.AddWorkingCopy(approval.WorkingCopy{Path: gone}); err != nil {
		t.Fatal(err)
	}
	m.run(t, []wcCase{
		{"read under a folder that is gone", "", wcRead(filepath.Join(gone, "x.txt")), ""},
		{"cat under a folder that is gone", "", wcShell("cat " + bashPath(filepath.Join(gone, "x.txt"))), ""},
		{"a word that cannot be placed is judged by the entry that is there", "", wcShell(`cat "$X"`), gate.ReasonWorkingCopy},
		{"the live one is still denied", "", wcRead(filepath.Join(m.wc, wcReport)), gate.ReasonWorkingCopy},
	})
	if err := os.RemoveAll(m.wc); err != nil {
		t.Fatal(err)
	}
	m.run(t, []wcCase{
		{"every entry is stale: a read of a path that was under one", "", wcRead(filepath.Join(m.wc, wcReport)), ""},
		// The adopted repository's own rules apply again, not the working copy's.
		{"every entry is stale: a word that cannot be placed", "", wcShell(`cat "$X"`), gate.ReasonFinding},
		{"every entry is stale: a search above", "", wcSearch(m.ctl.home), ""},
	})
	write(t, m.wc, "x.txt", "back\n")
	m.run(t, []wcCase{
		{"the folder returns", "", wcRead(filepath.Join(m.wc, "x.txt")), gate.ReasonWorkingCopy},
	})
}

// TestWorkingCopyBrokenRegistry checks that a registry the gate cannot
// read denies every call, as a broken store does.
func TestWorkingCopyBrokenRegistry(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, m wcMachine){
		"an entry that is not json": func(t *testing.T, m wcMachine) {
			write(t, filepath.Join(m.ctl.store, "stages"), strings.Repeat("c", 64)+".json", "not json")
		},
		"an entry with an unknown key": func(t *testing.T, m wcMachine) {
			write(t, filepath.Join(m.ctl.store, "stages"), strings.Repeat("c", 64)+".json", `{"version":1,"path":"/x","note":"y"}`)
		},
		"an entry with a relative path": func(t *testing.T, m wcMachine) {
			write(t, filepath.Join(m.ctl.store, "stages"), strings.Repeat("c", 64)+".json", `{"version":1,"path":"copy"}`)
		},
		"an empty entry": func(t *testing.T, m wcMachine) {
			write(t, filepath.Join(m.ctl.store, "stages"), strings.Repeat("c", 64)+".json", "")
		},
		"an entry that is a directory": func(t *testing.T, m wcMachine) {
			if err := os.Mkdir(filepath.Join(m.ctl.store, "stages", strings.Repeat("c", 64)+".json"), 0o750); err != nil {
				t.Fatal(err)
			}
		},
		"a registry that is a file": func(t *testing.T, m wcMachine) {
			if err := os.RemoveAll(filepath.Join(m.ctl.store, "stages")); err != nil {
				t.Fatal(err)
			}
			write(t, m.ctl.store, "stages", "x")
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newWCMachine(t)
			damage(t, m)
			internal := gate.ReasonInternalError
			m.run(t, []wcCase{
				{"read outside every repository", "", wcRead(filepath.Join(m.outside, "notes.txt")), internal},
				{"read in an adopted repository", m.adopted, wcRead(cleanNote), internal},
				{"a shell command that touches nothing", "", wcShell("echo hi"), internal},
				{"any other tool", "", gate.Event{Tool: gate.Other}, internal},
				{"the working copy itself", "", wcRead(filepath.Join(m.wc, wcReport)), internal},
			})
		})
	}
}

// TestWorkingCopyNoRegistry checks that a store with no registry at all,
// the case before any stage, changes nothing.
func TestWorkingCopyNoRegistry(t *testing.T) {
	a := newAdoption(t)
	check(t, a.decide(gate.Event{Tool: gate.Read, Path: filepath.Join(a.outside, "notes.txt"), Cwd: a.outside}), "")
	check(t, a.decide(gate.Event{Tool: gate.Shell, Command: `cat "$X"`, Cwd: a.plain}), gate.ReasonFinding)
}

// TestWorkingCopyRegistryIsGuarded checks that an agent cannot add, change
// or remove an entry: the registry is in the store, and a write anywhere
// in the store is denied from every directory.
func TestWorkingCopyRegistryIsGuarded(t *testing.T) {
	m := newWCMachine(t)
	entries, err := os.ReadDir(filepath.Join(m.ctl.store, "stages"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the registry holds %d entries, %v", len(entries), err)
	}
	entry := filepath.Join(m.ctl.store, "stages", entries[0].Name())
	fresh := filepath.Join(m.ctl.store, "stages", strings.Repeat("d", 64)+".json")
	as := gate.ReasonAgentSettings
	m.run(t, []wcCase{
		{"write over the entry", "", wcWrite(entry), as},
		{"edit the entry", "", wcEdit(entry), as},
		{"write a new entry", "", wcWrite(fresh), as},
		{"write a new entry from an adopted repository", m.adopted, wcWrite(fresh), as},
		{"write into the registry directory", "", wcWrite(filepath.Join(m.ctl.store, "stages", "x")), as},
		{"redirect into a new entry", "", wcShell("echo '{}' > " + bashPath(fresh)), as},
		{"redirect over the entry", "", wcShell("echo '{}' > " + bashPath(entry)), as},
		{"rm the entry", "", wcShell("rm " + bashPath(entry)), as},
		{"rm the registry", "", wcShell("rm -rf " + bashPath(filepath.Join(m.ctl.store, "stages"))), as},
		{"mv the registry away", "", wcShell("mv " + bashPath(filepath.Join(m.ctl.store, "stages")) + " " + m.outside), as},
		{"cp a file over the entry", "", wcShell("cp " + filepath.Join(m.outside, "notes.txt") + " " + bashPath(entry)), as},
		{"tee into the entry", "", wcShell("echo '{}' | tee " + bashPath(entry)), as},
		{"PowerShell Set-Content", "", wcPS("Set-Content -Path " + entry + " -Value '{}'"), as},
		{"PowerShell Remove-Item", "", wcPS("Remove-Item " + entry), as},
	})
	if _, err := os.Stat(entry); err != nil {
		t.Errorf("the entry is gone: %v", err)
	}
}

// TestWorkingCopyMessages checks the deny's words: a fixed message and a
// hint, a path as a path is displayed, and never file content.
func TestWorkingCopyMessages(t *testing.T) {
	m := newWCMachine(t)
	const hint = "Leave it out of the call: a person works there in Power BI Desktop."
	for name, tc := range map[string]struct {
		ev   gate.Event
		cwd  string
		want []string
	}{
		"a read":                       {wcRead(filepath.Join(m.wc, wcCache)), "", []string{"<working copy>/" + wcCache + " is in a Power BI Desktop working copy"}},
		"a write":                      {wcWrite(filepath.Join(m.wc, wcReport)), "", []string{"<working copy>/" + wcReport + " is in a Power BI Desktop working copy"}},
		"the folder itself":            {wcRead(m.wc), "", []string{"<working copy> is a Power BI Desktop working copy"}},
		"a cat":                        {wcShell("cat " + bashPath(filepath.Join(m.wc, wcCache))), "", []string{"<working copy>/" + wcCache + " is in a Power BI Desktop working copy"}},
		"a search above":               {wcSearch(filepath.Dir(m.wc)), "", []string{"could reach a Power BI Desktop working copy"}},
		"a word that cannot be placed": {wcShell(`cat "$X"`), "", []string{"could reach a Power BI Desktop working copy"}},
		"a working directory in it":    {wcShell("ls"), m.wc, []string{"<working copy> is a Power BI Desktop working copy"}},
	} {
		t.Run(name, func(t *testing.T) {
			d := m.decide(tc.ev, tc.cwd)
			check(t, d, gate.ReasonWorkingCopy)
			for _, w := range tc.want {
				if !strings.Contains(d.Message, w) {
					t.Errorf("the message lacks %q: %s", w, d.Message)
				}
			}
			for _, tell := range []string{"copy of real data", "cannot be vetted", hint} {
				if !strings.Contains(d.Message, tell) {
					t.Errorf("the message lacks %q: %s", tell, d.Message)
				}
			}
			if !strings.HasSuffix(d.Message, hint) {
				t.Errorf("the message does not end with the hint: %s", d.Message)
			}
			for _, leak := range []string{m.wc, m.ctl.home, "Doe, Jane"} {
				if strings.Contains(d.Message, leak) {
					t.Errorf("the message holds %q", leak)
				}
			}
		})
	}
}

// TestWorkingCopyPathIsDisplayed checks that a name in the path with the
// shape of personal data prints as the display rule prints it.
func TestWorkingCopyPathIsDisplayed(t *testing.T) {
	m := newWCMachine(t)
	d := m.decide(wcRead(filepath.Join(m.wc, "jane.doe@example.com", "x.txt")), "")
	check(t, d, gate.ReasonWorkingCopy)
	if strings.Contains(d.Message, "jane.doe@example.com") {
		t.Errorf("the message prints a name with a personal-data shape: %s", d.Message)
	}
	if !strings.Contains(d.Message, "{?}") {
		t.Errorf("the message does not show the placeholder: %s", d.Message)
	}
}

// TestWorkingCopyNoScanRead checks the working copy's files are never
// opened by the gate: a file it cannot read still denies as a working copy.
func TestWorkingCopyNoScanRead(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a file the test cannot read")
	}
	m := newWCMachine(t)
	cache := filepath.Join(m.wc, filepath.FromSlash(wcCache))
	if err := os.Chmod(cache, 0); err != nil {
		t.Fatal(err)
	}
	check(t, m.decide(wcRead(cache), ""), gate.ReasonWorkingCopy)
	check(t, m.decide(wcShell("cat "+bashPath(cache)), ""), gate.ReasonWorkingCopy)
}

// TestWorkingCopyReasonIsListed checks the new class is in the closed list.
func TestWorkingCopyReasonIsListed(t *testing.T) {
	found := false
	for _, r := range gate.Reasons {
		found = found || r == "working-copy"
	}
	if !found || gate.ReasonWorkingCopy != "working-copy" {
		t.Errorf("working-copy is not a reason class: %v", gate.Reasons)
	}
}
