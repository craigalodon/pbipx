package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/baseline"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/scan"
)

// noRecordEnv are the variables under which recording an acceptance is
// refused: an acceptance is a reviewer's decision, never an automation's.
var noRecordEnv = []string{"CI", "PRE_COMMIT"}

func newBaselineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "record and check accepted warnings",
		Long: `Record and check accepted warnings in ` + baseline.FileName + `.

A warning may be accepted until a date. A block never can: a false positive
is fixed in policy, in a reviewable change.`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return usageError{"baseline needs a subcommand: record or verify"}
		},
	}
	cmd.AddCommand(newBaselineRecordCmd(), newBaselineVerifyCmd())
	return cmd
}

type recordFlags struct {
	note     string
	days     int
	policy   string
	baseline string
}

func newBaselineRecordCmd() *cobra.Command {
	var f recordFlags
	cmd := &cobra.Command{
		Use:   "record <fingerprint> --note TEXT",
		Short: "accept a current warning until a date",
		Long: `Accept the current warning with the given fingerprint (from scan's output)
until an expiry date. The note is required and says why.

Refused (exit 3) under CI or PRE_COMMIT. Refused (exit 4) for a block, or for
a fingerprint no current finding has.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecord(cmd, args[0], &f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.note, "note", "", "why the warning is accepted (required)")
	fl.IntVar(&f.days, "expires-days", baseline.DefaultExpiryDays, "days until the acceptance expires")
	fl.StringVar(&f.policy, "policy", "", "the policy file (default: .pbipx.toml at the repository root)")
	fl.StringVar(&f.baseline, "baseline", "", "the baseline file (default: "+baseline.FileName+" at the repository root)")
	return cmd
}

func runRecord(cmd *cobra.Command, fp string, f *recordFlags) error {
	for _, name := range noRecordEnv {
		if _, set := os.LookupEnv(name); set {
			return operational(cmd, "recording an acceptance is refused when "+name+" is set")
		}
	}
	switch {
	case f.note == "":
		return usageError{"record needs --note"}
	case f.days <= 0:
		return usageError{"--expires-days must be at least 1"}
	}
	cwd, repo, err := openRepo(cmd, formatHuman)
	if err != nil {
		return err
	}
	p, err := loadPolicy(cmd, formatHuman, cwd, repo, f.policy)
	if err != nil {
		return err
	}
	path, name := baselinePath(cwd, repo, f.baseline)
	base, err := loadBaseline(cmd, formatHuman, path, name)
	if err != nil {
		return err
	}
	set, err := repo.All(p.ProtectedRoots())
	if err != nil {
		return sourceError(cmd, err)
	}
	defer func() { _ = set.Close() }()
	// The current findings, with nothing accepted by the baseline, so an
	// entry can be renewed.
	res := scan.Run(set.Paths, set, scan.Options{Policy: p, Scanners: scanners(), Now: now()})

	entry, err := base.Record(fp, res.Findings, f.note, f.days, now())
	if err != nil {
		var refusal baseline.Refusal
		if errors.As(err, &refusal) {
			return usageError{refusal.Error()}
		}
		return operational(cmd, "recording failed")
	}
	if err := base.Save(path); err != nil {
		return operational(cmd, name+" cannot be written")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "pbipx: recorded %s in %s, expires %s\n", fp, name, entry.Expires)
	return nil
}

func newBaselineVerifyCmd() *cobra.Command {
	var baselineFlag string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "check that no accepted warning has expired",
		Long: `Check the baseline. Exit 2 when any entry has expired (an expiry that does
not parse counts as expired), 3 when the baseline is invalid.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runVerify(cmd, baselineFlag)
		},
	}
	cmd.Flags().StringVar(&baselineFlag, "baseline", "", "the baseline file (default: "+baseline.FileName+" at the repository root)")
	return cmd
}

func runVerify(cmd *cobra.Command, flagPath string) error {
	cwd, repo, err := openRepo(cmd, formatHuman)
	if err != nil {
		return err
	}
	path, name := baselinePath(cwd, repo, flagPath)
	base, err := loadBaseline(cmd, formatHuman, path, name)
	if err != nil {
		return err
	}
	expired := base.Expired(now())
	w := cmd.OutOrStdout()
	// Only the key is printed, and only when it is a fingerprint: the other
	// fields are free text in a file anyone can edit.
	for _, fp := range expired {
		fmt.Fprintf(w, "expired  %s\n", baseline.PrintableKey(fp))
	}
	fmt.Fprintf(w, "pbipx: %d %s in %s, %d expired\n",
		len(base.Entries), plural(len(base.Entries), "entry", "entries"), name, len(expired))
	if len(expired) > 0 {
		return exitError{code: finding.ExitWarnStrict}
	}
	return nil
}
