package gate_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellContinuations: bash removes a backslash-newline before it reads
// the line, next to an operator too, and in an unquoted here-document
// body. The repository has a finding under the protected root, so an
// allow is not for want of one.
func TestShellContinuations(t *testing.T) {
	root := newRepo(t, leakyFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git, unread := gate.ReasonGitContent, gate.ReasonInternalError
	run(t, root, []decideCase{
		{"&& then a continuation", sh("npm ci &&\\\nnpm test"), ""},
		{"| then a continuation", sh("git log --oneline |\\\n  head -5"), ""},
		{"2>&1 | then a continuation", sh("go test ./... 2>&1 |\\\n  tail -20"), ""},
		{"; then a continuation", sh("make build;\\\nmake test"), ""},
		{"a continuation, then &&", sh("make build \\\n&& make test"), ""},
		{"a continuation, then |", sh("git status \\\n| head"), ""},
		{"a subshell, a continuation, then &&", sh("(cd src && go test ./...)\\\n&& echo ok"), ""},
		{"a multi-line if", sh("if true; then\\\n echo a;\\\nfi"), ""},
		{"a Dockerfile written from a here-document",
			sh("cat > Dockerfile <<EOF\nFROM scratch\nRUN apt-get update && \\\n    apt-get install -y make\nEOF"), ""},
		{"a script written from a here-document", sh("cat <<EOF > run.sh\n#!/bin/sh\nmake \\\n  build\nEOF\nchmod +x run.sh"), ""},
		{"a commit message with a wrapped line",
			sh("git commit -m \"$(cat <<EOF\nA wrapped \\\nline\nEOF\n)\""), ""},
		// The join read as bash reads it.
		{"& continued into &&, then git log -p", sh("true &\\\n& git log -p"), git},
		{"$ continued into $( in arithmetic", sh("echo $(( $\\\n(rg Doe) ))"), gate.ReasonFinding},
		{"$ continued into $( in double quotes", sh("echo \"$\\\n(rg Doe)\""), gate.ReasonFinding},
		{"$ continued into $(", sh("echo $\\\n(git log -p)"), git},
		{"a name continued into a reader", sh("ca\\\nt " + leakVisual), gate.ReasonFinding},
		{"a body line continued into the delimiter, then a command", sh("cat <<E\nx \\\nE\ngit log -p\nE"), unread},
		{"a body line continued past a $(", sh("cat <<E\n$(echo \\\n)\nE"), unread},
	})
}

// TestShellSplitHeredocOperator: a here-document operator split by a
// backslash-newline, << after the join, whose quoted body bash does not
// join: the gate must not take the body's backslash-newline for a join.
// The finding is in history only; real bash runs the git log.
func TestShellSplitHeredocOperator(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	unread := gate.ReasonInternalError
	body := "\nx\\\nE\ngit log -p -1\nE"
	run(t, root, []decideCase{
		{"<\\<nl><'E'", sh("cat <\\\n<'E'" + body), unread},
		{"<\\<nl><-'E'", sh("cat <\\\n<-'E'" + body), unread},
		{"<\\<nl><\\E", sh("cat <\\\n<\\E" + body), unread},
		{"<\\<nl><\"E\"", sh("cat <\\\n<\"E\"" + body), unread},
		{"nested in bash <<'EOF'", sh("bash <<'EOF'\ncat <\\\n<'E'" + body + "\nEOF"), unread},
		{"nested in eval", sh("eval 'cat <\\\n<\"E\"" + body + "'"), unread},
		{"a join in a quoted body read as code first", sh("cat <<'E'\nx\\\nE\ngit log -p -1\nE"), gate.ReasonGitContent},
		{"a join after a single-quoted word", sh("echo 'a'\\\n && git log --oneline -1"), ""},
		{"a join in a comment", sh("true # x \\\ngit log --oneline -1"), ""},
		// The backstop fires only where the join can make one operator of
		// two characters; elsewhere a redirection after a continuation is
		// read as bash reads it.
		{"a continuation, then a redirection", sh("go test ./... \\\n> notes/out.txt"), ""},
		{"a continuation, then an append", sh("go test ./... \\\n>> notes/out.txt"), ""},
		{"a redirection, then a continuation", sh("go test ./... > \\\nnotes/out.txt"), ""},
		{"> split from >", sh("echo x >\\\n> notes/out.txt; git log -p -1"), unread},
		{"& split from >", sh("echo x &\\\n> notes/out.txt; git log -p -1"), unread},
		{"< split from (", sh("cat <\\\n(git log -p -1)"), unread},
	})
}

// TestShellFedScriptJoins: the script echo or printf feeds a shell is what
// it prints, its backslash-newlines joined as bash joins them.
func TestShellFedScriptJoins(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"printf of a continued line into bash", sh(`printf '%s\n' 'git log \' '-p -1' | bash`), git},
		{"printf with an escaped newline into sh", sh(`printf 'git log \\\n-p -1\n' | sh`), git},
		{"echo -e of a continued line into bash", sh(`echo -e 'git log \\\n-p' | bash`), git},
		{"printf of a plain line into bash", sh(`printf '%s\n' 'git log --oneline -1' | bash`), ""},
		{"printf with %d into bash", sh(`printf '%d\n' 1 | bash`), git},
		// echo -e and %b keep \' and \" as written; only printf's format
		// reads them. Any escape the gate does not read exactly is a
		// script built at run time.
		{`echo -e with \" into bash`, sh(`echo -e 'echo \"; git log -p; echo \"' | bash`), git},
		{`echo -e with \' into bash`, sh(`echo -e "echo \\' ; git log -p ; echo \\'" | bash`), git},
		{`printf %b with \" into bash`, sh(`printf '%b\n' 'echo \"; git log -p; echo \"' | bash`), git},
		{`echo -e with \x into bash`, sh(`echo -e 'git log \x2dp' | bash`), git},
		{`echo -e with \0NNN into bash`, sh(`echo -e 'git log \055p' | bash`), git},
		{`printf format with \x into bash`, sh(`printf 'git log \x2dp\n' | bash`), git},
		{`printf %b with \0NNN into bash`, sh(`printf '%b\n' 'git log -\0160' | bash`), git},
		{`echo -ne with \x into bash`, sh(`echo -ne 'git log -\x70' | bash`), git},
		{`printf format with \" into bash`, sh(`printf 'echo \"hi\"\n' | bash`), ""},
	})
}

// TestShellEchoShells: whose echo prints the text a shell is fed matters.
// bash's echo reads escapes only with -e (or with xpg_echo set); the
// shell the Bash tool runs may be zsh, and sh, dash, zsh and ksh read
// escapes without -e. Where the shell running echo is not known to be
// bash, a backslash in echo's text with no -e or -E leaves the text
// untold.
func TestShellEchoShells(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		// A top-level echo: the shell may be zsh.
		{"top-level echo with an escape, fed to bash", sh(`echo 'git log \055p' | bash`), git},
		{"top-level echo with an escape and -e, fed to bash", sh(`echo -e 'git log \055p' | bash`), git},
		{"top-level echo with an escape and -E, fed to bash", sh(`echo -E 'x\ngit log -p' | sh`), git},
		{"-E in bash -c, an escape, fed to bash", sh(`bash -c "echo -E 'git log --oneline \\055p' | bash"`), ""},
		{"top-level echo with no backslash", sh(`echo 'git log --oneline -1' | bash`), ""},
		{"top-level echo -n with no backslash", sh(`echo -n 'git log --oneline -1' | sh`), ""},
		// A nested line: the shell running echo is the one -c names.
		{"bash -c echo with an escape, fed to bash", sh(`bash -c "echo 'git log \\055p' | bash"`), ""},
		{"sh -c echo with an escape, fed to bash", sh(`sh -c "echo 'git log \\055p' | bash"`), git},
		{"dash -c echo with an escape", sh(`dash -c "echo 'git log \\055p' | sh"`), git},
		{"zsh -c echo with an escape", sh(`zsh -c "echo 'git log \\055p' | sh"`), git},
		{"ksh -c echo with an escape", sh(`ksh -c "echo 'git log \\055p' | sh"`), git},
		{"sh -c echo with no backslash", sh(`sh -c "echo 'git log --oneline -1' | sh"`), ""},
		// xpg_echo makes bash's echo read escapes without -e.
		{"shopt -s xpg_echo, then echo with an escape", sh(`shopt -s xpg_echo; echo 'git log \055p' | bash`), git},
		{"bash -O xpg_echo -c echo with an escape", sh(`bash -O xpg_echo -c "echo 'git log \\055p' | bash"`), git},
		{"set -o posix alone changes nothing here", sh(`bash -c "set -o posix; echo 'git log --oneline -1' | bash"`), ""},
	})
}

// TestShellUnreadableMessages: each construct the gate cannot read as bash
// would is named in its own fixed words.
func TestShellUnreadableMessages(t *testing.T) {
	root := newRepo(t, cleanFiles())
	for _, tc := range []struct{ line, want string }{
		{"echo x )", "a ) closes nothing"},
		{"cat <<$X\nx\n$X", "delimiter holds"},
		{"cat <<E\nx \\\nE\nE", "unquoted here-document body"},
		{"case x in", "leaves a case"},
		{"x=$(cat <<'E'\nmsg\nE )\ntrue\nE\n)", "versions of bash"},
	} {
		d := gate.Decide(gate.Event{Tool: gate.Shell, Command: tc.line, Cwd: filepath.Clean(root)}, gate.Options{})
		if d.Allow || d.Reason != gate.ReasonInternalError || !strings.Contains(d.Message, tc.want) {
			t.Errorf("%q: got %v %q %q, want internal-error naming %q", tc.line, d.Allow, d.Reason, d.Message, tc.want)
		}
	}
}
