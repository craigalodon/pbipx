package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/scan"
	"github.com/craigalodon/pbipx/internal/source"
)

// stdinPath names standard input on the command line and in findings.
const stdinPath = "-"

type scanTextFlags struct {
	out       outputFlags
	commitMsg bool
	policy    string
}

func newScanTextCmd() *cobra.Command {
	var f scanTextFlags
	cmd := &cobra.Command{
		Use:   "scan-text [files... | -]",
		Short: "scan prose (notes, a commit message) for personal data",
		Long: `Scan prose for personal data, line by line: a name standing alone, an
email, an SSN or a phone number. "-" reads standard input. Every path given is
scanned, whatever the policy's prose scope says.

Exit codes: 0 clean, 1 a finding, 3 an error or a failure, 4 a usage error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScanText(cmd, args, &f)
		},
	}
	cmd.Flags().BoolVar(&f.commitMsg, "commit-msg", false, "scan a commit message: every line, comment lines included, up to git's scissors line")
	cmd.Flags().StringVar(&f.policy, "policy", "", "the policy file (default: .pbipx.toml at the repository root)")
	f.out.register(cmd, true)
	return cmd
}

func runScanText(cmd *cobra.Command, args []string, f *scanTextFlags) error {
	if len(args) == 0 {
		return usageError{"scan-text needs a file, or - for standard input"}
	}
	if err := f.out.check(cmd.OutOrStdout()); err != nil {
		return err
	}
	format := f.out.format

	cwd, err := os.Getwd()
	if err != nil {
		return operational(cmd, "the working directory cannot be read")
	}
	// The repository is where the policy is found. With --policy, prose can
	// be scanned outside one.
	repo, err := source.Open(cwd)
	if err != nil {
		if f.policy == "" {
			return repoError(cmd, format, err)
		}
		repo = nil
	}
	p, err := loadPolicy(cmd, format, cwd, repo, f.policy)
	if err != nil {
		return err
	}

	scanner := scan.Prose
	if f.commitMsg {
		comment := "#"
		if repo != nil {
			comment = repo.CommentChar()
		}
		scanner = scan.CommitMessage(comment)
	}
	var findings []finding.Finding
	for i, a := range args {
		path := stdinPath
		var data []byte
		var rerr error
		if a == stdinPath {
			data, rerr = io.ReadAll(cmd.InOrStdin())
		} else {
			path = displayPath(repo, cwd, a, i)
			data, rerr = os.ReadFile(resolve(cwd, a))
		}
		if rerr != nil {
			findings = append(findings, finding.Finding{Path: path, Tier: finding.Error, Reasons: []finding.Reason{finding.Unreadable}})
			continue
		}
		findings = append(findings, scan.File(path, data, scanner, p)...)
	}
	res := scan.Accept(findings, scan.Options{Policy: p, Now: now()})
	if err := f.out.writeResult(cmd.OutOrStdout(), res, false); err != nil {
		return operational(cmd, "writing the result failed")
	}
	return exitWith(res.ExitCode(false))
}

// displayPath is how a finding names the file given as argument i (from
// 0): its repository-relative path with forward slashes when it names an
// existing regular file inside the repository, and otherwise "argument N"
// (from 1). An argument that names no such file is text typed on a command
// line, which can carry a value, so it is never echoed (ADR-0005).
func displayPath(repo *source.Repo, cwd, arg string, i int) string {
	byPosition := fmt.Sprintf("argument %d", i+1)
	if repo == nil {
		return byPosition
	}
	rel, err := repo.Rel(cwd, arg)
	if err != nil || rel == "." {
		return byPosition
	}
	info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(rel)))
	if err != nil || !info.Mode().IsRegular() {
		return byPosition
	}
	return rel
}
