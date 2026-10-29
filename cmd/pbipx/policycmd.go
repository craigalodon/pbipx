package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
)

// approveWord is what a person types, in full, to approve a policy.
const approveWord = "approve"

// stdinIsTerminal reports whether r is an interactive terminal. A test
// replaces it.
var stdinIsTerminal = isTerminalReader

func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && isTerminal(f)
}

func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "approve the repository's policy, or report whether it is approved",
		Long: `Approve the repository's policy, or report whether it is approved (ADR-0006).

The gate denies every call while .pbipx.toml has no approval on this machine,
or has changed since it was approved. An approval is the SHA-256 digest of
the file's bytes, stored outside the repository in
${PBIPX_CONFIG_DIR:-<user config dir>/pbipx}/approved/.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.ErrOrStderr(), cmd.UsageString())
			return errUsage
		},
	}
	cmd.AddCommand(newPolicyApproveCmd(), newPolicyStatusCmd())
	return cmd
}

func newPolicyApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve",
		Short: "approve .pbipx.toml for the gate, at a terminal",
		Long: `Approve the repository's .pbipx.toml for the gate on this machine.

It shows the policy's digest and asks for "approve" to be typed in full.
Approval is a person's act: it is refused, with a usage error, unless
standard input and standard output are terminals, and when CI, PRE_COMMIT
or PBIPX_NO_REVEAL is set.`,
		Args: cobra.NoArgs,
		RunE: runPolicyApprove,
	}
}

// approveRefusal is the usage error that refuses an approval outside a
// person's terminal, or nil.
func approveRefusal(stdin io.Reader, stdout io.Writer) error {
	for _, name := range noRevealEnv {
		if _, set := os.LookupEnv(name); set {
			return usageError{"policy approve is refused when " + name + " is set"}
		}
	}
	if !stdinIsTerminal(stdin) {
		return usageError{"policy approve is refused when standard input is not a terminal"}
	}
	if !stdoutIsTerminal(stdout) {
		return usageError{"policy approve is refused when standard output is not a terminal"}
	}
	return nil
}

func runPolicyApprove(cmd *cobra.Command, _ []string) error {
	if err := approveRefusal(cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
		return err
	}
	cwd, repo, err := openRepo(cmd, formatHuman)
	if err != nil {
		return err
	}
	// A policy that does not load is not approved: the gate would deny with
	// policy-error whatever the approval says.
	if _, err := loadPolicy(cmd, formatHuman, cwd, repo, ""); err != nil {
		return err
	}
	store, err := approval.Open()
	if err != nil {
		return operational(cmd, "the approval store cannot be found: set "+approval.EnvConfigDir+" to an absolute path")
	}
	path := filepath.Join(repo.Root, policy.FileName)
	digest, err := approval.DigestFile(path)
	if err != nil {
		return failure(cmd, formatHuman, finding.PolicyError, policy.FileName+": cannot be read")
	}
	status, err := store.Status(repo.Root, path)
	if err != nil {
		return operational(cmd, "the approval record cannot be read")
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "policy  %s\ndigest  %s\nstatus  %s\n\n", policy.FileName, digest, status)
	fmt.Fprintf(out, "Type %q to approve this policy for the gate in this repository on this machine: ", approveWord)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if strings.TrimSpace(line) != approveWord {
		fmt.Fprintln(out)
		return usageError{"the policy was not approved"}
	}
	if err := store.Approve(repo.Root, digest, now()); err != nil {
		return operational(cmd, "the approval cannot be written")
	}
	fmt.Fprintf(out, "pbipx: %s approved\n", policy.FileName)
	return nil
}

func newPolicyStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "report whether .pbipx.toml is approved",
		Long: `Report whether the repository's .pbipx.toml is approved on this machine:
approved, unapproved (no approval), changed (the file differs from the
approved one) or missing (no policy file). It exits 0 when approved and 3
otherwise.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, repo, err := openRepo(cmd, formatHuman)
			if err != nil {
				return err
			}
			store, err := approval.Open()
			if err != nil {
				return operational(cmd, "the approval store cannot be found: set "+approval.EnvConfigDir+" to an absolute path")
			}
			status, err := store.Status(repo.Root, filepath.Join(repo.Root, policy.FileName))
			if err != nil {
				return operational(cmd, "the approval record cannot be read")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", policy.FileName, status)
			if status != approval.Approved {
				return exitError{code: finding.ExitOperational}
			}
			return nil
		},
	}
}
