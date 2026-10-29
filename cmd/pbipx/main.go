// Command pbipx is a toolkit for developing Power BI projects with AI agents
// without leaking the data Power BI Desktop persists into them.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// version is set at release with -ldflags "-X main.version=<tag>".
var version = "dev"

// errUsage is returned for every usage error. Cobra's own messages quote what
// was typed, and a command line can carry a value, so they are never printed
// (ADR-0005).
var errUsage = errors.New("usage error")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Exit codes (testdata/corpus/vocabulary.toml). The gate has its own.
const (
	exitClean = 0
	exitUsage = 4
)

// run executes a command and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if args == nil {
		// Cobra reads os.Args when given nil.
		args = []string{}
	}
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		// Every error is reported as a usage error for now; the error itself
		// is not printed.
		fmt.Fprintln(stderr, "pbipx: usage error; run 'pbipx help'")
		return exitUsage
	}
	return exitClean
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pbipx",
		Short: "tools for agentic Power BI project development",
		Long: `pbipx: tools for agentic Power BI project development

Planned: scan, scan-text, gate, explain, stage, collect, perf.
See docs/ROADMAP.md.`,
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
	root.AddCommand(newVersionCmd())
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
