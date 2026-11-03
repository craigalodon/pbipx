package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellRunnerMore: a producer a runner feeds, piped into a shell, makes
// the fed script built at run time; runner-like programs the gate does not
// know feed every word; script, watch and the other command carriers read
// a runner's placeholder in the command they run.
func TestShellRunnerMore(t *testing.T) {
	leaky := newRepo(t, leakyFiles())
	clean := newRepo(t, cleanFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	deny := []struct{ name, line, reason string }{
		{"printf appended by xargs, piped to sh", `git ls-files | xargs printf 'cat %s\n' | sh`, gate.ReasonGitContent},
		{"echo appended by xargs, piped to sh", `git ls-files | xargs echo cat | sh`, gate.ReasonGitContent},
		{"echo -n1 appended by xargs, piped to bash", `git ls-files | xargs -n1 echo cat | bash`, gate.ReasonGitContent},
		{"echo with a placeholder, piped to sh", `git ls-files | xargs -I{} echo cat {} | sh`, gate.ReasonGitContent},
		{"echo from parallel, piped to sh", `git ls-files | parallel echo cat | sh`, gate.ReasonGitContent},
		{"rush", `git ls-files | rush 'cat {}'`, gate.ReasonFinding},
		{"rargs", `git ls-files | rargs cat {0}`, gate.ReasonFinding},
		{"xe", `git ls-files | xe cat`, gate.ReasonFinding},
		{"entr", `git ls-files | entr cat /_`, gate.ReasonFinding},
		{"sem", `git ls-files | sem cat`, gate.ReasonFinding},
		{"env_parallel", `git ls-files | env_parallel cat`, gate.ReasonFinding},
		{"script under xargs", `git ls-files | xargs -I{} script -qc 'cat {}' /dev/null`, gate.ReasonFinding},
		{"script in a nested shell", `git ls-files | xargs -I{} sh -c 'script -qc "cat {}" /dev/null'`, gate.ReasonFinding},
		{"watch in a nested shell", `git ls-files | xargs -I{} sh -c 'watch -n1 cat {}'`, gate.ReasonFinding},
		{"flock in a nested shell", `git ls-files | xargs -I{} sh -c 'flock /tmp/l -c "cat {}"'`, gate.ReasonGitContent},
		{"eval in a nested shell", `git ls-files | xargs -I{} sh -c 'eval "cat {}"'`, gate.ReasonFinding},
		{"find -exec in a nested shell", `git ls-files | xargs -I{} sh -c 'find . -name x -exec cat {} \;'`, gate.ReasonFinding},
	}
	for _, l := range deny {
		run(t, leaky, []decideCase{{"leaky: " + l.name, sh(l.line), l.reason}})
	}
	// The same lines over a clean tree: a scan finds nothing (built at run
	// time still denies).
	for _, l := range deny {
		want := ""
		if l.reason == gate.ReasonGitContent {
			want = gate.ReasonGitContent
		}
		run(t, clean, []decideCase{{"clean: " + l.name, sh(l.line), want}})
	}
	run(t, leaky, []decideCase{
		{"xargs echo, not piped to a shell", sh(`git ls-files | xargs echo`), ""},
		{"xargs printf to a file", sh(`git ls-files | xargs printf '%s\n' > list.txt`), ""},
	})
}
