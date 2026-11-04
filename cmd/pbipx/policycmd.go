package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/gate"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/shape"
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
		Short: "approve the repository's policy, report whether it is approved, or unenroll",
		Long: `Approve the repository's policy, or report whether it is approved (ADR-0006).

The gate denies every call while .pbipx.toml has no approval on this machine,
or has changed since it was approved. An approval is the SHA-256 digest of
the file's bytes, stored outside the repository in
${PBIPX_CONFIG_DIR:-<user config dir>/pbipx}/approved/. Every work tree of
one clone shares its approvals, and the last 16 approved policies stay
approved.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.ErrOrStderr(), cmd.UsageString())
			return errUsage
		},
	}
	cmd.AddCommand(newPolicyApproveCmd(), newPolicyStatusCmd(), newPolicyUnenrollCmd())
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

// personRefusal is the usage error that refuses the person-only step name
// ("policy approve") outside a person's terminal, or nil.
func personRefusal(name string, stdin io.Reader, stdout io.Writer) error {
	for _, env := range noRevealEnv {
		if _, set := os.LookupEnv(env); set {
			return usageError{name + " is refused when " + env + " is set"}
		}
	}
	if !stdinIsTerminal(stdin) {
		return usageError{name + " is refused when standard input is not a terminal"}
	}
	if !stdoutIsTerminal(stdout) {
		return usageError{name + " is refused when standard output is not a terminal"}
	}
	return nil
}

func runPolicyApprove(cmd *cobra.Command, _ []string) error {
	if err := personRefusal("policy approve", cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
		return err
	}
	cwd, repo, err := openRepo(cmd, formatHuman)
	if err != nil {
		return err
	}
	// A policy that does not load is not approved: the gate would deny with
	// policy-error whatever the approval says. The digest is of the bytes
	// that loaded, read once.
	_, data, err := loadPolicyData(cmd, formatHuman, cwd, repo, "")
	if err != nil {
		return err
	}
	store, id, err := openStore(cmd, repo.Root)
	if err != nil {
		return err
	}
	digest := approval.Digest(data)
	status, err := store.Check(id, digest)
	if err != nil {
		return operational(cmd, "the approval record cannot be read")
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "policy  %s\ndigest  %s\nstatus  %s\n\n", policy.FileName, digest, status)
	fmt.Fprintf(out, "Type %q to approve this policy for the gate in every work tree of this clone on this machine: ", approveWord)
	if !confirmed(cmd.InOrStdin(), approveWord) {
		fmt.Fprintln(out)
		return usageError{"the policy was not approved"}
	}
	if err := store.Approve(id, digest, now()); err != nil {
		return operational(cmd, "the approval cannot be written")
	}
	fmt.Fprintf(out, "pbipx: %s approved\n", policy.FileName)
	return nil
}

// confirmed reads one line from in and reports whether it is exactly word.
// The line must end in a newline (a word followed by end of input does not
// count), and only that newline and a carriage return before it are
// removed: spaces around the word do not confirm.
func confirmed(in io.Reader, word string) bool {
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	return line == word
}

// shownPath is a path as a terminal screen prints it: a segment that holds
// a control character or is not valid UTF-8 is {?}, and one with a
// personal-data shape too (shape.DisplayName), so a path from the file
// system or the environment cannot move the cursor or overwrite the line a
// person is reading.
func shownPath(p string) string {
	segs := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
	if len(segs) == 0 {
		return p
	}
	out := p
	for _, seg := range segs {
		var shown string
		if !utf8.ValidString(seg) || strings.ContainsFunc(seg, unicode.IsControl) {
			shown = shape.HiddenName
		} else {
			shown = shape.DisplayName(seg)
		}
		if shown != seg {
			out = strings.Replace(out, seg, shown, 1)
		}
	}
	return out
}

// openStore is the approval store for the repository at root, and what its
// approval is recorded for (the clone), or the operational failure that
// refuses them: not found, git cannot name the clone, or the store is
// inside the repository or the clone, where an agent could write an
// approval.
func openStore(cmd *cobra.Command, root string) (*approval.Store, approval.Repository, error) {
	store, err := approval.OpenFor(root)
	inRepo := func() error {
		return operational(cmd, "the approval store is inside the repository, where an agent could write an approval: set "+approval.EnvConfigDir+" to a directory outside it")
	}
	switch {
	case errors.Is(err, approval.ErrInRepository):
		return nil, approval.Repository{}, inRepo()
	case err != nil:
		return nil, approval.Repository{}, operational(cmd, "the approval store cannot be found: set "+approval.EnvConfigDir+" to an absolute path")
	}
	id, err := gate.Identify(root)
	if err != nil {
		return nil, approval.Repository{}, operational(cmd, "git could not say which clone the repository belongs to")
	}
	if store.InClone(id) {
		return nil, approval.Repository{}, inRepo()
	}
	return store, id, nil
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
			store, id, err := openStore(cmd, repo.Root)
			if err != nil {
				return err
			}
			status, err := store.Status(id, filepath.Join(repo.Root, policy.FileName))
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
