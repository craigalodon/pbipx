package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellInputRedirectionUnknown: a file an input redirection names
// that the gate cannot tell (an expansion, a runner's placeholder) is read
// by whatever program the redirection is for: the command, a loop, exec.
// It touches every root.
func TestShellInputRedirectionUnknown(t *testing.T) {
	leaky := newRepo(t, leakyFiles())
	clean := newRepo(t, cleanFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	finding := gate.ReasonFinding
	lines := []struct{ name, line string }{
		{"xargs, jq <", `git ls-files | xargs -I{} sh -c 'jq . < {}'`},
		{"xargs, cat <", `git ls-files | xargs -I{} sh -c 'cat < {}'`},
		{"parallel, jq <", `git ls-files | parallel 'jq . < {}'`},
		{"bash -c, cat <", `git ls-files | xargs -I{} bash -c 'cat < {}'`},
		{"redirection first, jq", `git ls-files | xargs -I{} sh -c '< {} jq .'`},
		{"redirection attached, cat", `git ls-files | xargs -I{} sh -c '<{} cat'`},
		{"0<", `git ls-files | xargs -I{} sh -c 'cat 0< {}'`},
		{"quoted placeholder", `git ls-files | xargs -I{} sh -c 'jq . < "{}"'`},
		{"python json.tool", `git ls-files | xargs -I{} sh -c 'python3 -m json.tool < {}'`},
		{"tee", `git ls-files | xargs -I{} sh -c 'tee /dev/null < {}'`},
		{"exec 3<", `git ls-files | xargs -I{} sh -c 'exec 3< {}; cat <&3'`},
		{"cat 3<", `git ls-files | xargs -I{} sh -c 'cat 3< {} <&3'`},
		{"while read, done <", `git ls-files | xargs -I{} sh -c 'while read l; do echo "$l"; done < {}'`},
		{"a variable, cat <", `f=pbip/B.Report/definition/pages/0000/visuals/0001/visual.json; cat < "$f"`},
		{"an unknown variable, cat <", `cat < "$f"`},
		{"an unknown variable, tee <", `tee /dev/null < "$f"`},
		{"an unknown variable, python <", `python3 -m json.tool < "$f"`},
		{"an unknown variable, while done <", `while read l; do echo "$l"; done < "$f"`},
		{"an unknown variable, exec 3<", `exec 3< "$f"; cat <&3`},
		{"a substitution, cat <", `cat < "$(git ls-files)"`},
	}
	for _, l := range lines {
		run(t, leaky, []decideCase{{l.name + ", leaky", sh(l.line), finding}})
		run(t, clean, []decideCase{{l.name + ", clean", sh(l.line), ""}})
	}
	// Known targets stay as they were, and a here-string is no file.
	run(t, leaky, []decideCase{
		{"cat < a file outside the roots", sh("cat < README.md"), ""},
		{"a here-string with an expansion", sh(`cat <<< "$f"`), ""},
		{"an output redirection to an unknown file", sh(`echo x > "$f"`), ""},
		{"xargs -I echo with a placeholder redirection target", sh(`git ls-files | xargs -I{} sh -c 'echo x > {}.bak'`), ""},
	})
}
