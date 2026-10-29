// Command pbipx is a toolkit for developing Power BI projects with AI agents
// without leaking the data Power BI Desktop persists into them.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at release with -ldflags "-X main.version=<tag>".
var version = "dev"

const usage = `pbipx: tools for agentic Power BI project development

Usage:
  pbipx <command> [arguments]

Commands:
  version    print the version
  help       print this help

Planned: scan, scan-text, gate, explain, stage, collect, perf.
See docs/ROADMAP.md.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a command and returns the process exit code: 0 on success,
// 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintln(stdout, "pbipx", version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	// The argument is not echoed: a command line can carry a value too.
	fmt.Fprintln(stderr, "pbipx: unknown command; run 'pbipx help'")
	return 2
}
