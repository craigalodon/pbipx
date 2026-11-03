package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellCdGlob: a cd, pushd or chdir target with an unquoted glob
// character is expanded by the shell to a directory the gate cannot tell,
// as an expansion is: the working directory is unknown after it.
func TestShellCdGlob(t *testing.T) {
	leaky := newRepo(t, leakyFiles())
	clean := newRepo(t, cleanFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	finding := gate.ReasonFinding
	lines := []string{
		`cd pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`cd pb?p && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`cd pb[i]p && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`cd p*/B.Report && cat definition/pages/0000/visuals/0001/visual.json`,
		`builtin cd pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`command cd pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`cd -P pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`cd -L pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`cd -- pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`pushd pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`chdir pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`(cd pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json)`,
		`{ cd pb*; cat B.Report/definition/pages/0000/visuals/0001/visual.json; }`,
		`cd pb* ; cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
		`cd notes && cd ../pb* && cat B.Report/definition/pages/0000/visuals/0001/visual.json`,
	}
	for _, l := range lines {
		run(t, leaky, []decideCase{{"leaky: " + l, sh(l), finding}})
		run(t, clean, []decideCase{{"clean: " + l, sh(l), ""}})
	}
	run(t, leaky, []decideCase{
		{"a quoted glob character is literal", sh(`cd 'pb*' 2>/dev/null; cat README.md`), ""},
		{"a plain cd to a note directory", sh(`cd notes && cat clean.md`), ""},
		{"an escaped glob character is literal", sh(`cd pb\* 2>/dev/null; cat README.md`), ""},
	})
}
