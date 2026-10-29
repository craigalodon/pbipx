// Command pbipx is a toolkit for developing Power BI projects with AI agents
// without leaking the data Power BI Desktop persists into them.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/finding"
)

// version is set at release with -ldflags "-X main.version=<tag>".
var version = "dev"

// errUsage is returned for every usage error Cobra reports. Cobra's own
// messages quote what was typed, and a command line can carry a value, so
// they are never printed (ADR-0005).
var errUsage = errors.New("usage error")

// usageError is a usage error pbipx itself detects. Its message is fixed
// text that names flags, never what was typed.
type usageError struct{ msg string }

func (e usageError) Error() string { return "usage error: " + e.msg }

// exitError ends a command with a code after the command has written its own
// output.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// exitWith is nil for a clean exit and an exitError otherwise.
func exitWith(code int) error {
	if code == finding.ExitClean {
		return nil
	}
	return exitError{code: code}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes a command and returns the process exit code. A panic is an
// operational error, reported without its message, which could carry a
// value.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	if len(args) > 0 && args[0] == gateCommand {
		return runGate(args, stdin, stdout, stderr)
	}
	defer func() {
		if recover() != nil {
			fmt.Fprintln(stderr, "pbipx: internal error")
			code = finding.ExitOperational
		}
	}()
	if args == nil {
		// Cobra reads os.Args when given nil.
		args = []string{}
	}
	root := newRootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	var ee exitError
	var ue usageError
	switch {
	case err == nil:
		return finding.ExitClean
	case errors.As(err, &ee):
		return ee.code
	case errors.As(err, &ue):
		fmt.Fprintf(stderr, "pbipx: %s\n", ue.Error())
		return finding.ExitUsage
	}
	// Every other error is Cobra's, and is not printed.
	fmt.Fprintln(stderr, "pbipx: usage error; run 'pbipx help'")
	return finding.ExitUsage
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pbipx",
		Short: "tools for agentic Power BI project development",
		Long: `pbipx: tools for agentic Power BI project development

Commands: init, scan, scan-text, baseline, explain, gate.
Planned: stage, collect, perf. See docs/ROADMAP.md.`,
		Version:            version,
		SilenceErrors:      true,
		SilenceUsage:       true,
		DisableSuggestions: true,
		CompletionOptions:  cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.ErrOrStderr(), cmd.UsageString())
			return errUsage
		},
	}
	root.SetVersionTemplate("pbipx {{.Version}}\n")
	root.SetFlagErrorFunc(func(*cobra.Command, error) error { return errUsage })
	root.SetHelpCommand(newHelpCmd())
	root.AddCommand(newVersionCmd(), newScanCmd(), newScanTextCmd(), newBaselineCmd(), newExplainCmd(), newGateCmd(), newInitCmd())
	return root
}

// newHelpCmd replaces Cobra's help command, which echoes an unknown topic.
func newHelpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "print help for a command",
		RunE: func(cmd *cobra.Command, args []string) error {
			target, rest, err := cmd.Root().Find(args)
			if err != nil || len(rest) > 0 {
				return errUsage
			}
			return target.Help()
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "pbipx", version)
			return nil
		},
	}
}
