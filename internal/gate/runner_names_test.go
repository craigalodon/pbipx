package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellRunnerPlaceholders: file names a runner feeds a command (xargs
// -I's replacement string, GNU parallel's placeholders and appended
// input, find -exec's {}) reach a reader through a nested shell line or
// as any program's argument. They are words the gate cannot tell, in a
// tree with a finding.
func TestShellRunnerPlaceholders(t *testing.T) {
	files := leakyFiles()
	files["src/main.go"] = "package main\n"
	leaky := newRepo(t, files)
	clean := newRepo(t, cleanFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	finding := gate.ReasonFinding
	deny := []struct{ name, line string }{
		{"xargs -I sh -c cat", `git ls-files | xargs -I{} sh -c 'cat {}'`},
		{"diff names, xargs -I sh -c cat", `git diff --name-only HEAD~1 | xargs -I{} sh -c 'cat {}'`},
		{"porcelain names, xargs -I sh -c cat", `git status --porcelain | cut -c4- | xargs -I{} sh -c 'cat {}'`},
		{"xargs -n1 -P4 -I bash -c cat", `git ls-files | xargs -n1 -P4 -I{} bash -c 'cat {}'`},
		{"parallel 'cat {}'", `git ls-files | parallel 'cat {}'`},
		{"parallel cat", `git ls-files | parallel cat`},
		{"xargs -I perl -e", `git ls-files | xargs -I{} perl -e 'print <>' {}`},
		{"xargs -I sh -c jq", `git ls-files | xargs -I{} sh -c 'jq . {}'`},
		{"xargs -I bash -c jq", `git ls-files | xargs -I{} bash -c 'jq . {}'`},
		{"timeout, xargs -I sh -c jq", `git ls-files | timeout 5 xargs -I{} sh -c 'jq . {}'`},
		{"parallel 'jq . {}'", `git ls-files | parallel 'jq . {}'`},
		{"parallel jq . {}", `git ls-files | parallel jq . {}`},
		{"xargs -I sh -c jq to /dev/null", `git ls-files | xargs -I{} sh -c 'jq . {}' > /dev/null`},
		{"xargs -I with another string", `git ls-files | xargs -I@ sh -c 'cat @'`},
		{"xargs -i default", `git ls-files | xargs -i sh -c 'cat {}'`},
		{"xargs --replace=R", `git ls-files | xargs --replace=% sh -c 'cat %'`},
		{"xargs --replace default", `git ls-files | xargs --replace sh -c 'cat {}'`},
		{"parallel {.} in a nested shell", `git ls-files | parallel 'sh -c "cat {.}"'`},
		{"parallel {/} placeholder", `git ls-files | parallel cat {/}`},
		{"xargs I-string in a grep -r", `git ls-files | xargs -I{} sh -c 'grep -r x {}'`},
		{"xargs -I perl nested", `git ls-files | xargs -I{} sh -c 'perl -e "print <>" {}'`},
		{"find from . with sh -c cat", `find . -exec sh -c 'cat {}' \;`},
		{"find . -type f -exec sh -c jq", `find . -type f -exec sh -c 'jq . {}' \;`},
	}
	for _, l := range deny {
		run(t, leaky, []decideCase{{l.name + ", leaky", sh(l.line), finding}})
	}
	run(t, leaky, []decideCase{
		{"find src with sh -c jq", sh(`find src -exec sh -c 'jq . {}' \;`), ""},
		{"find src with cat", sh(`find src -exec cat {} \;`), ""},
		{"xargs -I echo", sh(`git ls-files | xargs -I{} echo {}`), ""},
		{"xargs -I sh -c echo", sh(`git ls-files | xargs -I{} sh -c 'echo {}'`), ""},
		{"parallel echo", sh(`git ls-files | parallel echo {}`), ""},
		{"parallel with literal arguments", sh(`parallel cat ::: README.md`), ""},
		{"xargs -I mkdir", sh(`git ls-files | xargs -I{} mkdir -p {}.d`), ""},
	})
	// A clean tree has nothing to leak: the scan finds nothing.
	for _, l := range deny {
		run(t, clean, []decideCase{{l.name + ", clean", sh(l.line), ""}})
	}
}
