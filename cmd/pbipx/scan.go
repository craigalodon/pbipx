package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/baseline"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/scan"
	"github.com/craigalodon/pbipx/internal/source"
)

// scanners is the scanner registry the commands use. A test replaces it.
var scanners = scan.Scanners

type scanFlags struct {
	out        outputFlags
	staged     bool
	all        bool
	rev        string
	revSet     bool
	strict     bool
	policy     string
	baseline   string
	noBaseline bool
}

func newScanCmd() *cobra.Command {
	var f scanFlags
	cmd := &cobra.Command{
		Use:   "scan [paths...] [--staged | --all | --rev R]",
		Short: "scan the repository for persisted personal data",
		Long: `Scan files for persisted personal data.

Sources (one at most): --all, the default, scans every file git would commit
and every file on disk under a protected root; --staged scans the index;
--rev scans a commit's tree; paths scan those files, a directory expanding to
the files under it.

Exit codes: 0 clean (warnings pass without --strict), 1 a block, 2 warnings
under --strict, 3 an error finding or a failure, 4 a usage error.

Values are never printed. --show-shape prints a silhouette; --show-values is
for an interactive terminal only.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, args, &f)
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.staged, "staged", false, "scan the index")
	fl.BoolVar(&f.all, "all", false, "scan every file git would commit and every file under a protected root (the default)")
	fl.StringVar(&f.rev, "rev", "", "scan the tree of a commit")
	fl.BoolVar(&f.strict, "strict", false, "fail on warnings")
	fl.StringVar(&f.policy, "policy", "", "the policy file (default: .pbipx.toml at the repository root)")
	fl.StringVar(&f.baseline, "baseline", "", "the baseline file (default: "+baseline.FileName+" at the repository root)")
	fl.BoolVar(&f.noBaseline, "no-baseline", false, "ignore the baseline")
	f.out.register(cmd, true)
	return cmd
}

func runScan(cmd *cobra.Command, args []string, f *scanFlags) error {
	sources := 0
	f.revSet = cmd.Flags().Changed("rev")
	for _, on := range []bool{f.staged, f.all, f.revSet, len(args) > 0} {
		if on {
			sources++
		}
	}
	if sources > 1 {
		return usageError{"give at most one of --staged, --all, --rev and paths"}
	}
	if f.noBaseline && f.baseline != "" {
		return usageError{"--baseline and --no-baseline together"}
	}
	if err := f.out.check(cmd.OutOrStdout()); err != nil {
		return err
	}
	format := f.out.format

	cwd, repo, err := openRepo(cmd, format)
	if err != nil {
		return err
	}
	p, err := loadPolicy(cmd, format, cwd, repo, f.policy)
	if err != nil {
		return err
	}
	var base *baseline.Baseline
	if !f.noBaseline {
		path, name := baselinePath(cwd, repo, f.baseline)
		if base, err = loadBaseline(cmd, format, path, name); err != nil {
			return err
		}
	}
	set, err := openSet(repo, p, cwd, args, f)
	if err != nil {
		return sourceError(cmd, err)
	}
	defer func() { _ = set.Close() }()

	res := scan.Run(set.Paths, set, scan.Options{Policy: p, Baseline: base, Scanners: scanners(), Now: now(), AgentSettings: true})
	if err := f.out.writeResult(cmd.OutOrStdout(), res, f.strict); err != nil {
		return operational(cmd, "writing the result failed")
	}
	return exitWith(res.ExitCode(f.strict))
}

// openSet is the files the flags select.
func openSet(repo *source.Repo, p *policy.Policy, cwd string, args []string, f *scanFlags) (*source.Set, error) {
	switch {
	case f.staged:
		return repo.Staged()
	case f.revSet:
		return repo.Rev(f.rev)
	case len(args) > 0:
		return repo.Paths(cwd, args)
	}
	return repo.All(p.ProtectedRoots())
}

// sourceError reports why the files to scan could not be listed: a usage
// error for a bad path or revision, an operational error for git. Neither
// names what was typed.
func sourceError(cmd *cobra.Command, err error) error {
	var pe *source.PathError
	switch {
	case errors.As(err, &pe):
		return usageError{pe.Error()}
	case errors.Is(err, source.ErrBadRevision):
		return usageError{"--rev does not name a commit"}
	}
	return operational(cmd, gitDetail(err))
}
