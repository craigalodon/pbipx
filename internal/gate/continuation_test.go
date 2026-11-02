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
		{"$ continued into $(", sh("echo $\\\n(git log -p)"), git},
		{"a name continued into a reader", sh("ca\\\nt " + leakVisual), gate.ReasonFinding},
		{"a body line continued into the delimiter, then a command", sh("cat <<E\nx \\\nE\ngit log -p\nE"), unread},
		{"a body line continued past a $(", sh("cat <<E\n$(echo \\\n)\nE"), unread},
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
