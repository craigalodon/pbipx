package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellRunnerNamesAsWords: xargs and parallel are runners only as a
// wrapper or as the program. As an argument (a branch, a message, a
// pattern, a file) they are words.
func TestShellRunnerNamesAsWords(t *testing.T) {
	files := leakyFiles()
	files["xargs"] = "x\n"
	files["parallel"] = "x\n"
	leaky := newRepo(t, files)
	clean := newRepo(t, map[string]string{"README.md": "x\n", "xargs": "x\n", "parallel": "x\n"})
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	lines := []string{
		`git checkout -b parallel`,
		`git merge parallel`,
		`git switch parallel`,
		`git commit -m 'xargs'`,
		`git commit -m parallel`,
		`git log -S xargs --oneline`,
		`git diff --stat xargs`,
		`rg parallel src`,
		`grep -n xargs README.md`,
		`grep -c parallel README.md`,
		`head xargs`,
		`echo xargs parallel`,
	}
	for _, l := range lines {
		reason := ""
		// A search or read of the tree, with a finding in it, is judged as
		// it always was: rg and grep -r of "src" read nothing protected.
		run(t, clean, []decideCase{{"clean: " + l, sh(l), reason}})
		run(t, leaky, []decideCase{{"leaky: " + l, sh(l), reason}})
	}
	// A runner as a wrapper or the program is still one.
	run(t, leaky, []decideCase{
		{"xargs as the program", sh(`git ls-files | xargs cat`), gate.ReasonFinding},
		{"xargs under a wrapper", sh(`git ls-files | env A=1 xargs cat`), gate.ReasonFinding},
		{"xargs under timeout", sh(`git ls-files | timeout 5 xargs cat`), gate.ReasonFinding},
		{"parallel as the program", sh(`git ls-files | parallel cat`), gate.ReasonFinding},
		{"xargs under sudo", sh(`git ls-files | sudo xargs -I{} cat {}`), gate.ReasonFinding},
	})
}
