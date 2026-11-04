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
