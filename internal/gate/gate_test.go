package gate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/gate"
)

// slicer is a report visual whose slicer keeps value selected on
// entity.column.
func slicer(entity, column, value string) string {
	return fmt.Sprintf(`{"name":"0001","visual":{"visualType":"slicer","objects":{"general":[{"properties":{"filter":{"filter":{
"Version":2,"From":[{"Name":"s","Entity":%q,"Type":0}],
"Where":[{"Condition":{"In":{"Expressions":[{"Column":{"Expression":{"SourceRef":{"Source":"s"}},"Property":%q}}],
"Values":[[{"Literal":{"Value":%q}}]]}}}]}}}}]}}}`, entity, column, "'"+value+"'")
}

const (
	reportJSON   = "pbip/A.Report/definition/report.json"
	warnVisual   = "pbip/A.Report/definition/pages/0000/visuals/0001/visual.json"
	leakVisual   = "pbip/B.Report/definition/pages/0000/visuals/0001/visual.json"
	localState   = "pbip/A.Report/.pbi/localSettings.json"
	logo         = "pbip/A.Report/StaticResources/RegisteredResources/logo.png"
	leakNote     = "notes/leak.md"
	cleanNote    = "notes/clean.md"
	cleanSQL     = "sql/clean.sql"
	unparseable  = "pbip/C.Report/definition/report.json"
	unvettable   = "pbip/C.pbix"
	baselineFile = ".pbipx-baseline.toml"
)

// cleanFiles is a repository whose protected tree is clean: a warning and
// local state (which tree scans skip) hold values, and a note outside the
// roots leaks.
func cleanFiles() map[string]string {
	return map[string]string{
		"README.md": "An example repository.\n",
		reportJSON:  `{"layoutOptimization": "None"}`,
		warnVisual:  slicer("enrollment", "cohort_year", "2150"),
		localState:  `{"lastFilter": "Doe, Jane", "email": "jane.doe@example.com"}`,
		logo:        "not really a png",
		leakNote:    "Attendees:\n\nDoe, Jane\n",
		cleanNote:   "Nothing personal here.\n",
		cleanSQL:    "select 1;\n",
	}
}

// leakyFiles adds a block finding under the protected root.
func leakyFiles() map[string]string {
	files := cleanFiles()
	files[leakVisual] = slicer("student", "full_name", "Doe, Jane")
	return files
}

type decideCase struct {
	name   string
	ev     gate.Event // Cwd relative to the repository root; "" is the root
	reason string     // "" for an allow
}

// run decides each case in root and checks the decision, and that no
// sentinel appears in a message.
func run(t *testing.T, root string, cases []decideCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := tc.ev
			ev.Cwd = filepath.Join(root, filepath.FromSlash(ev.Cwd))
			d := gate.Decide(ev, gate.Options{})
			check(t, d, tc.reason)
		})
	}
}

func check(t *testing.T, d gate.Decision, reason string) {
	t.Helper()
	switch {
	case reason == "" && !d.Allow:
		t.Errorf("denied %s: %s; want an allow", d.Reason, d.Message)
	case reason == "" && (d.Reason != "" || d.Message != ""):
		t.Errorf("an allow carries a reason or message")
	case reason != "" && d.Allow:
		t.Errorf("allowed; want deny %s", reason)
	case reason != "" && d.Reason != reason:
		t.Errorf("denied %s: %s; want %s", d.Reason, d.Message, reason)
	case reason != "" && d.Message == "":
		t.Errorf("a deny has no message")
	}
	for i, s := range sentinels {
		if strings.Contains(d.Message, s) {
			t.Errorf("sentinels[%d] appears in the message", i)
		}
	}
}

func TestRead(t *testing.T) {
	root := newRepo(t, leakyFiles())
	outside := t.TempDir()
	write(t, outside, "file.md", "Doe, Jane\n")
	read := func(p string) gate.Event { return gate.Event{Tool: gate.Read, Path: p} }
	in := func(dir, p string) gate.Event { return gate.Event{Tool: gate.Read, Path: p, Cwd: dir} }
	run(t, root, []decideCase{
		{"empty path", read(""), ""},
		{"outside the repository", read(filepath.Join(outside, "file.md")), ""},
		{"directory under a root", read("pbip/B.Report/definition"), ""},
		{"local-state directory", read("pbip/A.Report/.pbi"), ""},
		{"outside roots and scope", read("README.md"), ""},
		{"in scope outside roots, clean", read(cleanNote), ""},
		{"in scope outside roots, leaking", read(leakNote), gate.ReasonFinding},
		{"allowlisted image", read(logo), ""},
		{"local state", read(localState), gate.ReasonLocalState},
		{"missing local state", read("pbip/Z.Report/.pbi/editorSettings.json"), gate.ReasonLocalState},
		{"missing unvettable", read("pbip/Z.Report/.pbi/cache.abf"), gate.ReasonUnvettable},
		{"bim", read("pbip/Z.SemanticModel/model.bim"), gate.ReasonUnvettable},
		{"missing scanned file", read("pbip/Z.Report/definition/report.json"), ""},
		{"clean report beside a leak", read(reportJSON), ""},
		{"warning only", read(warnVisual), ""},
		{"block", read(leakVisual), gate.ReasonFinding},
		{"absolute block", read(filepath.Join(root, filepath.FromSlash(leakVisual))), gate.ReasonFinding},
		{"relative to a subdirectory", in("pbip/B.Report", "definition/pages/0000/visuals/0001/visual.json"), gate.ReasonFinding},
		{"relative climbing out of a root", in("pbip/A.Report", "../../notes/leak.md"), gate.ReasonFinding},
		{"relative clean", in("pbip/A.Report", "definition/report.json"), ""},
	})
}

func TestReadSymlinks(t *testing.T) {
	root := newRepo(t, cleanFiles())
	if err := os.Symlink(filepath.Join(root, filepath.FromSlash(localState)), filepath.Join(root, "notes", "link.md")); err != nil {
		t.Skip("symlinks are not available")
	}
	outside := t.TempDir()
	write(t, outside, "leak.json", slicer("student", "full_name", "Doe, Jane"))
	if err := os.Symlink(filepath.Join(outside, "leak.json"), filepath.Join(root, "pbip", "A.Report", "definition", "linked.json")); err != nil {
		t.Fatal(err)
	}
	read := func(p string) gate.Event { return gate.Event{Tool: gate.Read, Path: p} }
	run(t, root, []decideCase{
		// The link is a note, but what it reads is local state.
		{"link to local state", read("notes/link.md"), gate.ReasonLocalState},
		// The link is under a root; what it reads is judged as its kind.
		{"link under a root to a leak outside", read("pbip/A.Report/definition/linked.json"), gate.ReasonFinding},
	})
}

func TestReadUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	root := newRepo(t, cleanFiles())
	if err := os.Chmod(filepath.Join(root, filepath.FromSlash(reportJSON)), 0); err != nil {
		t.Fatal(err)
	}
	run(t, root, []decideCase{
		{"unreadable scanned file", gate.Event{Tool: gate.Read, Path: reportJSON}, gate.ReasonScanError},
		{"unreadable file in a tree scan", gate.Event{Tool: gate.Search, Path: "pbip"}, gate.ReasonScanError},
	})
}

func TestSearch(t *testing.T) {
	clean := newRepo(t, cleanFiles())
	leaky := newRepo(t, leakyFiles())
	search := func(p string) gate.Event { return gate.Event{Tool: gate.Search, Path: p} }
	for _, tc := range []struct {
		name         string
		ev           gate.Event
		clean, leaky string
	}{
		{"default path at the repository root", search(""), "", gate.ReasonFinding},
		{"the repository root", search("."), "", gate.ReasonFinding},
		{"the root", search("pbip"), "", gate.ReasonFinding},
		{"inside the root, leak elsewhere", search("pbip/A.Report"), "", gate.ReasonFinding},
		{"a directory away from the roots", search("sql"), "", ""},
		{"notes outside the roots", search("notes"), "", ""},
		{"above the repository", search(".."), "", gate.ReasonFinding},
		{"default path inside a root", gate.Event{Tool: gate.Search, Cwd: "pbip/A.Report"}, "", gate.ReasonFinding},
		{"default path away from the roots", gate.Event{Tool: gate.Search, Cwd: "sql"}, "", ""},
	} {
		run(t, clean, []decideCase{{tc.name + " (clean)", tc.ev, tc.clean}})
		run(t, leaky, []decideCase{{tc.name + " (leaky)", tc.ev, tc.leaky}})
	}
}

func TestSearchAbsolutePath(t *testing.T) {
	root := newRepo(t, leakyFiles())
	run(t, root, []decideCase{
		{"absolute root", gate.Event{Tool: gate.Search, Path: filepath.Join(root, "pbip")}, gate.ReasonFinding},
		{"absolute elsewhere", gate.Event{Tool: gate.Search, Path: filepath.Join(root, "sql")}, ""},
		{"unrelated directory", gate.Event{Tool: gate.Search, Path: t.TempDir()}, ""},
	})
}

func TestTreeScanReasons(t *testing.T) {
	search := gate.Event{Tool: gate.Search, Path: "pbip"}
	for _, tc := range []struct {
		name   string
		add    map[string]string
		reason string
	}{
		{"unvettable", map[string]string{unvettable: "binary"}, gate.ReasonUnvettable},
		{"unparseable", map[string]string{unparseable: "{"}, gate.ReasonScanError},
		{"block and error: finding", map[string]string{unparseable: "{", leakVisual: slicer("student", "full_name", "Doe, Jane")}, gate.ReasonFinding},
		{"block and unvettable: finding", map[string]string{unvettable: "binary", leakVisual: slicer("student", "email", "jane.doe@example.com")}, gate.ReasonFinding},
	} {
		files := cleanFiles()
		for k, v := range tc.add {
			files[k] = v
		}
		root := newRepo(t, files)
		run(t, root, []decideCase{{tc.name, search, tc.reason}})
	}
}

func TestShell(t *testing.T) {
	clean := newRepo(t, cleanFiles())
	leaky := newRepo(t, leakyFiles())
	// An unstaged change under the root, with nothing personal in it: the
	// diff of the working tree names a file there.
	for _, root := range []string{clean, leaky} {
		write(t, root, reportJSON, `{"layoutOptimization": "Fit"}`)
	}
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	shIn := func(dir, cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd, Cwd: dir} }
	for _, tc := range []struct {
		name         string
		ev           gate.Event
		clean, leaky string
	}{
		// Rule 1: the tool alone.
		{"tool alone", sh("pbipx scan --all"), "", ""},
		{"tool after an assignment", sh("PBIPX_NO_REVEAL=1 pbipx scan pbip"), "", ""},
		{"tool with a redirection", sh("pbipx scan --all > out.txt 2>&1"), "", ""},
		{"tool chained", sh("pbipx scan --all; cat pbip/B.Report/definition/report.json"), "", gate.ReasonFinding},
		{"tool with a backtick", sh("pbipx scan `echo pbip`"), "", gate.ReasonFinding},
		{"tool by path is not the tool", sh("./pbipx pbip"), "", gate.ReasonFinding},

		// Rule 2: git content.
		{"git diff", sh("git diff"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git diff --stat", sh("git diff --stat"), "", ""},
		{"git diff --name-only", sh("git diff --name-only HEAD~1"), "", ""},
		{"git diff --stat -p", sh("git diff --stat -p"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git diff -- notes", sh("git diff -- notes"), "", ""},
		{"git diff -- .", sh("git diff -- ."), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git diff of a path without --", sh("git diff notes"), "", ""},
		{"git diff of a root without --", sh("git diff pbip"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git diff excluding the root", sh("git diff -- . ':!pbip'"), "", gate.ReasonFinding},
		{"git diff excluding with ^", sh("git diff -- ':^pbip'"), "", gate.ReasonFinding},
		{"git diff excluding with long magic", sh("git diff -- ':(exclude)pbip'"), "", gate.ReasonFinding},
		{"git diff excluding inside the root only", sh("git diff -- ':!pbip/A.Report/StaticResources'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git diff glob exclusion, judged by git", sh("git diff -- ':!pbi*'"), "", ""},
		{"git diff glob pathspec, judged by git", sh("git diff -- 'notes/*'"), "", ""},
		{"git diff icase pathspec, judged by git", sh("git diff -- ':(icase)notes'"), "", ""},
		{"git diff icase pathspec reaching the root", sh("git diff -- ':(icase)PBIP'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git diff top pathspec", sh("git diff -- :/notes"), "", ""},
		{"git diff path outside the repository", sh("git diff -- ../elsewhere"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git log", sh("git log --oneline"), "", ""},
		{"git log -p", sh("git log -p"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git log -U1", sh("git log -U1"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git log -L", sh("git log -L1,2:README.md"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git log -p limited", sh("git log -p -- notes sql"), "", ""},
		{"git log grep for a value", sh("git log -p --grep='Doe, Jane'"), "", ""},
		{"git reflog -p", sh("git reflog -p"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git show", sh("git show"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git show --stat", sh("git show --stat HEAD"), "", ""},
		{"git show REV:path outside", sh("git show HEAD:README.md"), "", ""},
		{"git show REV:path inside", sh("git show HEAD:" + reportJSON), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git show --stat REV:path inside", sh("git show --stat HEAD:" + reportJSON), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git show index path inside", sh("git show :" + reportJSON), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git show commit limited", sh("git show HEAD -- notes"), "", ""},
		{"git blame outside", sh("git blame README.md"), "", ""},
		{"git blame inside", sh("git blame -L 1,2 " + reportJSON), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git -C moves blame into a root", sh("git -C pbip/A.Report blame definition/report.json"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git -C moves blame away", sh("git -C notes blame clean.md"), "", ""},
		{"git annotate inside", sh("git annotate " + reportJSON), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git grep", sh("git grep student"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git grep limited", sh("git grep student -- notes"), "", ""},
		{"git archive", sh("git archive HEAD"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git format-patch", sh("git format-patch -1"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git cat-file -p REV:path outside", sh("git cat-file -p HEAD:README.md"), "", ""},
		{"git cat-file blob REV:path inside", sh("git cat-file blob HEAD:" + reportJSON), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git cat-file -p object", sh("git cat-file -p HEAD"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git cat-file -t", sh("git cat-file -t HEAD"), "", ""},
		{"git cat-file --batch", sh("git cat-file --batch"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git cat-file --batch-check", sh("git cat-file --batch-check"), "", ""},
		{"git stash show", sh("git stash show"), "", ""},
		{"git stash show -p with no stash", sh("git stash show -p"), "", ""},
		{"git --git-dir", sh("git --git-dir=.git diff -- notes"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git --no-pager", sh("git --no-pager diff"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git -c option", sh("git -c core.pager=cat diff"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"env before git", sh("env GIT_PAGER=cat git diff"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git by path", sh("/usr/bin/git diff"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git after another command", sh("echo start; git diff | head"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git in a subshell", sh("(git diff)"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git status", sh("git status"), "", ""},
		{"commit message quoting git diff", sh(`git commit -m "run git diff first"`), "", ""},
		{"git diff from inside a root", shIn("pbip/A.Report", "git diff -- ."), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git diff from inside a root, limited away", shIn("pbip/A.Report", "git diff -- ../../notes"), "", gate.ReasonFinding},

		// Divergence: local state named in a command.
		{"cat local state", sh("cat " + localState), gate.ReasonLocalState, gate.ReasonLocalState},
		{"quoted local state", sh("cat '" + localState + "'"), gate.ReasonLocalState, gate.ReasonLocalState},
		{"local state as input", sh("wc -l < " + localState), gate.ReasonLocalState, gate.ReasonLocalState},
		{"local state as a flag value", sh("jq . --from-file=" + localState), gate.ReasonLocalState, gate.ReasonLocalState},
		{"local state by glob", sh("cat pbip/*/.pbi/localSettings.json"), gate.ReasonLocalState, gate.ReasonLocalState},
		{"local-state directory", sh("ls pbip/A.Report/.pbi/"), gate.ReasonLocalState, gate.ReasonLocalState},
		{"search of a local-state directory", sh("rg lastFilter pbip/A.Report/.pbi"), gate.ReasonLocalState, gate.ReasonLocalState},
		{"local state relative to the cwd", shIn("pbip/A.Report", "cat .pbi/localSettings.json"), gate.ReasonLocalState, gate.ReasonLocalState},
		{"missing local state", sh("cat pbip/Z.Report/.pbi/localSettings.json"), gate.ReasonLocalState, gate.ReasonLocalState},

		// Rule 3: the tree scan.
		{"cwd inside a root", shIn("pbip/A.Report", "ls"), "", gate.ReasonFinding},
		{"names the root", sh("cat " + reportJSON), "", gate.ReasonFinding},
		{"names the root by absolute path", sh("ls /x/../" + reportJSON), "", gate.ReasonFinding},
		{"names the root in quotes", sh(`wc -c "pbip"`), "", gate.ReasonFinding},
		{"unbalanced quotes still name the root", sh("cat 'pbip/A.Report"), "", gate.ReasonFinding},
		{"a longer word is not the root", sh("ls pbipx-notes pbip.bak my-pbip"), "", ""},
		{"a glob matching the root", sh("ls p*"), "", gate.ReasonFinding},
		{"a quoted glob is not expanded", sh("ls 'p*'"), "", ""},
		{"away from the roots", sh("ls sql"), "", ""},

		// Rule 4: recursive searches.
		{"grep -r over .", sh("grep -r student ."), "", gate.ReasonFinding},
		{"grep -rn over notes", sh("grep -rn student notes"), "", ""},
		{"grep -R with no path", sh("grep -R student"), "", gate.ReasonFinding},
		{"grep --recursive", sh("grep --recursive student ."), "", gate.ReasonFinding},
		{"grep -d recurse", sh("grep -d recurse student ."), "", gate.ReasonFinding},
		{"grep -e gives the pattern", sh("grep -r -e student ."), "", gate.ReasonFinding},
		{"grep -e and a path away", sh("grep -r -e student notes"), "", ""},
		{"grep with a value as the pattern", sh("grep -r 'Doe, Jane' ."), "", gate.ReasonFinding},
		{"grep not recursive", sh("grep -n student README.md"), "", ""},
		{"egrep -r", sh("egrep -r student"), "", gate.ReasonFinding},
		{"rg default path", sh("rg student"), "", gate.ReasonFinding},
		{"rg over notes", sh("rg -i -g '*.md' student notes"), "", ""},
		{"rg over notes and .", sh("rg -e student notes ."), "", gate.ReasonFinding},
		{"rg --files", sh("rg --files"), "", gate.ReasonFinding},
		{"rg in a pipeline", sh("cat README.md | rg -n student"), "", gate.ReasonFinding},
		{"rg above the repository", shIn("sql", "rg student ../.."), "", gate.ReasonFinding},
		{"rg from a directory away", shIn("sql", "rg student"), "", ""},
		{"ag", sh("ag student"), "", gate.ReasonFinding},
		{"ack over notes", sh("ack student notes"), "", ""},
		{"xargs grep -r", sh("echo . | xargs grep -r student"), "", gate.ReasonFinding},

		// Rule 5.
		{"plain command", sh("go test ./..."), "", ""},
		{"empty command", sh(""), "", ""},
	} {
		run(t, clean, []decideCase{{tc.name + " (clean)", tc.ev, tc.clean}})
		run(t, leaky, []decideCase{{tc.name + " (leaky)", tc.ev, tc.leaky}})
	}
}

func TestOtherToolsAllow(t *testing.T) {
	root := newRepo(t, leakyFiles())
	run(t, root, []decideCase{
		{"other", gate.Event{Tool: gate.Other, Path: leakVisual}, ""},
		{"empty tool", gate.Event{Path: leakVisual}, ""},
		{"unknown tool", gate.Event{Tool: "write", Path: leakVisual}, ""},
	})
}

func TestSetupFailures(t *testing.T) {
	isolateGit(t)
	notRepo := t.TempDir()
	write(t, notRepo, reportJSON, "{}")
	for _, tc := range []struct {
		name   string
		cwd    string
		reason string
	}{
		{"no working directory", "", gate.ReasonEventError},
		{"relative working directory", "pbip", gate.ReasonEventError},
		{"missing working directory", filepath.Join(notRepo, "missing"), gate.ReasonNotARepository},
		{"not a repository", notRepo, gate.ReasonNotARepository},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, tool := range []gate.Tool{gate.Read, gate.Search, gate.Shell, gate.Other} {
				d := gate.Decide(gate.Event{Tool: tool, Path: reportJSON, Cwd: tc.cwd}, gate.Options{})
				check(t, d, tc.reason)
			}
		})
	}
}

func TestPolicyAndBaselineErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		reason string
	}{
		{"missing policy", map[string]string{".pbipx.toml": ""}, gate.ReasonPolicyError},
		{"unknown key", map[string]string{".pbipx.toml": "version = 1\nprotected_roots = [\"pbip\"]\nnote = \"Doe, Jane\"\n"}, gate.ReasonPolicyError},
		{"invalid TOML", map[string]string{".pbipx.toml": "version = = 'Doe, Jane'\n"}, gate.ReasonPolicyError},
		{"invalid baseline", map[string]string{baselineFile: "version = 1\n[\"Doe, Jane\"]\nseverity = 'block'\n"}, gate.ReasonPolicyError},
		{"baseline not TOML", map[string]string{baselineFile: "Doe, Jane"}, gate.ReasonPolicyError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := cleanFiles()
			for k, v := range tc.files {
				files[k] = v
			}
			root := newRepo(t, files)
			if policy, ok := tc.files[".pbipx.toml"]; ok && policy == "" {
				if err := os.Remove(filepath.Join(root, ".pbipx.toml")); err != nil {
					t.Fatal(err)
				}
			}
			for _, tool := range []gate.Tool{gate.Read, gate.Search, gate.Shell, gate.Other} {
				d := gate.Decide(gate.Event{Tool: tool, Path: "README.md", Command: "ls", Cwd: root}, gate.Options{})
				check(t, d, tc.reason)
			}
		})
	}
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBaselineErrorNamesTheBaseline(t *testing.T) {
	files := cleanFiles()
	files[baselineFile] = "not toml at all ["
	root := newRepo(t, files)
	d := gate.Decide(gate.Event{Tool: gate.Other, Cwd: root}, gate.Options{})
	if !strings.Contains(d.Message, baselineFile) {
		t.Errorf("the message does not name the baseline: %s", d.Message)
	}
}

func TestCheckPolicy(t *testing.T) {
	root := newRepo(t, cleanFiles())
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	var gotRoot string
	var gotData []byte
	unapproved := &gate.Decision{Reason: gate.ReasonPolicyUnapproved, Message: "the policy has no approval"}
	d := gate.Decide(gate.Event{Tool: gate.Other, Cwd: root}, gate.Options{
		CheckPolicy: func(r string, data []byte) *gate.Decision {
			gotRoot, gotData = r, data
			return unapproved
		},
	})
	check(t, d, gate.ReasonPolicyUnapproved)
	if gotRoot != resolved || string(gotData) != defaultPolicy(t) {
		t.Errorf("CheckPolicy got root %q and %d bytes, want the repository root and its policy's bytes", gotRoot, len(gotData))
	}

	d = gate.Decide(gate.Event{Tool: gate.Read, Path: reportJSON, Cwd: root}, gate.Options{
		CheckPolicy: func(string, []byte) *gate.Decision { return nil },
	})
	check(t, d, "")

	// A policy that fails to load is reported before the check runs.
	if err := os.WriteFile(filepath.Join(root, ".pbipx.toml"), []byte("version = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	d = gate.Decide(gate.Event{Tool: gate.Other, Cwd: root}, gate.Options{
		CheckPolicy: func(string, []byte) *gate.Decision { called = true; return nil },
	})
	check(t, d, gate.ReasonPolicyError)
	if called {
		t.Error("CheckPolicy ran for a policy that failed to load")
	}
}

func TestPanicDenies(t *testing.T) {
	root := newRepo(t, cleanFiles())
	d := gate.Decide(gate.Event{Tool: gate.Other, Cwd: root}, gate.Options{
		CheckPolicy: func(string, []byte) *gate.Decision { panic("Doe, Jane") },
	})
	check(t, d, gate.ReasonInternalError)
}

func TestReasonsInStepWithVocabulary(t *testing.T) {
	v, err := corpus.LoadVocabulary(filepath.Join(corpusDir, "vocabulary.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := mapKeys(v.GateReasons)
	got := slices.Clone(gate.Reasons)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("gate.Reasons = %v, vocabulary has %v", got, want)
	}
}
