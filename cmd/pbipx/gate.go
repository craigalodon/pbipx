package main

import (
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/gate"
	"github.com/craigalodon/pbipx/internal/gate/claudecode"
)

// gateCommand is the gate's name on the command line.
const gateCommand = "gate"

// gateOptions are the options every gate decision is made with: the policy
// must be one a person approved (ADR-0006).
var gateOptions = gate.Options{CheckPolicy: gate.RequireApproval(nil)}

// errGateUsage is any problem with the gate's own command line. It is a deny
// like every other failure of the gate.
var errGateUsage = errors.New("gate usage error")

func newGateCmd() *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "gate [--agent claude-code]",
		Short: "decide an agent's tool call (a pre-tool hook)",
		Long: `Decide an agent's tool call, as its pre-tool hook.

The event is read from stdin. An allow prints nothing and exits 0. A deny
prints the agent's decision JSON on stdout and the reason on stderr, and
exits 2. Every failure, including a usage error, is a deny: the gate never
exits with any other code.

The repository's .pbipx.toml must be approved on this machine ('pbipx policy
approve'). Until it is, and after any change to it, every call is denied.

--agent claude-code, the default and only value, reads a Claude Code
PreToolUse event. Configure the hook with a command that turns every other
exit, such as a missing binary, into a deny (internal/gate/claudecode).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if agent != claudecode.Agent {
				return errGateUsage
			}
			return exitWith(claudecode.Run(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), gateOptions))
		},
	}
	cmd.Flags().StringVar(&agent, "agent", claudecode.Agent, "the agent whose hook protocol to speak (claude-code)")
	return cmd
}

// runGate runs `pbipx gate` (args[0] is "gate"). Unlike every other command
// it exits only 0 or 2: Claude Code proceeds on any other code, so a usage
// error, a help request and a panic are each a deny.
func runGate(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	deny := func(msg string) int {
		return claudecode.Render(claudecode.Deny(claudecode.ReasonInternalError, msg), stdout, stderr)
	}
	defer func() {
		if recover() != nil {
			code = deny("the gate failed")
		}
	}()
	root := newRootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	cmd, _, err := root.Find(args)
	if err != nil || cmd.Name() != gateCommand {
		return deny("the gate's command line is not valid")
	}
	// Help exits 0, which would allow the call. On this path it is a deny.
	helped := false
	cmd.SetHelpFunc(func(*cobra.Command, []string) { helped = true })
	err = root.Execute()
	var ee exitError
	switch {
	case helped:
		return deny("the gate's command line is not valid")
	case err == nil:
		return claudecode.ExitAllow
	case errors.As(err, &ee) && (ee.code == claudecode.ExitAllow || ee.code == claudecode.ExitDeny):
		return ee.code
	}
	return deny("the gate's command line is not valid")
}
