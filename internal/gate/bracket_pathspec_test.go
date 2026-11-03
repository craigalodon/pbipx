package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellBracketPathspec: [ is a glob character in a git pathspec as *
// and ? are, so a pathspec holding one is not provably outside a root; the
// name-only run decides, as git does.
func TestShellBracketPathspec(t *testing.T) {
	hist, _ := historyRepo(t)
	files := leakyFiles()
	files["src/main.go"] = "package main\n"
	leaky := newRepo(t, files)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git, finding := gate.ReasonGitContent, gate.ReasonFinding
	run(t, hist, []decideCase{
		{"bracket matching the root's files", sh("git diff HEAD~1 -- 'pb[i]p/*'"), git},
		{"bracket and star", sh("git diff HEAD~1 -- 'pb[i]p*'"), git},
		{"log -p with a bracket and star", sh("git log -p -- 'pb[i]p*'"), git},
		{"bracket matching the root only", sh("git diff HEAD~1 -- 'pb[i]p'"), ""},
		{"bracket far from the root", sh("git diff HEAD~1 -- 'READM[E].md'"), ""},
	})
	run(t, leaky, []decideCase{
		{"a writer, then diff of a bracket pathspec", sh("make fmt && git diff -- 'pb[i]p/*'"), finding},
		{"a writer, then diff of a bracket pathspec for src", sh("make fmt && git diff -- 'sr[c]'"), finding},
		{"a writer, then diff of src", sh("make fmt && git diff -- src"), ""},
	})
}
