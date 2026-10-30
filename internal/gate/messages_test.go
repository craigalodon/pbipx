package gate_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// messageCase is a call, the reason class it is denied with, and the
// words its message must hold: what triggered the refusal, and the hint
// it ends with ("" to leave the end unchecked).
type messageCase struct {
	name   string
	ev     gate.Event // Cwd relative to the repository root
	reason string
	want   []string
	hint   string
}

func runMessages(t *testing.T, root string, cases []messageCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := tc.ev
			ev.Cwd = filepath.Join(root, filepath.FromSlash(ev.Cwd))
			d := gate.Decide(ev, gate.Options{})
			check(t, d, tc.reason)
			for _, w := range tc.want {
				if !strings.Contains(d.Message, w) {
					t.Errorf("the message lacks %q: %s", w, d.Message)
				}
			}
			if tc.hint != "" && !strings.HasSuffix(d.Message, tc.hint) {
				t.Errorf("the message does not end with the hint %q: %s", tc.hint, d.Message)
			}
			if !strings.HasSuffix(d.Message, ".") {
				t.Errorf("the message does not end with a sentence: %s", d.Message)
			}
		})
	}
}

// The hints messages end with.
const (
	hintSearch    = "Name the files to search explicitly, or exclude the protected folders."
	hintGit       = "Limit the paths (`-- src`) or use --stat."
	hintSelect    = "Clear the selection in Power BI Desktop and save, or ask a person to run `pbipx scan` at a terminal."
	hintCwd       = "Run it from outside the protected folders; use Read for a single file there."
	hintNames     = "Leave the protected folders out of the command; use Read for a single file there."
	hintExpand    = "Write the paths out in full, without variables or command substitutions."
	hintPlain     = "Write the command out plainly, without building it at run time or nesting it more than 4 deep."
	hintLister    = "Point find or fd away from the protected folders, or exclude them."
	hintChange    = "Run the change on its own, then the read as a separate call."
	hintOption    = "Drop that global option, or use --stat."
	hintGitExpand = "Write git's arguments out in full, without variables or command substitutions."
)

// The file the leaky tree's tree scans reach first.
const reached = "so the call could reach " + leakVisual + ", which has a block finding " +
	"(context slicer-selection; column student.full_name; reasons block-column, person-name-shape)"

// TestMessagesNameTheTrigger checks that every refusal says what
// triggered it and ends with a hint.
func TestMessagesNameTheTrigger(t *testing.T) {
	isolateSearch(t)
	root := newRepo(t, leakyFiles())
	read := func(p string) gate.Event { return gate.Event{Tool: gate.Read, Path: p} }
	runMessages(t, root, []messageCase{
		// A Read names the file and the finding, and the finding's hint.
		{"read of a finding", read(leakVisual), gate.ReasonFinding,
			[]string{leakVisual + " has a block finding (context slicer-selection; column student.full_name"}, hintSelect},
		{"read of local state", read(localState), gate.ReasonLocalState, []string{localState + " is Power BI Desktop local state"}, ""},
		{"grep tool over the root", gate.Event{Tool: gate.Search, Path: "pbip"}, gate.ReasonFinding,
			[]string{"the search covers a path that is the protected root pbip, " + reached}, hintSearch},
		{"grep tool from the repository root", gate.Event{Tool: gate.Search}, gate.ReasonFinding,
			[]string{"the search covers a path that holds the protected root pbip"}, hintSearch},

		// A search taken to be recursive names why.
		{"--depth", sh("grep --depth=99 student"), gate.ReasonFinding,
			[]string{"grep is taken to search its working directory recursively (the option --depth, with no file operands), " +
				"and the working directory holds the protected root pbip, " + reached}, hintSearch},
		{"-r", sh("grep -r student"), gate.ReasonFinding, []string{"(the option -r, with no file operands)"}, hintSearch},
		{"-R in a cluster", sh("grep -nR student"), gate.ReasonFinding, []string{"(the option -R, with no file operands)"}, hintSearch},
		{"-d recurse", sh("grep -d recurse student"), gate.ReasonFinding, []string{"(the option -d recurse, with no file operands)"}, hintSearch},
		{"--glob", sh("ugrep --glob=*.json student"), gate.ReasonFinding, []string{"ugrep is taken", "(the option --glob, with no file operands)"}, hintSearch},
		{"-NUM", sh("ugrep -3 student"), gate.ReasonFinding, []string{"(the option -NUM, with no file operands)"}, hintSearch},
		{"an unknown option", sh("grep --frobnicate student README.md"), gate.ReasonFinding,
			[]string{"(an option the gate does not know)"}, hintSearch},
		{"no file operands", sh("rg student"), gate.ReasonFinding,
			[]string{"rg is taken to search its working directory recursively (no file operands)"}, hintSearch},
		{"GREP_OPTIONS", sh("GREP_OPTIONS=-r grep student README.md"), gate.ReasonFinding, []string{"(GREP_OPTIONS is set)"}, hintSearch},
		{"a pattern operand", sh("grep student 'pbi*'"), gate.ReasonFinding, []string{"(a file operand that is a pattern)"}, hintSearch},
		{"an option that reads anywhere", sh("ugrep --config=x student README.md"), gate.ReasonFinding,
			[]string{"ugrep is taken to read files anywhere: the option --config, which reads files the command does not name"}, hintSearch},
		{"an argument that cannot be expanded", sh("rg $PATTERN README.md"), gate.ReasonFinding,
			[]string{"rg is given a word with an expansion the gate cannot resolve"}, hintExpand},
		{"a search of the root", sh("rg -n student ./p*"), gate.ReasonFinding,
			[]string{"rg searches a path that is the protected root pbip"}, hintSearch},
		{"a search of a directory above", gate.Event{Tool: gate.Shell, Command: "grep -r student ..", Cwd: "notes"}, gate.ReasonFinding,
			[]string{"grep searches a path that holds the protected root pbip"}, hintSearch},

		// A tree scan for any other reason says which.
		{"cwd in a root", gate.Event{Tool: gate.Shell, Command: "wc -c report.json", Cwd: "pbip/A.Report"}, gate.ReasonFinding,
			[]string{"the working directory is in the protected root pbip, " + reached}, hintCwd},
		{"names a root", sh("make -C pbip"), gate.ReasonFinding, []string{"the command names the protected root pbip"}, hintNames},
		{"a word reaches a root", sh("wc -c ./p*/A.Report/definition/report.json"), gate.ReasonFinding,
			[]string{"wc is given a path that is in the protected root pbip"}, hintNames},
		{"cd to a root", sh("cd pb''ip && make"), gate.ReasonFinding,
			[]string{"a cd moves the shell into a directory that is the protected root pbip"}, hintNames},
		{"cd to an expansion", sh("cd $SOMEWHERE && make"), gate.ReasonFinding,
			[]string{"a cd, pushd or popd moves a command to a working directory the gate cannot tell"}, hintExpand},
		{"an unresolvable expansion", sh("cat $SOMEFILE"), gate.ReasonFinding,
			[]string{"cat is given a word with an expansion the gate cannot resolve"}, hintExpand},
		{"a line built at run time", sh(`bash -c "$CMD"`), gate.ReasonFinding,
			[]string{"a nested command line is built at run time"}, hintPlain},
		{"find -exec", sh("find . -name '*.json' -exec cat {} +"), gate.ReasonFinding,
			[]string{"find runs a command on, or reads, the files it finds, in a directory that holds the protected root pbip"}, hintLister},
		{"an unbalanced quote", sh("cat 'README.md"), gate.ReasonFinding,
			[]string{"the line has an unbalanced quote or a trailing backslash"}, "Balance the quotes in the command."},

		// git names the subcommand, and the path it would print or what
		// makes its content unprovable.
		{"git log -p", sh("git log -p"), gate.ReasonGitContent,
			[]string{"git log would print content from pbip/", "which is in the protected root pbip; git history cannot be vetted."}, hintGit},
		{"git grep", sh("git grep -e student"), gate.ReasonGitContent,
			[]string{"git grep would print content from " + leakVisual + ", which is in the protected root pbip"}, hintGit},
		{"git show of a root file", sh("git show HEAD:" + leakVisual), gate.ReasonGitContent,
			[]string{"git show is given a path that reaches the protected root pbip"}, hintGit},
		{"git blame of a root file", sh("git blame " + leakVisual), gate.ReasonGitContent,
			[]string{"git blame is given a path that reaches the protected root pbip"}, hintGit},
		{"git archive", sh("git archive HEAD"), gate.ReasonGitContent,
			[]string{"git archive has no pathspec that keeps it out of the protected root pbip"}, hintGit},
		{"git -c", sh("git -c core.pager=less diff"), gate.ReasonGitContent,
			[]string{"git diff runs with -c setting a key"}, hintOption},
		{"git message search", sh("git show :/fix"), gate.ReasonGitContent,
			[]string{"git show is given a revision that is not a commit"}, hintGit},
		{"git object name", sh("git cat-file -p HEAD"), gate.ReasonGitContent,
			[]string{"git cat-file is given an object name, whose content cannot be placed"}, hintGit},
		{"git grep -O", sh("git grep -O student"), gate.ReasonGitContent, []string{"git grep with -O opens"}, hintGit},
		{"git unknown subcommand", sh("git frobnicate"), gate.ReasonGitContent,
			[]string{"git runs a subcommand that is neither one of git's own nor an alias"}, ""},
		{"git --pathspec-from-file", sh("git add -p --pathspec-from-file=list.txt"), gate.ReasonGitContent,
			[]string{"git add is given --pathspec-from-file"}, hintGit},
		{"git glob pathspec", sh("git add -p -- 'p*'"), gate.ReasonGitContent,
			[]string{"git add is given a pathspec with a glob"}, hintGit},
		{"git log -L", sh("git log -L1,2:README.md"), gate.ReasonGitContent, []string{"git log with -L, --stdin or a remerge diff"}, hintGit},
		{"git environment", sh("GIT_PAGER=less git diff"), gate.ReasonGitContent,
			[]string{"git diff runs after an assignment"}, "Drop the assignment before git."},
		{"git --git-dir", sh("git --git-dir=.git log -p"), gate.ReasonGitContent,
			[]string{"git log is pointed at another tree by --git-dir or --work-tree"}, ""},
		// An argument git is given that the gate cannot expand is never
		// echoed.
		{"git unknown subcommand word", sh("git $SUB"), gate.ReasonGitContent,
			[]string{"git runs a subcommand the gate cannot expand"}, hintGitExpand},
		{"git unknown global option", sh("git --namespace=$N log"), gate.ReasonGitContent,
			[]string{"git log is given a global option the gate cannot expand"}, hintGitExpand},
		{"git unknown option", sh("git log $(echo -p)"), gate.ReasonGitContent,
			[]string{"git log is given, before --, an argument the gate cannot expand", "could be an option that prints content"},
			hintGitExpand},
		{"git unknown pathspec", sh("git diff -- $X"), gate.ReasonGitContent,
			[]string{"git diff is given an argument the gate cannot expand", "could name a path in a protected root"},
			hintGitExpand},

		// A read after a tree change names the change.
		{"read after a switch", sh("git switch -d HEAD && cat README.md"), gate.ReasonGitContent,
			[]string{"git switch changes the working tree"}, hintChange},
		{"read after tar -x", sh("tar -xf x.tar && cat README.md"), gate.ReasonGitContent,
			[]string{"tar -x changes the working tree"}, hintChange},
	})

	// A search configuration in the working directory.
	conf := newRepo(t, leakyFiles())
	write(t, conf, ".ugrep", "recursive\n")
	runMessages(t, conf, []messageCase{
		{"a .ugrep file", sh("grep student README.md"), gate.ReasonFinding,
			[]string{"grep is taken to search its working directory recursively (a .ugrep configuration file)"}, hintSearch},
	})

	// A path git would print is shown segment by segment: one shaped like
	// a value is {?}.
	files := cleanFiles()
	delete(files, localState)
	files["pbip/A.Report/Doe, Jane/notes.md"] = "x\n"
	shaped := newRepo(t, files)
	runMessages(t, shaped, []messageCase{
		{"a shaped segment", sh("git log -p -- pbip/A.Report/D*"), gate.ReasonGitContent,
			[]string{"git log would print content from pbip/A.Report/{?}/notes.md"}, hintGit},
	})
}

// TestMessagesNeverEchoTheCommand plants a sentinel as an unknown option,
// a pattern, an operand, an expansion, an alias's name, a revision and a
// configuration key, and checks that each call is denied and the sentinel
// appears in no message.
func TestMessagesNeverEchoTheCommand(t *testing.T) {
	isolateSearch(t)
	const sentinel = "zq7sentinel"
	root := newRepo(t, leakyFiles())
	gitIn(t, root, "config", "alias."+sentinel, "!cat x")
	for _, line := range []string{
		"grep --" + sentinel + " student",
		"grep --" + sentinel + "=x student README.md",
		"grep -r " + sentinel,
		"grep -e " + sentinel + " -r",
		"rg -" + sentinel + " student",
		"rg " + sentinel,
		"ugrep --conf=" + sentinel + " x README.md",
		"cat $" + sentinel,
		"cd $" + sentinel + " && cat x",
		"bash -c \"$" + sentinel + "\"",
		"git " + sentinel,
		"git " + sentinel + "x",
		"git show " + sentinel,
		"git log -p --format=" + sentinel,
		"git -c " + sentinel + ".x=1 diff",
		"git log -p -- . ':!" + sentinel + "'",
		"git show HEAD:" + sentinel + "/../" + leakVisual,
		"git grep -e student -- . " + sentinel,
		"git switch -d HEAD && cat " + sentinel,
		"find . -name " + sentinel + " -exec cat {} +",
		"wc -c " + sentinel + " ./p*/A.Report/definition/report.json",
		"echo " + sentinel + " | xargs grep -r x",
		"GREP_OPTIONS=" + sentinel + " grep x README.md",
		"cat '" + sentinel,
	} {
		d := gate.Decide(gate.Event{Tool: gate.Shell, Command: line, Cwd: root}, gate.Options{})
		if d.Allow {
			t.Errorf("%q is allowed; the test wants a message to look at", line)
			continue
		}
		if strings.Contains(strings.ToLower(d.Message), sentinel) {
			t.Errorf("%q: the message echoes the command: %s", line, d.Message)
		}
	}
}
