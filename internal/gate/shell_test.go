package gate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
		{"cd to an absolute path outside", sh("cd " + bashPath(home) + " && rg x"), "", ""},
	})

	root := nestedRepo(t)
	run(t, root, []decideCase{
		{"cd into the parent of a root, then read", sh("cd work && cat pbip/B.Report/" + leakInReport), gate.ReasonFinding},
		{"cd through a directory, then read", sh("cd work/other && cat ../pbip/B.Report/" + leakInReport), gate.ReasonFinding},
		{"cd beside the root, then read there", sh("cd work/other && cat readme.md"), ""},
	})

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "") // the home directory on Windows when HOME is unset
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
		{"the root by a relative name", shIn("work", "wc -c pbip"), gate.ReasonFinding},
		{"a directory that contains the root", sh("wc -c work"), gate.ReasonFinding},
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
		// On Windows, resolving a path spells each existing component as it
		// is on disk, so the other case is local state either way.
		want, shellWant := "", gate.ReasonFinding
		if fold || runtime.GOOS == "windows" {
			want, shellWant = gate.ReasonLocalState, gate.ReasonLocalState
		}
		run(t, root, []decideCase{
			{"read local state in another case", gate.Event{Tool: gate.Read, Path: upper}, want},
			{"shell local-state directory in another case", sh("wc -c pbip/A.Report/.PBI/"), shellWant},
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
		{"a .claude directory copied in", sh("cp -r " + bashPath(filepath.Join(outside, ".claude")) + " ."), gate.ReasonAgentSettings},
		{"dd into .claude", sh("dd if=x.json of=.claude/"), gate.ReasonAgentSettings},
		{"in another case", sh("cp x.json .CLAUDE/"), gate.ReasonAgentSettings},
		{"settings by **", sh("sed -i s/a/b/ **/settings.local.json"), gate.ReasonAgentSettings},

		{"cp into a subdirectory of .claude", sh("cp x.md .claude/commands/"), ""},
		{"cp out of .claude", sh("cp .claude/commands/x.md " + bashPath(outside)), ""},
		{"cp into .claude outside the repository", sh("cp x.json " + bashPath(filepath.Join(outside, ".claude"))), ""},
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
		{"redirect into the store", sh("echo x > " + bashPath(record)), gate.ReasonAgentSettings},
		{"cp into the store", sh("cp x " + bashPath(filepath.Join(c.store, "approved")) + "/"), gate.ReasonAgentSettings},
		{"remove the store", sh("rm -rf " + bashPath(c.store)), gate.ReasonAgentSettings},
		{"the store through PBIPX_CONFIG_DIR's value after cd", sh("cd " + bashPath(c.store) + " && echo x > approved/y"), gate.ReasonAgentSettings},
		{"redirect into CLAUDE_CONFIG_DIR settings", sh("echo {} > " + bashPath(filepath.Join(c.claude, "settings.json"))), gate.ReasonAgentSettings},
		{"tee into home settings", sh("echo {} | tee ~/.claude/settings.local.json"), gate.ReasonAgentSettings},
		{"mv into home settings by $HOME", sh("mv x.json $HOME/.claude/settings.json"), gate.ReasonAgentSettings},
		{"cp into the home Claude directory", sh("cp settings.json ~/.claude/"), gate.ReasonAgentSettings},
		{"cp into CLAUDE_CONFIG_DIR", sh("cp x.json " + bashPath(c.claude)), gate.ReasonAgentSettings},
		{"cd home, then redirect", sh("cd && cd .claude && echo {} > settings.json"), gate.ReasonAgentSettings},

		{"another file in the Claude directory", sh("cat ~/.claude/keybindings.json"), ""},
		{"cp a command into the Claude directory", sh("cp x.md ~/.claude/commands/"), ""},
	})
}

func TestShellHeredocs(t *testing.T) {
	runBoth(t, []both{
		{"an apostrophe in a body, then a search",
			sh("cat > notes/x.md <<'EOF'\nIt's done\nEOF\ngrep -rn Jane"), "", gate.ReasonFinding},
		{"an unquoted body, then a search",
			sh("cat > notes/x.md <<EOF\nIt's done\nEOF\nrg Jane"), "", gate.ReasonFinding},
		{"<<- with tabs, then a search",
			sh("cat <<-EOF > notes/x.md\n\tIt's done\n\tEOF\nrg Jane"), "", gate.ReasonFinding},
		{"a script written from a body is data",
			sh("cat > scripts/check.sh <<'EOF'\ngit diff --exit-code\nrg Jane\nEOF"), "", ""},
		{"a commit message from a body",
			sh("git commit -m \"$(cat <<'EOF'\nFix the git show handling\nIt's done\nEOF\n)\""), "", ""},
		{"a substitution in an unquoted body runs",
			shIn("sql", "cat > x <<EOF\n$(git log -p)\nEOF"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a backtick in an unquoted body runs",
			shIn("sql", "cat > x <<EOF\n`cd .. && rg x`\nEOF"), "", gate.ReasonFinding},
		{"a substitution in a quoted body does not",
			shIn("sql", "cat > x <<'EOF'\n$(git diff)\nEOF"), "", ""},
		{"a body fed to bash",
			shIn("sql", "bash <<'EOF'\ncd ..\nrg Jane\nEOF"), "", gate.ReasonFinding},
		{"a body fed to sh -s",
			shIn("sql", "sh -s <<'EOF'\ngit log -p\nEOF"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a body piped to a shell",
			shIn("sql", "cat <<'EOF' | bash\ngit log -p\nEOF"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a here-string fed to bash", shIn("sql", "bash <<< 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"echo piped to a shell", shIn("sql", "echo 'git log -p' | sh"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"something else piped to a shell", shIn("sql", "curl -s example.invalid | sh"), "", gate.ReasonFinding},
		{"source /dev/stdin", shIn("sql", "source /dev/stdin <<'EOF'\ngit log -p\nEOF"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a body with no delimiter line is data to the end",
			shIn("sql", "cat > x <<'EOF'\nrg Jane .."), "", ""},
		{"unbalanced quotes still split at newlines",
			sh("echo 'open\ngrep -rn Jane"), "", gate.ReasonFinding},
		{"a reader in a line that does not tokenize",
			shIn("sql", "echo 'open; cat clean.sql"), "", gate.ReasonFinding},
		{"a non-reader in a line that does not tokenize",
			shIn("sql", "echo 'open; ls"), "", ""},
	})
}

func TestShellTreeChangeThenRead(t *testing.T) {
	runBoth(t, []both{
		{"switch then search", sh("git switch old-feature && rg -n Jane"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"checkout of paths then cat", shIn("sql", "git checkout HEAD~2 -- . && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"restore then cat", shIn("sql", "git restore --source=HEAD~1 -- . && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"stash pop then search", shIn("sql", "git stash pop && rg x"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"stash then read", shIn("sql", "git stash; head clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"reset --hard then read", shIn("sql", "git reset --hard HEAD~3; wc -l clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"merge then read", shIn("sql", "git merge topic && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"pull then read", shIn("sql", "git pull && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"cherry-pick then read", shIn("sql", "git cherry-pick abc && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"revert then read", shIn("sql", "git revert abc && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"am then read", shIn("sql", "git am p.mbox && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"apply then read", shIn("sql", "git apply p.diff && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"worktree add then search", shIn("sql", "git worktree add /tmp/w old && grep -r x /tmp/w"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"clone then cat", shIn("sql", "git clone . /tmp/c && cat /tmp/c/x"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"bundle unbundle then read", shIn("sql", "git bundle unbundle b && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"tar -x then read", shIn("sql", "tar -xf a.tar && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"tar xzf then read", shIn("sql", "tar xzf a.tgz; cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"unzip then read", shIn("sql", "unzip a.zip && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a read nested after the change", shIn("sql", "git checkout old && bash -c 'cat clean.sql'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"an alias", shIn("sql", "git co old && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"checkout of paths then cat under the root",
			sh("git checkout HEAD~2 -- . && cat pbip/A.Report/definition/report.json"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"stash pop then a search of .", sh("git stash pop && grep -r x ."), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a word naming the root", shIn("sql", "git switch old && ls ../pbip"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"an option value naming the root", shIn("sql", "git pull && ./tool --input=../pbip/x"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a word naming local state", sh("git pull && ./tool pbip/A.Report/.pbi/localSettings.json"),
			gate.ReasonGitContent, gate.ReasonGitContent},
		{"an unknown word", shIn("sql", "git pull && ./tool $F"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"an input redirection from the root", shIn("sql", "git pull && ./tool < ../pbip/x"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"the repository root after the change", sh("git switch old && ./tool ."), gate.ReasonGitContent, gate.ReasonGitContent},
		{"switch -c to another start point", shIn("sql", "git switch -c new no-such-rev && cat clean.sql"),
			gate.ReasonGitContent, gate.ReasonGitContent},
		{"switch --orphan", shIn("sql", "git switch --orphan new && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"checkout -b with paths", shIn("sql", "git checkout -b new -- . && cat clean.sql"), gate.ReasonGitContent, gate.ReasonGitContent},

		{"a script after the change", shIn("sql", "git checkout old && ./build.sh"), "", ""},
		{"go test after checkout", sh("git checkout main && go test ./..."), "", ""},
		{"make after switch", sh("git switch feature && make build"), "", ""},
		{"npm install after pull", sh("git pull && npm install"), "", ""},
		{"an output file after the change", sh("git pull && ./tool -o out.txt > log.txt"), "", ""},
		{"checkout -b", shIn("sql", "git checkout -b new && cat clean.sql"), "", ""},
		{"checkout -B", shIn("sql", "git checkout -B new && cat clean.sql"), "", ""},
		{"switch -c", shIn("sql", "git switch -c new && cat clean.sql"), "", ""},
		{"switch -C from HEAD", shIn("sql", "git switch -C new HEAD && cat clean.sql"), "", ""},
		{"checkout -b from HEAD's commit by name", shIn("sql", "git checkout -b new main && cat clean.sql"), "", ""},

		{"the change alone", shIn("sql", "git switch old-feature"), "", ""},
		{"a read before the change", shIn("sql", "cat clean.sql && git switch old"), "", ""},
		{"git after the change", shIn("sql", "git checkout main && git pull --rebase origin main && git log --oneline"), "", ""},
		{"echo after the change", shIn("sql", "git switch main && echo done"), "", ""},
		{"stash list then read", shIn("sql", "git stash list && cat clean.sql"), "", ""},
		{"reset of the index then read", shIn("sql", "git reset HEAD~1 && cat clean.sql"), "", ""},
		{"restore --staged then read", shIn("sql", "git restore --staged x && cat clean.sql"), "", ""},
		{"apply --check then read", shIn("sql", "git apply --check p.diff && cat clean.sql"), "", ""},
		{"tar -c then read", shIn("sql", "tar -cf /tmp/a.tar . && cat clean.sql"), "", ""},

		// A command substitution in the change's own words, or in its
		// here-documents, runs before the change.
		{"merge with a message from a here-document",
			shIn("sql", "git merge --no-ff -m \"$(cat <<'EOF'\nMerge topic\nEOF\n)\" main"), "", ""},
		{"stash push with a message from a here-document",
			shIn("sql", "git stash push -m \"$(cat <<'EOF'\nWork in progress\nEOF\n)\""), "", ""},
		{"a read in the change's own substitution", shIn("sql", `git merge -m "$(cat clean.sql)" topic`), "", ""},
		{"a read in the change's own backticks", shIn("sql", "git merge -m \"`head -1 clean.sql`\" topic"), "", ""},
		{"a read in the change's own here-document", shIn("sql", "git am <<EOF\n$(cat clean.sql)\nEOF"), "", ""},
		// Anything after the change still counts.
		{"a here-document message, then a read",
			shIn("sql", "git merge -m \"$(cat <<'EOF'\nMerge topic\nEOF\n)\" main && cat clean.sql"),
			gate.ReasonGitContent, gate.ReasonGitContent},
		{"a substitution in a later command", shIn("sql", `git merge topic && echo "$(cat clean.sql)"`),
			gate.ReasonGitContent, gate.ReasonGitContent},
		{"a process substitution of the change", shIn("sql", "git merge topic <(cat clean.sql)"),
			gate.ReasonGitContent, gate.ReasonGitContent},
		{"a process substitution of the change, output", shIn("sql", "git merge topic >(cat clean.sql)"),
			gate.ReasonGitContent, gate.ReasonGitContent},
		{"a change inside the substitution, then a read", shIn("sql", `git commit -m "$(git checkout old; cat clean.sql)"`),
			gate.ReasonGitContent, gate.ReasonGitContent},
		{"a change in a substitution, then the command reads", shIn("sql", `cat clean.sql "$(git checkout old)"`),
			gate.ReasonGitContent, gate.ReasonGitContent},
	})
}

func TestShellFdAndSearchers(t *testing.T) {
	runBoth(t, []both{
		{"fd -x", sh("fd -e json -x cat"), "", gate.ReasonFinding},
		{"fd --exec-batch", sh("fd . --exec-batch wc -l"), "", gate.ReasonFinding},
		{"fd -X under notes", sh("fd -X cat ';' md notes"), "", ""},
		{"fd -x under a root", shIn("sql", "fd json ../pbip -x cat"), "", gate.ReasonFinding},
		{"fd with a search path", shIn("sql", "fd --search-path ../pbip -x cat"), "", gate.ReasonFinding},
		{"fd -x running git", shIn("sql", "fd -x git log -p"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"fd lists names", sh("fd json"), "", ""},
		{"fd piped to xargs", sh("fd json | xargs cat"), "", gate.ReasonFinding},
		{"fdfind -x", sh("fdfind -x cat"), "", gate.ReasonFinding},
		{"ugrep", sh("ugrep -n Jane"), "", gate.ReasonFinding},
		{"ug", sh("ug Jane"), "", gate.ReasonFinding},
		{"rga", sh("rga Jane"), "", gate.ReasonFinding},
		{"sift", sh("sift Jane"), "", gate.ReasonFinding},
		{"pt", sh("pt Jane"), "", gate.ReasonFinding},
		{"ugrep away", shIn("sql", "ugrep -n Jane"), "", ""},
	})
}

func TestShellWrappersAndRemote(t *testing.T) {
	runBoth(t, []both{
		{"unbuffer", sh("unbuffer rg Jane"), "", gate.ReasonFinding},
		{"taskset", sh("taskset -c 1 rg Jane"), "", gate.ReasonFinding},
		{"flock", sh("flock /tmp/l rg Jane"), "", gate.ReasonFinding},
		{"flock -c", shIn("sql", "flock /tmp/l -c 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"coproc", sh("coproc rg Jane"), "", gate.ReasonFinding},
		{"watch", shIn("sql", "watch -n 5 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"env -S", shIn("sql", "env -S 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"parallel :::", shIn("sql", "parallel ::: 'git log -p' ls"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"parallel template", shIn("sql", "parallel cat ::: ../pbip/B.Report/"+leakInReport), "", gate.ReasonFinding},
		{"chronic", sh("chronic rg Jane"), "", gate.ReasonFinding},
		{"ionice", sh("ionice -c 3 rg Jane"), "", gate.ReasonFinding},
		{"nice -n", sh("nice -n 10 rg Jane"), "", gate.ReasonFinding},
		{"stdbuf", sh("stdbuf -oL rg Jane"), "", gate.ReasonFinding},
		{"setsid", sh("setsid rg Jane"), "", gate.ReasonFinding},
		{"caffeinate", sh("caffeinate -i rg Jane"), "", gate.ReasonFinding},
		{"xvfb-run", sh("xvfb-run -a rg Jane"), "", gate.ReasonFinding},
		{"timeout with a long option", sh("timeout --signal KILL 5 rg Jane"), "", gate.ReasonFinding},
		{"an unknown wrapper", sh("mywrap -q rg Jane"), "", gate.ReasonFinding},
		{"bash -c --", shIn("sql", "bash -c -- 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"bash --rcfile", shIn("sql", "bash --rcfile /dev/null -c 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"busybox sh -c", shIn("sql", "busybox sh -c 'git log -p'"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"pwsh -c", shIn("sql", "pwsh -c 'Get-Content x'"), "", gate.ReasonFinding},
		{"powershell -Command", shIn("sql", "powershell -Command 'gc x'"), "", gate.ReasonFinding},
		{"su -c", shIn("sql", "su -c 'cat x' nobody"), "", gate.ReasonFinding},
		{"ssh with a command", shIn("sql", "ssh localhost 'cat x'"), "", gate.ReasonFinding},

		{"pwsh version", shIn("sql", "pwsh --version"), "", ""},
		{"ssh alone", shIn("sql", "ssh -p 22 localhost"), "", ""},
		{"which", sh("which rg"), "", ""},
		{"command -v", sh("command -v rg"), "", ""},
		{"echo git diff", shIn("sql", "echo git diff"), "", ""},
		{"man", sh("man grep"), "", ""},
	})
}

func TestShellMessageNamesOnlyGitSubcommands(t *testing.T) {
	root := newRepo(t, cleanFiles())
	// An alias whose name could be a value; a shell alias is denied.
	cmd := exec.Command("git", "config", "alias.Doe-Jane-alias", "!cat x")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ cmd, want string }{
		{"git log -p", "git log "},
		{"git Doe-Jane-alias", "git runs a shell alias"},
	} {
		d := gate.Decide(shIn(root, tc.cmd), gate.Options{})
		check(t, d, gate.ReasonGitContent)
		if strings.Contains(d.Message, "Doe") || !strings.Contains(d.Message, tc.want) {
			t.Errorf("%s: message %q", tc.cmd, d.Message)
		}
	}
	d := gate.Decide(shIn(root, "git Doe-Jane-alias old && cat x"), gate.Options{})
	if strings.Contains(d.Message, "Doe") {
		t.Errorf("the message names the subcommand: %q", d.Message)
	}
}

// TestShellBounded checks that adversarial command lines decide quickly:
// the hook's own time limit lets a call through.
func TestShellBounded(t *testing.T) {
	root := newRepo(t, leakyFiles())
	for name, cmd := range map[string]string{
		"many words":           "cat " + strings.Repeat("a ", 512<<10),
		"many commands":        strings.Repeat("ls x;", 200<<10),
		"one long word":        "cat " + strings.Repeat("a", 1<<20),
		"nested substitutions": strings.Repeat("$(", 300<<10) + strings.Repeat(")", 200<<10),
		"many heredocs":        strings.Repeat("cat <<E\nx\nE\n", 80<<10),
		"many globs":           strings.Repeat("ls */*/* ", 100<<10),
		"deep **":              "cat " + strings.Repeat("**/", 2000) + "x",
		"many **":              "cat " + strings.Repeat("**/*/", 30) + "x " + strings.Repeat("pbip/**/*/**/x ", 1000),
		"unbalanced":           "'" + strings.Repeat("cat x; ", 140<<10),
	} {
		t.Run(name, func(t *testing.T) {
			if len(cmd) > 1<<20+16 {
				cmd = cmd[:1<<20]
			}
			start := time.Now()
			d := gate.Decide(shIn(root, cmd), gate.Options{})
			if el := time.Since(start); el > slowdown*2*time.Second {
				t.Errorf("took %v", el)
			}
			if name == "many commands" {
				check(t, d, gate.ReasonInternalError)
			}
		})
	}
	// Up to the limit, every command is judged.
	ok := strings.Repeat("ls notes;", 19000) + "git log -p"
	check(t, gate.Decide(shIn(root, ok), gate.Options{}), gate.ReasonGitContent)
}

// TestShellExeNames: a program is known by its name without ".exe", as
// Windows, Git Bash and WSL run it; its case is folded only where the
// file system folds case.
func TestShellExeNames(t *testing.T) {
	isolateSearch(t)
	runBoth(t, []both{
		{"rg.exe", sh("rg.exe Jane"), "", gate.ReasonFinding},
		{"rg.exe by its path", sh("/usr/bin/rg.exe Jane"), "", gate.ReasonFinding},
		{"rg.EXE", sh("rg.EXE Jane"), "", gate.ReasonFinding},
		{"grep.exe -r", sh("grep.exe -r Jane"), "", gate.ReasonFinding},
		{"cat.exe of a leak", sh("cat.exe pbip/B.Report/" + leakInReport), "", gate.ReasonFinding},
		{"bash.exe -c", sh(`bash.exe -c "rg Jane"`), "", gate.ReasonFinding},
		{"git.exe log -p", sh("git.exe log -p"), gate.ReasonGitContent, gate.ReasonGitContent},
		{"a name that only ends in exe", sh("rgexe Jane"), "", ""},
		{"a bare .exe", sh(".exe Jane"), "", ""},
	})
	root, _ := historyRepo(t)
	run(t, root, []decideCase{
		{"git.exe show of the leaky commit", sh("git.exe show HEAD~1"), gate.ReasonGitContent},
		{"git.exe show --stat", sh("git.exe show --stat HEAD~1"), ""},
	})
	leaky := newRepo(t, leakyFiles())
	for _, fold := range []bool{true, false} {
		restore := gate.SetFoldCase(fold)
		// Where names are told apart by case, RG is not rg.
		want, gitWant := "", ""
		if fold {
			want, gitWant = gate.ReasonFinding, gate.ReasonGitContent
		}
		run(t, leaky, []decideCase{
			{"RG.EXE", sh("RG.EXE Jane"), want},
			{"Rg", sh("Rg Jane"), want},
		})
		run(t, root, []decideCase{{"GIT.EXE log -p", sh("GIT.EXE log -p"), gitWant}})
		restore()
	}
}

// TestShellGitMessageText: a protected root's name in git's message text
// (-m, --message, or a here-document read by -F -) is prose, not a path,
// and is left out of the root-name tests. Every other word of the line,
// and text a program reads as code, is tested as before.
func TestShellGitMessageText(t *testing.T) {
	isolateSearch(t)
	runBoth(t, []both{
		{"-m of a here-document naming the root",
			sh("git commit -m \"$(cat <<'EOF'\nNote a check\n\nThis leaves pbip/ alone.\nEOF\n)\""), "", ""},
		{"-m of an unquoted here-document naming the root",
			sh("git commit -m \"$(cat <<EOF\nNote a check\n\nThis leaves pbip/ alone.\nEOF\n)\""), "", ""},
		{"-F - of a here-document naming the root",
			sh("git commit -F - <<'EOF'\nNote a check\n\nThis leaves pbip/ alone.\nEOF"), "", ""},
		{"--file=- of a here-document naming the root",
			sh("git commit --file=- <<'EOF'\nLeave pbip alone\nEOF"), "", ""},
		{"--file - of a here-document naming the root",
			sh("git commit -a --file - <<'EOF'\nLeave pbip alone\nEOF"), "", ""},
		{"-aF- of a here-document naming the root",
			sh("git commit -aF- <<'EOF'\nLeave pbip alone\nEOF"), "", ""},
		{"-F - of an unquoted here-document running a cat of the root",
			sh("git commit -F - <<EOF\n$(cat pbip/x)\nEOF"), "", gate.ReasonFinding},
		{"-m naming the root", sh(`git commit -m "Leave pbip alone"`), "", ""},
		{"-m of the root's path", sh(`git commit -m pbip/`), "", ""},
		{"-am naming the root", sh(`git commit -am "Leave pbip alone"`), "", ""},
		{"-m attached naming the root", sh(`git commit "-mLeave pbip alone"`), "", ""},
		{"--message= naming the root", sh(`git commit --message="Leave pbip alone"`), "", ""},
		{"merge -m naming the root", sh(`git merge -m "Merge the pbip change" topic`), "", ""},
		{"tag -m naming the root", sh(`git tag -a v1 -m "pbip release"`), "", ""},

		// The rest of the line is tested as before.
		{"a pathspec naming the root", sh(`git commit -m "Leave pbip alone" -- pbip`), "", gate.ReasonFinding},
		{"-Fm: m is the file, the next word a pathspec", sh(`git commit -Fm pbip`), "", gate.ReasonFinding},
		{"a log -m naming the root", sh(`git log -m pbip`), "", gate.ReasonFinding},
		{"a cat of the root in the message", sh(`git commit -m "$(cat pbip/x)"`), "", gate.ReasonFinding},
		{"a cat of the root in the message's here-document",
			sh("git commit -m \"$(cat <<EOF\n$(cat pbip/x)\nEOF\n)\""), "", gate.ReasonFinding},
		{"a here-document read by another program in the message",
			sh("git commit -m \"$(python3 <<'EOF'\nopen('pbip/x')\nEOF\n)\""), "", gate.ReasonFinding},
		{"git under xargs, reading a here-document", sh("xargs git commit -F - <<'EOF'\npbip\nEOF"), "", gate.ReasonFinding},
		{"git under a wrapper, a message naming the root", sh(`nice git commit -m "Leave pbip alone"`), "", gate.ReasonFinding},
		{"a here-document to git without -F -", sh("git commit -F msg.txt <<'EOF'\npbip\nEOF"), "", gate.ReasonFinding},
		{"another command after the commit", sh(`git commit -m "Leave pbip alone" && cat pbip/x`), "", gate.ReasonFinding},
		{"python3 -c naming the root", sh(`python3 -c "open('pbip/x')"`), "", gate.ReasonFinding},
		{"python3 reading a here-document naming the root", sh("python3 - <<'EOF'\nopen('pbip/x')\nEOF"), "", gate.ReasonFinding},
		{"a here-document piped to an interpreter", sh("cat <<'EOF' | python3\nopen('pbip/x')\nEOF"), "", gate.ReasonFinding},
		// A known over-block: echo and printf can feed an interpreter.
		{"echo naming the root", sh("echo pbip"), "", gate.ReasonFinding},
		{"printf naming the root", sh("printf '%s\\n' pbip"), "", gate.ReasonFinding},
	})
}
