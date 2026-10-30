package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

func sh(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }

func shIn(dir, cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd, Cwd: dir} }

// leakInReport is the leaking visual below its report directory.
const leakInReport = "definition/pages/0000/visuals/0001/visual.json"

// both is a case decided in a clean and in a leaky repository.
type both struct {
	name         string
	ev           gate.Event
	clean, leaky string
}

func runBoth(t *testing.T, cases []both) {
	t.Helper()
	clean := newRepo(t, cleanFiles())
	leaky := newRepo(t, leakyFiles())
	for _, tc := range cases {
		run(t, clean, []decideCase{{tc.name + " (clean)", tc.ev, tc.clean}})
		run(t, leaky, []decideCase{{tc.name + " (leaky)", tc.ev, tc.leaky}})
	}
}

// nestedRepo is leakyFiles with the protected root moved to work/pbip, so
// a path can reach the root without spelling its name.
func nestedRepo(t *testing.T) string {
	t.Helper()
	policy := strings.ReplaceAll(defaultPolicy(t), `"pbip`, `"work/pbip`)
	files := map[string]string{".pbipx.toml": policy}
	for name, content := range leakyFiles() {
		if strings.HasPrefix(name, "pbip/") {
			name = "work/" + name
		}
		files[name] = content
	}
	files["work/other/readme.md"] = "Nothing here.\n"
	return newRepo(t, files)
}

func TestShellWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CDPATH", "")
	runBoth(t, []both{
		{"cd up then search", shIn("notes", "cd .. && rg Doe"), "", gate.ReasonFinding},
		{"cd away then search", shIn("notes", "cd ../sql && rg Doe"), "", ""},
		{"cd away from the root", sh("cd sql && rg student"), "", ""},
		{"cd away and back", sh("cd sql; cd ..; rg x"), "", gate.ReasonFinding},
		{"cd into a root", sh("cd sql && cd ../pbip && ls"), "", gate.ReasonFinding},
		{"cd to a variable", sh("cd sql && cd $DIR && ls"), "", gate.ReasonFinding},
		{"cd -", sh("cd - && ls"), "", gate.ReasonFinding},
		{"cd home", sh("cd && ls"), "", ""},
		{"cd ~", sh("cd ~ && ls"), "", ""},
		{"cd -P", sh("cd -P sql && rg x"), "", ""},
		{"pushd away", sh("pushd sql && rg x"), "", ""},
		{"popd", sh("pushd sql && popd && ls"), "", gate.ReasonFinding},
		{"cd in a subshell carries forward", sh("(cd sql) && rg x"), "", ""},
		{"the directory before a cd", sh("rg x; cd sql"), "", gate.ReasonFinding},
		{"cd to an absolute path outside", sh("cd " + home + " && rg x"), "", ""},
	})

	root := nestedRepo(t)
	run(t, root, []decideCase{
		{"cd into the parent of a root, then read", sh("cd work && cat pbip/B.Report/" + leakInReport), gate.ReasonFinding},
		{"cd through a directory, then read", sh("cd work/other && cat ../pbip/B.Report/" + leakInReport), gate.ReasonFinding},
		{"cd beside the root, then read there", sh("cd work/other && cat readme.md"), ""},
	})

	t.Setenv("HOME", "")
	runBoth(t, []both{{"cd home, home unknown", sh("cd && ls"), "", gate.ReasonFinding}})
}

func TestShellCDPATH(t *testing.T) {
	t.Setenv("CDPATH", "/somewhere")
	runBoth(t, []both{
		{"bare name under CDPATH", sh("cd sql && rg x"), "", gate.ReasonFinding},
		{"dot-relative under CDPATH", sh("cd ./sql && rg x"), "", ""},
	})
}

func TestShellResolvesWords(t *testing.T) {
	root := nestedRepo(t)
	run(t, root, []decideCase{
		{"relative to a parent of the root", shIn("work", "cat pbip/B.Report/"+leakInReport), gate.ReasonFinding},
		{"the root by a relative name", shIn("work", "ls pbip"), gate.ReasonFinding},
		{"a directory that contains the root", sh("ls work"), gate.ReasonFinding},
		{"a sibling of the root", shIn("work", "ls other"), ""},
		{"from inside the root's sibling", shIn("work/other", "cat ../pbip/B.Report/"+leakInReport), gate.ReasonFinding},
		{"a flag value", shIn("work", "jq . --from-file=pbip/B.Report/"+leakInReport), gate.ReasonFinding},
		{"a redirection", shIn("work", "wc -l < pbip/B.Report/"+leakInReport), gate.ReasonFinding},
	})
	if err := os.Symlink(filepath.Join("..", "work", "pbip", "B.Report"), filepath.Join(root, "notes", "lnk")); err != nil {
		t.Skip("symlinks are not available")
	}
	run(t, root, []decideCase{
		{"through a link", sh("cat notes/lnk/" + leakInReport), gate.ReasonFinding},
	})
}

func TestShellKeywords(t *testing.T) {
	runBoth(t, []both{
		{"if", sh("if rg -q x; then echo y; fi"), "", gate.ReasonFinding},
		{"then", sh("if true; then rg x; fi"), "", gate.ReasonFinding},
		{"while", sh("while true; do rg x; done"), "", gate.ReasonFinding},
		{"until", sh("until rg -q x; do sleep 1; done"), "", gate.ReasonFinding},
		{"!", sh("! rg Doe"), "", gate.ReasonFinding},
		{"group", sh("{ rg Doe; }"), "", gate.ReasonFinding},
		{"function definition", sh("f() { rg Doe; }; f"), "", gate.ReasonFinding},
		{"function keyword", sh("function f { rg Doe; }"), "", gate.ReasonFinding},
		{"time", sh("time rg x"), "", gate.ReasonFinding},
		{"for over files", sh("for f in notes/*; do cat $f; done"), "", gate.ReasonFinding},
		{"git after if", sh("if true; then git log -p; fi"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git after !", sh("! git log -p"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"git in a group", sh("{ git show; }"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a test away from the roots", sh("if [ -f x ]; then echo y; fi"), "", ""},
		{"a search away after if", shIn("sql", "if rg -q x; then echo y; fi"), "", ""},
	})
}

func TestShellGlobstar(t *testing.T) {
	runBoth(t, []both{
		{"** from the repository root", sh("cat **/*.md"), "", gate.ReasonFinding},
		{"** under notes", sh("cat notes/**/*.md"), "", ""},
		{"** under the root", sh("grep Doe pbip/**/visual.json"), "", gate.ReasonFinding},
		{"** reaching local state", sh("cat pbip/**/localSettings.json"), gate.ReasonLocalState, gate.ReasonLocalState},
		{"** quoted", sh("cat 'notes/**/x'"), "", ""},
	})
}

func TestShellVariables(t *testing.T) {
	root := nestedRepo(t)
	t.Setenv("HOME", filepath.Join(root, "work"))
	run(t, root, []decideCase{
		{"~", sh("cat ~/pbip/B.Report/" + leakInReport), gate.ReasonFinding},
		{"$HOME", sh("cat $HOME/pbip/B.Report/" + leakInReport), gate.ReasonFinding},
		{"${HOME}", sh(`cat "${HOME}/pbip/B.Report/` + leakInReport + `"`), gate.ReasonFinding},
		{"$PWD", shIn("work", "cat $PWD/pbip/B.Report/"+leakInReport), gate.ReasonFinding},
		{"${PWD} after cd", sh("cd work && cat ${PWD}/pbip/B.Report/" + leakInReport), gate.ReasonFinding},
		{"$OLDPWD", sh("cd work && cd ../notes && cat $OLDPWD/pbip/B.Report/" + leakInReport), gate.ReasonFinding},
		{"$OLDPWD away", sh("cd sql && cd ../notes && cat $OLDPWD/clean.sql"), ""},
		{"~ away", sh("cat ~/other/readme.md"), ""},
		{"~user is unknown", sh("cat ~nobody/x"), gate.ReasonFinding},
		{"single quotes keep $HOME literal", sh("cat '$HOME/x'"), ""},
	})
	runBoth(t, []both{
		{"unknown variable in a reader", sh("cat $FILE"), "", gate.ReasonFinding},
		{"unknown braces in a reader", sh("head -n 1 ${FILE:-x}"), "", gate.ReasonFinding},
		{"unknown $OLDPWD", sh("cat $OLDPWD/x"), "", gate.ReasonFinding},
		{"command substitution in a reader", sh(`cat "$(echo x)"`), "", gate.ReasonFinding},
		{"unknown variable in a search", shIn("sql", "grep x $F"), "", gate.ReasonFinding},
		{"unknown variable, not a reader", shIn("sql", "echo $FILE"), "", ""},
		{"unknown option value is still unknown", shIn("sql", "grep --file=$F x clean.sql"), "", gate.ReasonFinding},
	})
}

func TestShellFindAndXargs(t *testing.T) {
	runBoth(t, []both{
		{"find -exec without a path", sh("find -exec cat {} +"), "", gate.ReasonFinding},
		{"find -exec over notes", sh(`find notes -exec cat {} \;`), "", ""},
		{"find -execdir", shIn("sql", "find .. -execdir cat {} +"), "", gate.ReasonFinding},
		{"find piped to xargs", sh("find -print0 | xargs -0 wc -l"), "", gate.ReasonFinding},
		{"find away piped to xargs", shIn("sql", "find -print0 | xargs -0 wc -l"), "", ""},
		{"xargs reader from the repository root", sh("echo x | xargs cat"), "", gate.ReasonFinding},
		{"xargs reader with options", sh("echo x | xargs -n 1 -I {} cat {}"), "", gate.ReasonFinding},
		{"xargs reader from a directory away", shIn("sql", "echo x | xargs cat"), "", ""},
		{"xargs non-reader", shIn("sql", "echo x | xargs echo"), "", ""},
		{"find -exec git", sh(`find notes -exec git log -p \;`), gate.ReasonGitContent, gate.ReasonGitContent},
	})
}

func TestShellFoldCase(t *testing.T) {
	root := newRepo(t, leakyFiles())
	upper := "PBIP/A.Report/.PBI/localSettings.json"
	for _, fold := range []bool{true, false} {
		restore := gate.SetFoldCase(fold)
		// Without folding, the other case is not local state; the root
		// named in it still scans the (leaky) tree.
		want, shellWant := "", gate.ReasonFinding
		if fold {
			want, shellWant = gate.ReasonLocalState, gate.ReasonLocalState
		}
		run(t, root, []decideCase{
			{"read local state in another case", gate.Event{Tool: gate.Read, Path: upper}, want},
			{"shell local-state directory in another case", sh("ls pbip/A.Report/.PBI/"), shellWant},
			{"read through the repository root in another case",
				gate.Event{Tool: gate.Read, Path: strings.ToUpper(root) + "/pbip/A.Report/.pbi/localSettings.json"}, want},
		})
		if fold {
			run(t, root, []decideCase{
				{"read a leak in another case", gate.Event{Tool: gate.Read, Path: "pbip/b.report/" + leakInReport}, gate.ReasonFinding},
				{"read unvettable in another case", gate.Event{Tool: gate.Read, Path: "PBIP/x.PBIX"}, gate.ReasonUnvettable},
			})
		}
		// A root named in another case scans the tree on every platform.
		run(t, root, []decideCase{
			{"search the root in another case", gate.Event{Tool: gate.Search, Path: "PBIP"}, gate.ReasonFinding},
			{"shell names the root in another case", sh("cat PBIP/B.Report/" + leakInReport), gate.ReasonFinding},
			{"cd into the root in another case", shIn("sql", "cd ../PBIP/B.Report && ls"), gate.ReasonFinding},
		})
		restore()
	}
}

func TestReadSearchWriteHome(t *testing.T) {
	files := settingsFiles()
	files[leakVisual] = slicer("student", "full_name", "Doe, Jane")
	root := newRepo(t, files)
	t.Setenv("HOME", root)
	run(t, root, []decideCase{
		{"read ~/", gate.Event{Tool: gate.Read, Path: "~/" + leakVisual, Cwd: "notes"}, gate.ReasonFinding},
		{"read ~/ clean", gate.Event{Tool: gate.Read, Path: "~/" + cleanNote, Cwd: "notes"}, ""},
		{"search ~/", gate.Event{Tool: gate.Search, Path: "~/pbip", Cwd: "notes"}, gate.ReasonFinding},
		{"write ~/", gate.Event{Tool: gate.Write, Path: "~/" + agentSettings, Cwd: "notes"}, gate.ReasonAgentSettings},
	})
}

func TestSymlinkThenDotDot(t *testing.T) {
	root := newRepo(t, leakyFiles())
	// notes/lnk is pbip/B.Report/definition, so notes/lnk/.. is
	// pbip/B.Report, not notes.
	if err := os.Symlink(filepath.Join("..", "pbip", "B.Report", "definition"), filepath.Join(root, "notes", "lnk")); err != nil {
		t.Skip("symlinks are not available")
	}
	through := "notes/lnk/../definition/pages/0000/visuals/0001/visual.json"
	run(t, root, []decideCase{
		{"read", gate.Event{Tool: gate.Read, Path: through}, gate.ReasonFinding},
		{"shell", sh("cat " + through), gate.ReasonFinding},
		{"search", gate.Event{Tool: gate.Search, Path: "notes/lnk/.."}, gate.ReasonFinding},
	})
}

func TestShellAgentSettingsDirectory(t *testing.T) {
	newControlDirs(t)
	root := newRepo(t, settingsFiles())
	outside := t.TempDir()
	run(t, root, []decideCase{
		{"cd into .claude, then redirect", sh("cd .claude && echo {} > settings.local.json"), gate.ReasonAgentSettings},
		{"cd into .claude, then tee", sh("cd tools/.claude && echo {} | tee settings.json"), gate.ReasonAgentSettings},
		{"cp into .claude/", sh("cp x.json .claude/"), gate.ReasonAgentSettings},
		{"cp into .claude", sh("cp x.json .claude"), gate.ReasonAgentSettings},
		{"cp -t .claude", sh("cp -t .claude x.json"), gate.ReasonAgentSettings},
		{"cp --target-directory", sh("cp --target-directory=tools/.claude x.json"), gate.ReasonAgentSettings},
		{"mv into .claude", sh("mv x .claude"), gate.ReasonAgentSettings},
		{"ln into .claude", sh("ln -s /tmp/x.json .claude/"), gate.ReasonAgentSettings},
		{"install into .claude", sh("install -m 644 x.json .claude/"), gate.ReasonAgentSettings},
		{"rsync into .claude", sh("rsync -a conf/ .claude/"), gate.ReasonAgentSettings},
		{"cp from inside .claude to .", sh("cd .claude && cp ../x.json ."), gate.ReasonAgentSettings},
		{"a .claude directory copied in", sh("cp -r " + filepath.Join(outside, ".claude") + " ."), gate.ReasonAgentSettings},
		{"dd into .claude", sh("dd if=x.json of=.claude/"), gate.ReasonAgentSettings},
		{"in another case", sh("cp x.json .CLAUDE/"), gate.ReasonAgentSettings},
		{"settings by **", sh("cat **/settings.local.json"), gate.ReasonAgentSettings},

		{"cp into a subdirectory of .claude", sh("cp x.md .claude/commands/"), ""},
		{"cp out of .claude", sh("cp .claude/commands/x.md " + outside), ""},
		{"cp into .claude outside the repository", sh("cp x.json " + filepath.Join(outside, ".claude")), ""},
		{"list .claude", sh("ls .claude"), ""},
		{"cd into .claude, then list", sh("cd .claude && ls"), ""},
	})
}

func TestShellSelfApproval(t *testing.T) {
	newControlDirs(t)
	root := newRepo(t, settingsFiles())
	run(t, root, []decideCase{
		{"the tool", sh("pbipx policy approve"), gate.ReasonAgentSettings},
		{"the tool by path", sh("/usr/local/bin/pbipx policy approve"), gate.ReasonAgentSettings},
		{"after an option", sh("pbipx --no-color policy approve"), gate.ReasonAgentSettings},
		{"piped into", sh("yes approve | pbipx policy approve"), gate.ReasonAgentSettings},
		{"under env", sh("env TERM=xterm pbipx policy approve"), gate.ReasonAgentSettings},
		{"under script", sh("script -qc 'pbipx policy approve' /dev/null"), gate.ReasonAgentSettings},
		{"under unbuffer", sh("unbuffer pbipx policy approve"), gate.ReasonAgentSettings},
		{"under expect", sh(`expect -c 'spawn pbipx policy approve; send "approve\r"'`), gate.ReasonAgentSettings},
		{"in sh -c", sh("sh -c 'pbipx policy approve'"), gate.ReasonAgentSettings},

		{"policy status", sh("pbipx policy status"), ""},
		{"a commit message", sh(`git commit -m "run pbipx policy approve"`), ""},
	})
}

func TestShellQuoting(t *testing.T) {
	runBoth(t, []both{
		{"ANSI-C quote hides a second command", sh(`pbipx scan $'\'' ; cat pbip/B.Report/` + leakInReport + ` #'`), "", gate.ReasonFinding},
		{"ANSI-C quoted path", sh(`cat $'\x70bip'/B.Report/` + leakInReport), "", gate.ReasonFinding},
		{"locale quote", sh(`cat $"pb"ip/B.Report/` + leakInReport), "", gate.ReasonFinding},
		{"empty quotes split the root", sh(`cat pb""ip/B.Report/` + leakInReport), "", gate.ReasonFinding},
		{"an escape splits the root", sh(`cat pb\ip/B.Report/` + leakInReport), "", gate.ReasonFinding},
		{"quotes split local state", sh(`cat pbip/A.Report/.p""bi/localSettings.json`), gate.ReasonLocalState, gate.ReasonLocalState},
	})
}

func TestShellNested(t *testing.T) {
	runBoth(t, []both{
		{"sh -c", shIn("sql", `bash -c "cd ../notes && rg x .."`), "", gate.ReasonFinding},
		{"sh -c git", sh("sh -c 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"bash -lc", sh("bash -lc 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"zsh -o opt -c", sh("zsh -o extendedglob -c 'git show'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"eval", sh("eval 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"eval words", sh("eval git log -p"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"eval moves the directory", sh("eval 'cd sql'; rg x"), "", ""},
		{"sh -c does not", sh("sh -c 'cd sql'; rg x"), "", gate.ReasonFinding},
		{"too deep", shIn("sql", "eval eval eval eval eval eval ls"), "", gate.ReasonFinding},
		{"not too deep", shIn("sql", "eval eval eval ls"), "", ""},
		{"built at run time", shIn("sql", `bash -c "$CMD"`), "", gate.ReasonFinding},
		{"a substitution", shIn("sql", "echo $(git log -p)"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a backtick substitution", shIn("sql", "echo `cd .. && rg x`"), "", gate.ReasonFinding},
		{"script -c", shIn("sql", "script -c 'git log -p' /dev/null"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"sh running a file", shIn("sql", "sh build.sh"), "", ""},
	})
}

// TestMessagesHideNames checks that a path named by an event or a command
// is printed with each segment as a finding prints a name.
func TestMessagesHideNames(t *testing.T) {
	files := cleanFiles()
	files["pbip/Doe, Jane/definition/pages/0000/visuals/0001/visual.json"] = slicer("student", "full_name", "Doe, Jane")
	root := newRepo(t, files)
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		{"shell local-state directory", sh(`ls "pbip/Doe, Jane/.pbi"`), gate.ReasonLocalState},
		{"read local state", gate.Event{Tool: gate.Read, Path: "pbip/Doe, Jane/.pbi/localSettings.json"}, gate.ReasonLocalState},
		{"read unvettable", gate.Event{Tool: gate.Read, Path: "pbip/Doe, Jane.pbix"}, gate.ReasonUnvettable},
		{"tree scan", gate.Event{Tool: gate.Search, Path: "pbip"}, gate.ReasonFinding},
		{"write settings", gate.Event{Tool: gate.Write, Path: "Doe, Jane/.claude/settings.json"}, gate.ReasonAgentSettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := tc.ev
			ev.Cwd = root
			d := gate.Decide(ev, gate.Options{})
			check(t, d, tc.reason)
			if strings.Contains(d.Message, "Doe") || strings.Contains(d.Message, "Jane") {
				t.Errorf("the message names the directory: %s", d.Message)
			}
			if !strings.Contains(d.Message, "{?}") {
				t.Errorf("the message does not mark the hidden name: %s", d.Message)
			}
		})
	}
}

func TestShellControlFiles(t *testing.T) {
	c := newControlDirs(t)
	root := newRepo(t, cleanFiles())
	record := filepath.Join(c.store, "approved", strings.Repeat("0", 64))
	run(t, root, []decideCase{
		{"redirect into the store", sh("echo x > " + record), gate.ReasonAgentSettings},
		{"cp into the store", sh("cp x " + filepath.Join(c.store, "approved") + "/"), gate.ReasonAgentSettings},
		{"remove the store", sh("rm -rf " + c.store), gate.ReasonAgentSettings},
		{"the store through PBIPX_CONFIG_DIR's value after cd", sh("cd " + c.store + " && echo x > approved/y"), gate.ReasonAgentSettings},
		{"redirect into CLAUDE_CONFIG_DIR settings", sh("echo {} > " + filepath.Join(c.claude, "settings.json")), gate.ReasonAgentSettings},
		{"tee into home settings", sh("echo {} | tee ~/.claude/settings.local.json"), gate.ReasonAgentSettings},
		{"mv into home settings by $HOME", sh("mv x.json $HOME/.claude/settings.json"), gate.ReasonAgentSettings},
		{"cp into the home Claude directory", sh("cp settings.json ~/.claude/"), gate.ReasonAgentSettings},
		{"cp into CLAUDE_CONFIG_DIR", sh("cp x.json " + c.claude), gate.ReasonAgentSettings},
		{"cd home, then redirect", sh("cd && cd .claude && echo {} > settings.json"), gate.ReasonAgentSettings},

		{"another file in the Claude directory", sh("cat ~/.claude/keybindings.json"), ""},
		{"cp a command into the Claude directory", sh("cp x.md ~/.claude/commands/"), ""},
	})
}
