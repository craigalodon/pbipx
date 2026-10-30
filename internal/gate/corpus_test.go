package gate_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/gate"
)

// corpusDir is the behavior corpus (docs/design/corpus.md).
const corpusDir = "../../testdata/corpus"

// hookEvent is the part of a Claude Code PreToolUse event the gate reads.
// The adapter that reads the real event lives with the gate command; this
// one exists so the corpus can drive Decide directly.
type hookEvent struct {
	Cwd       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		Path         string `json:"path"`
		Command      string `json:"command"`
		NotebookPath string `json:"notebook_path"`
	} `json:"tool_input"`
}

// eventFrom builds an Event from a case's stdin; ok is false when the stdin
// is not a JSON object, which the adapter denies with event-error before
// Decide is reached.
func eventFrom(stdin []byte) (gate.Event, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(stdin, &obj) != nil {
		return gate.Event{}, false
	}
	var he hookEvent
	if json.Unmarshal(stdin, &he) != nil {
		return gate.Event{}, false
	}
	ev := gate.Event{Cwd: he.Cwd, Tool: gate.Other}
	switch he.ToolName {
	case "Read":
		ev.Tool, ev.Path = gate.Read, he.ToolInput.FilePath
	case "Grep":
		ev.Tool, ev.Path = gate.Search, he.ToolInput.Path
	case "Bash":
		ev.Tool, ev.Command = gate.Shell, he.ToolInput.Command
	case "PowerShell":
		ev.Tool, ev.Command = gate.PowerShell, he.ToolInput.Command
	case "Write", "Edit", "MultiEdit":
		ev.Tool, ev.Path = gate.Write, he.ToolInput.FilePath
	case "NotebookEdit":
		ev.Tool, ev.Path = gate.Write, he.ToolInput.NotebookPath
	}
	return ev, true
}

// TestCorpusGateCases drives every case under testdata/corpus/gate/ through
// Decide, with the approval check on the case's own store, and checks the decision and its reason class, and that no
// sentinel appears in the message.
func TestCorpusGateCases(t *testing.T) {
	isolateGit(t)
	isolateSearch(t)
	c, err := corpus.Open(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, cs := range c.Cases {
		if cs.Area != "gate" {
			continue
		}
		ran++
		t.Run(cs.Name, func(t *testing.T) {
			if cs.Expected == nil {
				t.Fatal("the case has no expected.json")
			}
			root, config := tempRoot(t), t.TempDir()
			if _, err := c.Prepare(cs, root, config, ""); err != nil {
				t.Fatal(err)
			}
			stdin := cs.StdinFor(root)
			ev, ok := eventFrom(stdin)
			if !ok {
				// Not an event at all: the adapter's case, not Decide's.
				if cs.Expected.Decision != "deny" || cs.Expected.Reason != gate.ReasonEventError {
					t.Errorf("stdin is not a JSON object, but the case expects %s %s",
						cs.Expected.Decision, cs.Expected.Reason)
				}
				return
			}
			store := &approval.Store{Dir: config}
			d := gate.Decide(ev, gate.Options{CheckPolicy: gate.RequireApproval(store), Adoption: &gate.Adoption{Store: store}})
			decision := "deny"
			if d.Allow {
				decision = "allow"
			}
			if decision != cs.Expected.Decision || d.Reason != cs.Expected.Reason {
				t.Errorf("Decide = %s %q, want %s %q", decision, d.Reason, cs.Expected.Decision, cs.Expected.Reason)
			}
			for i, s := range cs.Spec.Sentinels {
				if strings.Contains(d.Message, s) || strings.Contains(d.Message, strings.ReplaceAll(s, "'", "''")) {
					t.Errorf("sentinels[%d] appears in the message", i)
				}
			}
		})
	}
	if ran == 0 {
		t.Fatal("no gate cases found")
	}
}
