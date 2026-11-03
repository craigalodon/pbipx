package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellWorktreeAfterWrite: a command that may write files (a formatter,
// a build, a script), then a git command that prints the working tree:
// git was judged against the tree as it was before, so the protected tree
// is scanned as for Grep (F1). The finding is committed and in the
// working tree: git diff alone prints nothing.
func TestShellWorktreeAfterWrite(t *testing.T) {
	leaky := newRepo(t, leakyFiles())
	clean := newRepo(t, cleanFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	finding := gate.ReasonFinding
	lines := []struct {
		name, line string
		leaky      string // the reason in the leaky tree; the clean tree allows
	}{
		{"make, then diff", "make fmt && git diff", finding},
		{"npm run, then diff HEAD", "npm run format && git diff HEAD", finding},
		{"a script, then diff of a root", "./scripts/gen.sh; git diff -- pbip/", finding},
		{"pre-commit run, then diff", "pre-commit run --all-files; git diff", finding},
		{"a formatter, then status -vv", "prettier --write . && git status -v -v", finding},
		{"a build, then add -p of a path outside the roots", "go generate ./... && git add -p -- README.md", ""},
		{"a redirection, then diff", "echo x > notes/x.md && git diff", finding},
		{"a write in an assignment's substitution", `x=$(make fmt) && git diff`, finding},
		{"a loop, then diff", "for i in 1; do git diff; done; make fmt", finding},
		{"diff alone", "git diff", ""},
		{"diff --cached after make", "make fmt && git diff --cached", ""},
		{"log -p of a path outside the roots after make", "make && git log -p -- README.md", ""},
		{"readers, then diff", "ls -la && cat README.md && git status && git diff", ""},
		{"diff, then make", "git diff && make fmt", ""},
		{"add, commit and push", `git add -A && git commit -m "x" && git push`, ""},
	}
	for _, l := range lines {
		run(t, leaky, []decideCase{{l.name + ", leaky", sh(l.line), l.leaky}})
		run(t, clean, []decideCase{{l.name + ", clean", sh(l.line), ""}})
	}
}
