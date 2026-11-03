package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestEdit: an Edit, MultiEdit or NotebookEdit shows the model a snippet
// of the file around the edit, so its target is judged as a Read of the
// file as well as a Write: a finding, local state or an unvettable file
// denies, and the settings rules of a Write still apply. A Write shows no
// old content, and is judged as before.
func TestEdit(t *testing.T) {
	files := leakyFiles()
	files[agentSettings] = "{}\n"
	root := newRepo(t, files)
	edit := func(p string) gate.Event { return gate.Event{Tool: gate.Edit, Path: p} }
	write := func(p string) gate.Event { return gate.Event{Tool: gate.Write, Path: p} }
	read := func(p string) gate.Event { return gate.Event{Tool: gate.Read, Path: p} }
	run(t, root, []decideCase{
		{"edit of a file with a finding", edit(leakVisual), gate.ReasonFinding},
		{"read of it, for comparison", read(leakVisual), gate.ReasonFinding},
		{"write of it", write(leakVisual), ""},
		{"edit of a note that leaks", edit(leakNote), gate.ReasonFinding},
		{"edit of local state", edit(localState), gate.ReasonLocalState},
		{"edit of the settings", edit(agentSettings), gate.ReasonAgentSettings},
		{"edit of a clean note", edit(cleanNote), ""},
		{"edit of a report file", edit(reportJSON), ""},
		{"edit of a file not there yet", edit("notes/new.md"), ""},
		{"edit with no path", edit(""), ""},
	})
}
