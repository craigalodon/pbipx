package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/gate"
)

// TestControlVariables checks that a word naming PBIPX_CONFIG_DIR or
// CLAUDE_CONFIG_DIR is judged as the directory it names: the agent's shell
// expands it, and the store or the settings directory must not be written
// through it. A plain read stays allowed.
func TestControlVariables(t *testing.T) {
	for _, set := range []bool{true, false} {
		name := "variables set"
		if !set {
			name = "variables unset"
		}
		t.Run(name, func(t *testing.T) {
			a := newAdoption(t)
			if !set {
				t.Setenv(approval.EnvConfigDir, "")
				t.Setenv("CLAUDE_CONFIG_DIR", "")
			}
			for _, tc := range []struct {
				name   string
				ev     gate.Event
				reason string
			}{
				{"rm -rf the store", gate.Event{Tool: gate.Shell, Command: "rm -rf $PBIPX_CONFIG_DIR/approved"}, gate.ReasonAgentSettings},
				{"braced", gate.Event{Tool: gate.Shell, Command: "rm -rf ${PBIPX_CONFIG_DIR}/approved"}, gate.ReasonAgentSettings},
				{"quoted", gate.Event{Tool: gate.Shell, Command: `rm -rf "$PBIPX_CONFIG_DIR/approved"`}, gate.ReasonAgentSettings},
				{"the store itself", gate.Event{Tool: gate.Shell, Command: "rm -rf $PBIPX_CONFIG_DIR"}, gate.ReasonAgentSettings},
				{"redirect into the store", gate.Event{Tool: gate.Shell, Command: "echo x > $PBIPX_CONFIG_DIR/approved/x"}, gate.ReasonAgentSettings},
				{"rm the user settings", gate.Event{Tool: gate.Shell, Command: "rm -f $CLAUDE_CONFIG_DIR/settings.json"}, gate.ReasonAgentSettings},
				{"redirect into the user settings", gate.Event{Tool: gate.Shell, Command: "echo '{}' > ${CLAUDE_CONFIG_DIR}/settings.json"}, gate.ReasonAgentSettings},
				{"copy into the settings directory", gate.Event{Tool: gate.Shell, Command: "cp notes.txt $CLAUDE_CONFIG_DIR/"}, gate.ReasonAgentSettings},
				{"cat the store", gate.Event{Tool: gate.Shell, Command: "cat $PBIPX_CONFIG_DIR/approved/x"}, ""},
				{"ls the settings directory", gate.Event{Tool: gate.Shell, Command: "ls $CLAUDE_CONFIG_DIR"}, ""},
				{"PowerShell, store", gate.Event{Tool: gate.PowerShell, Command: `Remove-Item -Recurse $env:PBIPX_CONFIG_DIR\approved`}, gate.ReasonAgentSettings},
				{"PowerShell, braced (a group: unreadable, so denied as such)", gate.Event{Tool: gate.PowerShell, Command: `Remove-Item -Recurse ${env:PBIPX_CONFIG_DIR}\approved`}, gate.ReasonFinding},
				{"PowerShell, any case", gate.Event{Tool: gate.PowerShell, Command: `Remove-Item -Recurse $ENV:pbipx_config_dir/approved`}, gate.ReasonAgentSettings},
				{"PowerShell, settings", gate.Event{Tool: gate.PowerShell, Command: `Remove-Item $env:CLAUDE_CONFIG_DIR\settings.json`}, gate.ReasonAgentSettings},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ev := tc.ev
					ev.Cwd = a.adopted
					check(t, a.decide(ev), tc.reason)
				})
			}
		})
	}
}

// TestControlVariablesSetByTheLine checks that the variables are expanded
// from the gate's environment only when the line provably does not set
// them. Otherwise the word is unknown, as it was before: the call is
// judged as touching every protected root, and a leaky tree denies it.
func TestControlVariablesSetByTheLine(t *testing.T) {
	a := newAdoption(t)
	leak := "/B.Report/definition/pages/0000/visuals/0001/visual.json" // below pbip, which the variable would hold
	for _, tc := range []struct {
		name, tool, cmd, reason string
	}{
		{"read", "sh", "read PBIPX_CONFIG_DIR < /dev/null; cat $PBIPX_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"printf -v", "sh", "printf -v PBIPX_CONFIG_DIR %s /x; cat $PBIPX_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"mapfile", "sh", "mapfile -t PBIPX_CONFIG_DIR < f; cat ${PBIPX_CONFIG_DIR}" + leak, gate.ReasonFinding},
		{"declare", "sh", "declare PBIPX_CONFIG_DIR=/x; cat $PBIPX_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"getopts", "sh", "getopts a PBIPX_CONFIG_DIR; cat $PBIPX_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"read, Claude's variable", "sh", "read CLAUDE_CONFIG_DIR < /dev/null; cat $CLAUDE_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"in a nested line", "sh", "bash -c 'read PBIPX_CONFIG_DIR < /dev/null'; cat $PBIPX_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"eval", "sh", "eval 'x=1'; cat $PBIPX_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"source", "sh", "source ./f; cat $PBIPX_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"dot", "sh", ". ./f; cat $PBIPX_CONFIG_DIR" + leak, gate.ReasonFinding},
		{"expansion that assigns", "sh", "cat ${PBIPX_CONFIG_DIR:=/x}" + leak, gate.ReasonFinding},
		{"still denied when nothing sets it", "sh", "rm -rf $PBIPX_CONFIG_DIR/approved", gate.ReasonAgentSettings},
		{"a plain read of the store", "sh", "cat $PBIPX_CONFIG_DIR/approved/x", ""},
		{"PowerShell, assigned", "ps", `$env:PBIPX_CONFIG_DIR = 'x'; Remove-Item $env:PBIPX_CONFIG_DIR\approved`, gate.ReasonFinding},
		{"PowerShell, Set-Item", "ps", `Set-Item env:PBIPX_CONFIG_DIR x; Remove-Item $env:PBIPX_CONFIG_DIR\approved`, gate.ReasonFinding},
		{"PowerShell, SetEnvironmentVariable", "ps", `[Environment]::SetEnvironmentVariable('PBIPX_CONFIG_DIR','x'); Remove-Item $env:PBIPX_CONFIG_DIR\approved`, gate.ReasonFinding},
		{"PowerShell, Invoke-Expression", "ps", `iex 'x'; Remove-Item $env:PBIPX_CONFIG_DIR\approved`, gate.ReasonAgentSettings},
		{"PowerShell, nothing sets it", "ps", `Remove-Item $env:PBIPX_CONFIG_DIR\approved`, gate.ReasonAgentSettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := gate.Shell
			if tc.tool == "ps" {
				tool = gate.PowerShell
			}
			d := a.decide(gate.Event{Tool: tool, Command: tc.cmd, Cwd: a.adopted})
			check(t, d, tc.reason)
		})
	}
}
