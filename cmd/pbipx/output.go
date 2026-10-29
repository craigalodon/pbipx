package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/secret"
)

// Output formats.
const (
	formatHuman = "human"
	formatJSON  = "json"
)

// noRevealEnv are the variables under which --show-values is refused.
var noRevealEnv = []string{"CI", "PRE_COMMIT", "PBIPX_NO_REVEAL"}

// stdoutIsTerminal reports whether w is an interactive terminal. A test
// replaces it. Only an *os.File can be one; a character device is taken as
// a terminal, which also admits a null device, where nothing is shown.
var stdoutIsTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// outputFlags are the flags of every command that prints findings.
type outputFlags struct {
	format     string
	showValues bool
	showShape  bool
}

func (o *outputFlags) register(cmd *cobra.Command, shape bool) {
	cmd.Flags().StringVar(&o.format, "format", formatHuman, "output format: human or json")
	cmd.Flags().BoolVar(&o.showValues, "show-values", false,
		"print each value (refused off a terminal, under CI, PRE_COMMIT or PBIPX_NO_REVEAL, and with --format json)")
	if shape {
		cmd.Flags().BoolVar(&o.showShape, "show-shape", false, "print each value's silhouette: character classes and lengths, no letters")
	}
}

// check validates the flags against stdout and the environment. A refused
// --show-values is a usage error, never a quiet downgrade (principle 2).
func (o *outputFlags) check(stdout io.Writer) error {
	if o.format != formatHuman && o.format != formatJSON {
		return usageError{"--format must be human or json"}
	}
	if !o.showValues {
		return nil
	}
	if o.format == formatJSON {
		return usageError{"--show-values is refused with --format json"}
	}
	for _, name := range noRevealEnv {
		if _, set := os.LookupEnv(name); set {
			return usageError{"--show-values is refused when " + name + " is set"}
		}
	}
	if !stdoutIsTerminal(stdout) {
		return usageError{"--show-values is refused when standard output is not a terminal"}
	}
	return nil
}

// valueText is how a finding's value prints: the fixed marker, its
// silhouette, or, on the --show-values path alone, its plaintext. Never a
// length or a digest.
func (o *outputFlags) valueText(v secret.Value) string {
	switch {
	case o.showValues:
		return revealValue(v)
	case o.showShape:
		return secret.Shape(v)
	}
	return secret.Redacted
}

// writeResult writes a scan result in the chosen format.
func (o *outputFlags) writeResult(w io.Writer, res finding.Result, strict bool) error {
	if o.format == formatJSON {
		return res.WriteJSON(w)
	}
	return o.writeHuman(w, res, strict)
}

// writeHuman writes one block per finding and a summary.
func (o *outputFlags) writeHuman(w io.Writer, res finding.Result, strict bool) error {
	findings := slices.Clone(res.Findings)
	finding.Sort(findings)
	var b strings.Builder
	counts := map[finding.Tier]int{}
	for _, f := range findings {
		counts[f.Tier]++
		fmt.Fprintf(&b, "%s  %s\n", f.Tier, f.Path)
		if f.Context != "" {
			fmt.Fprintf(&b, "  context      %s\n", f.Context)
		}
		if f.Locator != "" {
			fmt.Fprintf(&b, "  locator      %s\n", f.Locator)
		}
		if f.Column != nil {
			fmt.Fprintf(&b, "  column       %s\n", f.Column)
		}
		reasons := make([]string, len(f.Reasons))
		for i, r := range f.Reasons {
			reasons[i] = string(r)
		}
		fmt.Fprintf(&b, "  reasons      %s\n", strings.Join(reasons, ", "))
		if f.Tier != finding.Error {
			fmt.Fprintf(&b, "  fingerprint  %s\n", f.Fingerprint())
			fmt.Fprintf(&b, "  value        %s\n", o.valueText(f.Value))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "pbipx: %d %s, %d %s, %d %s; %d accepted by policy, %d suppressed by the baseline\n",
		counts[finding.Block], plural(counts[finding.Block], "block", "blocks"),
		counts[finding.Warn], plural(counts[finding.Warn], "warning", "warnings"),
		counts[finding.Error], plural(counts[finding.Error], "error", "errors"),
		res.Accepted, res.Suppressed)
	if counts[finding.Warn] > 0 && counts[finding.Block] == 0 && counts[finding.Error] == 0 && !strict {
		b.WriteString("pbipx: warnings pass without --strict\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// failure reports a failure before or instead of a scan result: a line on
// stderr naming the class and the structure, and with --format json the
// normalized failure on stdout. It returns the operational exit.
func failure(cmd *cobra.Command, format string, f finding.Failure, detail string) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "pbipx: %s: %s\n", f, detail)
	if format == formatJSON {
		_ = finding.Result{Failure: f}.WriteJSON(cmd.OutOrStdout())
	}
	return exitError{code: finding.ExitOperational}
}

// operational reports an operational error that is not a failure class of
// the vocabulary, such as a git command that failed. detail is structure
// only.
func operational(cmd *cobra.Command, detail string) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "pbipx: error: %s\n", detail)
	return exitError{code: finding.ExitOperational}
}
