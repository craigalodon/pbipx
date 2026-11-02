package gate_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestPowerShellGitMessages: a git refusal names what triggered it, in
// the Bash rule's words, and ends with a hint.
func TestPowerShellGitMessages(t *testing.T) {
	root, _ := historyRepo(t)
	ps := func(cmd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd} }
	git := gate.ReasonGitContent
	runMessages(t, root, []messageCase{
		{"log -p", ps("git log -p"), git, []string{"git log", "would print content from"}, hintGit},
		{"a variable", ps("git log $rev"), git, []string{"git log", "before --"}, hintGitExpand},
		{"Invoke-Expression", ps("iex $cmd"), git, []string{"Invoke-Expression"},
			"Run git directly, with its arguments written out in full."},
		{"a change, then a read", ps("git checkout HEAD~1; Get-Date"), git, []string{"git checkout"},
			"Run the change on its own, then the read as a separate call."},
	})
}

// TestPowerShellBounded checks that adversarial PowerShell lines decide
// quickly, and deny when they are too large to read.
func TestPowerShellBounded(t *testing.T) {
	root, _ := historyRepo(t)
	for name, tc := range map[string]struct {
		cmd    string
		reason string
	}{
		"deep groups":        {strings.Repeat("(", 300<<10) + strings.Repeat(")", 300<<10), gate.ReasonInternalError},
		"deep subexpression": {"git log " + strings.Repeat("$(", 300<<10), gate.ReasonInternalError},
		"many words":         {"Get-Date " + strings.Repeat("a ", 100<<10), gate.ReasonInternalError},
		"many git commands":  {strings.Repeat("git status;", 2000) + "git log -p", gate.ReasonGitContent},
		"nested strings":     {strings.Repeat("iex '", 8) + "git log -p", gate.ReasonGitContent},
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			d := gate.Decide(gate.Event{Tool: gate.PowerShell, Command: tc.cmd, Cwd: root}, gate.Options{})
			if el := time.Since(start); el > slowdown*2*time.Second {
				t.Errorf("took %v", el)
			}
			check(t, d, tc.reason)
		})
	}
}

func TestPowerShell(t *testing.T) {
	ps := func(cmd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd} }
	t.Run("leaking tree", func(t *testing.T) {
		root := newRepo(t, leakyFiles())
		run(t, root, []decideCase{
			{"any command scans the tree", ps("Get-ChildItem"), gate.ReasonFinding},
			{"a read away from the roots still scans", ps("Get-Content notes\\clean.md"), gate.ReasonFinding},
			{"from a directory away from the roots", gate.Event{Tool: gate.PowerShell, Command: "Get-Date", Cwd: "notes"}, gate.ReasonFinding},
		})
	})
	t.Run("clean tree", func(t *testing.T) {
		root := newRepo(t, cleanFiles())
		run(t, root, []decideCase{
			{"a listing", ps("Get-ChildItem -Recurse pbip"), ""},
			{"a read of a clean note", ps("Get-Content notes/clean.md"), ""},
			{"names a settings file", ps("Get-Content .claude\\settings.json"), gate.ReasonAgentSettings},
			{"names a settings file, slashes", ps("Set-Content -Path:.claude/settings.local.json -Value x"), gate.ReasonAgentSettings},
			{"names a settings file, quoted", ps(`Copy-Item x '.claude/settings.json'`), gate.ReasonAgentSettings},
			{"names a .claude directory", ps("Copy-Item x .claude"), gate.ReasonAgentSettings},
			{"names local state", ps("Get-Content " + strings.ReplaceAll(localState, "/", `\`)), gate.ReasonLocalState},
			{"runs policy approve", ps("pbipx policy approve"), gate.ReasonAgentSettings},
			{"policy approve quoted in one string", ps("Write-Output 'pbipx policy approve'"), ""},
		})
	})
}

// TestPowerShellGitHistory: the working tree is clean and only history
// holds the leak, so the tree scan allows; git printing content denies.
// The PowerShell judge reads text only, so this runs on every system.
func TestPowerShellGitHistory(t *testing.T) {
	root, _ := historyRepo(t)
	ps := func(cmd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		// Printing content history holds.
		{"log -p", ps("git log -p"), git},
		{"show a revision", ps("git show HEAD~1"), git},
		{"git.exe", ps("git.exe log -p"), git},
		{"in another case", ps("GIT.EXE log -p"), git},
		{"limited to the root", ps("git log -p -- pbip"), git},
		{"diff", ps("git diff HEAD~1"), git},
		{"after a statement", ps("Get-Date; git log -p"), git},
		{"in a pipeline", ps("git log -p | Select-String Doe"), git},
		{"after &&", ps("Get-Date && git log -p"), git},
		{"assigned to a variable", ps("$out = git log -p"), git},
		{"redirected to a file", ps("git log -p > out.txt"), git},
		{"redirected, then a stream", ps("git log -p 2>&1 > out.txt"), git},
		{"stop parsing", ps("git --% log -p"), git},
		{"a line continued", ps("git log `\n -p"), git},

		// Nothing printed from a root.
		{"status", ps("git status"), ""},
		{"log, one line each", ps("git log --oneline -5"), ""},
		{"diff --stat", ps("git diff --stat HEAD~1"), ""},
		{"diff of the clean tree", ps("git diff"), ""},
		{"commit", ps(`git commit -m "msg"`), ""},
		{"commit, a message naming git", ps(`git commit -m "fix git log -p"`), ""},
		{"commit, a message from a variable", ps(`git commit -m "$msg"`), ""},
		{"log -p of a path away from the roots", ps("git log -p -- README.md"), ""},
		{"a comment", ps("git log --oneline # -p"), ""},
		{"redirected away", ps("git status > $null"), ""},
		{"Get-Command git", ps("Get-Command git"), ""},
		{"a branch, then a listing", ps("git checkout -b topic; Get-ChildItem"), ""},

		// An argument that cannot be told.
		{"a revision in a variable", ps("git log $rev"), git},
		{"a pathspec in a variable", ps("git diff -- $p"), git},
		{"a splat", ps("git @args"), git},
		{"a subexpression", ps("git log $(Get-Rev)"), git},
		{"a group", ps("git log (Get-Rev)"), git},
		{"an array", ps("git log @('-p')"), git},
		{"an escape", ps("git log `-p"), git},
		{"a variable in quotes", ps(`git log "$opt"`), git},
		{"a subcommand in a variable", ps("git $sub"), git},
		{"stop parsing, an environment variable", ps("git --% log %OPT%"), git},

		// git run indirectly.
		{"call operator", ps("& git log -p"), git},
		{"call operator, quoted", ps("& 'git' log -p"), git},
		{"call operator, a full path", ps(`& "C:\Program Files\Git\cmd\git.exe" log -p`), git},
		{"call operator, a variable", ps("& $git log -p"), git},
		{"call operator, a group", ps("& (Get-Command git) log -p"), git},
		{"call operator, status", ps("& git status"), ""},
		{"a script block", ps("ForEach-Object { git log -p }"), git},
		{"a script block in a pipeline", ps("1 | ForEach-Object { git log -p }"), git},
		{"an if block", ps("if ($true) { git log -p }"), git},
		{"a subexpression run", ps("Write-Output $(git log -p)"), git},
		{"a subexpression in quotes", ps(`Write-Output "$(git log -p)"`), git},
		{"Start-Process", ps("Start-Process git -ArgumentList 'log','-p'"), git},
		{"Start-Process, one string", ps("Start-Process -FilePath git -ArgumentList 'log -p' -NoNewWindow -Wait"), git},
		{"Start-Process, positional", ps("start git 'log -p'"), git},
		{"Start-Process, status", ps("Start-Process git -ArgumentList 'status' -Wait"), ""},
		{"Start-Process, a variable", ps("Start-Process git -ArgumentList $a"), git},
		{"Invoke-Expression", ps("iex 'git log -p'"), git},
		{"Invoke-Expression, named", ps(`Invoke-Expression -Command "git log -p"`), git},
		{"Invoke-Expression, status", ps("iex 'git status'"), ""},
		{"Invoke-Expression, a variable", ps("iex $cmd"), git},
		{"a script block from a string", ps("& ([scriptblock]::Create('git log -p'))"), git},
		{"InvokeScript", ps("$ExecutionContext.InvokeCommand.InvokeScript('git log -p')"), git},
		{"Invoke-Expression, from a pipe", ps("'git log -p' | iex"), git},
		{"cmd /c", ps("cmd /c git log -p"), git},
		{"cmd /c, quoted", ps(`cmd /c "git log -p"`), git},
		{"pwsh -Command", ps(`pwsh -NoProfile -Command "git log -p"`), git},
		{"pwsh -EncodedCommand", ps("pwsh -EncodedCommand ZwBpAHQA"), git},
		{"an alias", ps("Set-Alias g git; g log -p"), git},
		{".NET Process.Start", ps("[Diagnostics.Process]::Start('git', 'log -p')"), git},
		{"a function defined in the line", ps("function g { git log -p }; g"), git},
		{"a pager in the environment", ps("$env:GIT_PAGER = 'less'; git log --oneline"), git},
		{"a harmless pager", ps("$env:GIT_PAGER = 'cat'; git log --oneline"), ""},
		{"a location change", ps("Set-Location pbip; git log -p -- ."), git},

		// A change to the tree, then a read.
		{"checkout, then a read", ps("git checkout HEAD~1; Get-Content README.md"), git},
		{"pull, then git status", ps("git pull; git status"), ""},
		// A subexpression in git's own arguments runs before git.
		{"merge, a message from a subexpression", ps(`git merge --no-ff -m "$(Get-Content msg.txt)" main`), ""},
		{"stash push, a message from a subexpression", ps(`git stash push -m "$(Get-Content msg.txt)"`), ""},
		{"merge, a message from a subexpression, then a read",
			ps(`git merge -m "$(Get-Content msg.txt)" main; Get-Content README.md`), git},
		{"merge, then a subexpression in a later statement", ps(`git merge main; Write-Output "$(Get-Content README.md)"`), git},
		// -m clustered after short options that take no value.
		{"commit -am, a message from a subexpression", ps(`git commit -am "$(Get-Content msg.txt)"`), ""},
		{"commit -Fm of a variable", ps(`git commit -Fm "$x"`), git},
		// An expansion outside double quotes may be an array: several
		// arguments, which could be options.
		{"commit -m of an unquoted variable", ps(`git commit -m $m`), git},
		{"commit -m of an unquoted subexpression", ps(`git commit -m $(Get-Content f)`), git},
		{"commit -m of an environment variable", ps(`git commit -m $env:MSG`), git},
		{"commit -m of a group", ps(`git commit -m (Get-Content f)`), git},
		{"commit -m of a splat", ps(`git commit -m @m`), git},
		{"commit -m with one unquoted part", ps(`git commit -m "$m"$n`), git},
		{"Start-Process, a message from a variable", ps(`Start-Process git -ArgumentList 'commit','-m',"$m"`), git},
		{"commit -m of a quoted variable", ps(`git commit -m "$m"`), ""},
		{"commit -m of a literal", ps(`git commit -m 'literal'`), ""},
		{"commit -m of a here-string", ps("git commit -m @\"\n$m\n\"@"), ""},
		{"commit -pm of a variable", ps(`git commit -pm "$x"`), git},

		// A nested line shares the session of the line around it: a
		// location change, an environment assignment or a change to the
		// tree in either counts for both.
		{"a root's path, no move", ps("git log -p -- A.Report/definition/report.json"), ""},
		{"a root's path, nested, no move", ps("iex 'git log -p -- A.Report/definition/report.json'"), ""},
		{"a move, then git", ps("Set-Location pbip; git log -p -- A.Report/definition/report.json"), git},
		{"a move, then nested git", ps("Set-Location pbip; iex 'git log -p -- A.Report/definition/report.json'"), git},
		{"a nested move, then git", ps("iex 'Set-Location pbip'; git log -p -- A.Report/definition/report.json"), git},
		{"a nested move, then nested git", ps("iex 'Set-Location pbip'; cmd /c 'git log -p -- A.Report/definition/report.json'"), git},
		{"Start-Process of a shell in another directory", ps(
			"Start-Process pwsh -WorkingDirectory pbip -ArgumentList '-c','git log -p -- A.Report/definition/report.json'"), git},
		{"a pager, then nested git", ps("$env:GIT_PAGER = 'less'; iex 'git log --oneline'"), git},
		{"a nested pager, then git", ps("iex '$env:GIT_PAGER = ''less'''; git log --oneline"), git},
		{"a harmless pager, then nested git", ps("$env:GIT_PAGER = 'cat'; iex 'git log --oneline'"), ""},
		{"a nested checkout, then a read", ps("iex 'git checkout HEAD~1'; Get-Content README.md"), git},
		{"a checkout, then a nested read", ps("git checkout HEAD~1; cmd /c 'type README.md'"), git},
		{"a nested checkout, then nested git", ps("iex 'git pull'; iex 'git status'"), git},
	})

	// A clean change in a root, not committed: the tree scan allows, and
	// git diff prints the root's content.
	write(t, root, reportJSON, gitIn(t, root, "show", "HEAD:"+reportJSON)+"\n\n")
	run(t, root, []decideCase{
		{"diff of a changed root", ps("git diff"), git},
		{"diff --stat of a changed root", ps("git diff --stat"), ""},
	})
}

func TestPowerShellAdoption(t *testing.T) {
	a := newAdoption(t)
	ps := func(cmd, cwd string) gate.Event { return gate.Event{Tool: gate.PowerShell, Command: cmd, Cwd: cwd} }
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		{"not adopted", ps("Get-ChildItem -Recurse", a.plain), ""},
		{"not adopted, read of a leak", ps("Get-Content "+leakVisual, a.plain), ""},
		{"outside every repository", ps("Get-ChildItem", a.outside), ""},
		{"adopted, leaking tree", ps("Get-ChildItem", a.adopted), gate.ReasonFinding},
		{"outside, a path into the adopted repository", ps("Get-Content '"+filepath.Join(a.adopted, "README.md")+"'", a.outside), gate.ReasonFinding},
		{"outside, a directory holding it", ps("Get-ChildItem -Recurse "+filepath.Dir(a.adopted), a.outside), gate.ReasonFinding},
		{"outside, a path that cannot be placed", ps(`Get-Content $env:USERPROFILE\x`, a.outside), gate.ReasonFinding},
		{"outside, names a user settings file", ps("Get-Content "+filepath.Join(a.ctl.claude, "settings.json"), a.outside), gate.ReasonAgentSettings},
		{"outside, names the approval store", ps("Get-ChildItem "+a.ctl.store, a.outside), gate.ReasonAgentSettings},
		{"outside, policy approve", ps("pbipx policy approve", a.outside), gate.ReasonAgentSettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check(t, a.decide(tc.ev), tc.reason)
		})
	}
}

func TestLSP(t *testing.T) {
	lsp := func(paths ...string) gate.Event { return gate.Event{Tool: gate.LSP, Paths: paths} }
	t.Run("leaking tree", func(t *testing.T) {
		root := newRepo(t, leakyFiles())
		run(t, root, []decideCase{
			{"a leaking file", lsp(leakVisual), gate.ReasonFinding},
			{"a clean note", lsp(cleanNote), ""},
			{"a clean note, then a leak", lsp(cleanNote, leakVisual), gate.ReasonFinding},
			{"local state", lsp(localState), gate.ReasonLocalState},
			{"a protected directory", lsp("pbip"), gate.ReasonFinding},
			{"no path scans the tree", lsp(), gate.ReasonFinding},
		})
	})
	t.Run("clean tree", func(t *testing.T) {
		root := newRepo(t, cleanFiles())
		run(t, root, []decideCase{
			{"no path", lsp(), ""},
			{"a report file", lsp(reportJSON), ""},
		})
	})
	t.Run("adoption", func(t *testing.T) {
		a := newAdoption(t)
		check(t, a.decide(gate.Event{Tool: gate.LSP, Cwd: a.plain}), "")
		check(t, a.decide(gate.Event{Tool: gate.LSP, Paths: []string{filepath.Join(a.adopted, leakVisual)}, Cwd: a.outside}), gate.ReasonFinding)
		check(t, a.decide(gate.Event{Tool: gate.LSP, Cwd: a.adopted}), gate.ReasonFinding)
	})
}
