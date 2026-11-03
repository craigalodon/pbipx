package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellInputRedirectionsWriteNothing: an input redirection and a
// here-string read; only output redirections (and <>) write.
func TestShellInputRedirectionsWriteNothing(t *testing.T) {
	root := newRepo(t, cleanFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"an input redirection from a variable, then git status", sh(`f=README.md; cat < "$f"; git status`), ""},
		{"a here-string of git status", sh(`while IFS= read -r l; do echo "$l"; done <<< "$(git status --short)"`), ""},
		{"read from a here-string of git log", sh(`IFS= read -r x <<< "$(git log --oneline -1)"`), ""},
		{"<> to an unknown target, then git log", sh(`exec 3<> "$F"; git log --oneline -1`), git},
		{"> to an unknown target, then git log", sh(`echo x > "$F"; git log --oneline -1`), git},
	})
}

// TestShellProgramPaths: a command word whose directory cannot be told but
// whose last element is literal is that program.
func TestShellProgramPaths(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"a linter under a substituted directory", sh(`"$(go env GOPATH)/bin/golangci-lint" run ./...`), ""},
		{"a tool under a defaulted directory", sh(`${GOBIN:-$HOME/go/bin}/staticcheck ./...`), ""},
		{"git under a substituted directory, log -p", sh(`"$(dirname x)/git" log -p`), git},
		{"git under a substituted directory, log --oneline", sh(`"$(dirname x)/git" log --oneline -1`), ""},
		{"a last element with an expansion", sh(`"$(go env GOPATH)/bin/$T" run`), git},
		{"a last element with a glob", sh(`"$(go env GOPATH)"/bin/gi? log -p`), git},
		{"a quoted glob character is literal", sh(`"$(go env GOPATH)/bin/gi?" log -p`), ""},
		{"a variable the line sets", sh(`D="git log -p pbip ."; ${D}/x`), git},
		{"an unquoted substitution", sh(`$(echo git log -p pbip .)/x`), git},
		{"a shell under a substituted directory", sh(`"$(dirname x)/bash" -c 'git log -p'`), git},
		{"a wrapper under a substituted directory", sh(`"$D/env" git log -p`), git},
	})
}
