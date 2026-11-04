package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/gate"
	"github.com/craigalodon/pbipx/internal/policy"
)

// unenrollWord is what a person types, in full, to remove an adoption.
const unenrollWord = "unenroll"

func newPolicyUnenrollCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unenroll",
		Short: "remove this clone's setup for the gate, at a terminal",
		Long: `Remove this clone's setup for the gate on this machine, for every one of
its work trees (ADR-0006).

It removes the clone's approval record, every approval in it, and the work
trees' place in the index of adopted repositories. The gate then stops
protecting these files from agents, until pbipx init or policy approve sets
the clone up again. The hook stays registered for other repositories.

It shows the approval store's directory, the work trees and the number of
approvals, and asks for "unenroll" to be typed in full. It is refused, with
a usage error, unless standard input and standard output are terminals, and
when CI, PRE_COMMIT or PBIPX_NO_REVEAL is set. It does not load the policy.
It is refused, exit 3, while this work tree has a .pbipx.toml: that file
would adopt the repository again, and the gate would deny every call until
the policy is approved. Remove or move the file first.

A mismatched PBIPX_CONFIG_DIR cannot be detected: check the store directory
shown is the one the gate reads.`,
		Args: cobra.NoArgs,
		RunE: runPolicyUnenroll,
	}
}

func runPolicyUnenroll(cmd *cobra.Command, _ []string) error {
	if err := personRefusal("policy unenroll", cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
		return err
	}
	_, repo, err := openRepo(cmd, formatHuman)
	if err != nil {
		return err
	}
	store, id, err := openStore(cmd, repo.Root)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(repo.Root, policy.FileName)); err == nil {
		return operational(cmd, policy.FileName+" is in this work tree: it would adopt the repository again, and the gate would then deny every call "+
			"with policy-unapproved until a person approves it. Nothing was removed; remove or move the file first")
	}
	trees := gate.WorkTrees(repo.Root)
	adoption := &gate.Adoption{Store: store}
	out := cmd.OutOrStdout()

	by, err := adoption.AdoptedBy(repo.Root)
	if err != nil {
		return operational(cmd, "the approval store cannot be read")
	}
	held, err := store.Holds(id, trees)
	if err != nil {
		return operational(cmd, "the approval store cannot be read")
	}
	if by == nil && !held {
		fmt.Fprintln(out, "pbipx: this clone is not set up for the gate on this machine; nothing to remove")
		return nil
	}

	digests, _, err := store.Recorded(id)
	count := fmt.Sprint(len(digests))
	if err != nil {
		count = "0 (a record cannot be read)"
	}
	fmt.Fprintf(out, "store        %s\nwork trees   %d\n", store.Dir, len(trees))
	for _, t := range trees {
		fmt.Fprintf(out, "  %s\n", t)
	}
	fmt.Fprintf(out, "digests  %s\n\n", count)
	for _, t := range trees[1:] {
		if _, err := os.Lstat(filepath.Join(t, policy.FileName)); err == nil {
			fmt.Fprintf(out, "warning: the linked work tree %s has a %s, which adopts it again whatever is removed here\n", t, policy.FileName)
		}
	}
	fmt.Fprint(out, "After this the gate stops protecting these files from agents in these work trees, "+
		"and every approval recorded for this clone is forgotten.\n\n")
	fmt.Fprintf(out, "Type %q to remove this clone's setup for the gate on this machine: ", unenrollWord)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if strings.TrimSpace(line) != unenrollWord {
		fmt.Fprintln(out)
		return usageError{"the adoption was not removed"}
	}

	if err := store.Unenroll(id, trees); err != nil {
		return operational(cmd, "the adoption cannot be removed from the approval store; the repository may still be adopted")
	}
	by, err = adoption.AdoptedBy(repo.Root)
	if err != nil {
		return operational(cmd, "the approval store cannot be read; the repository may still be adopted")
	}
	if by != nil {
		return operational(cmd, "this work tree is still adopted: "+stillAdopted(by))
	}
	fmt.Fprintln(out, "pbipx: this clone is no longer set up for the gate on this machine. "+
		"The hook stays registered for other repositories. pbipx init or policy approve sets it up again.")
	return nil
}

// stillAdopted says why the gate still acts for a work tree.
func stillAdopted(by *gate.AdoptedBy) string {
	switch by.Kind {
	case gate.ByPolicyFile:
		return "it has a " + policy.FileName
	case gate.ByIndex:
		return "it is still in the approval store's index of adopted work trees"
	case gate.ByRecord:
		return "its clone still has a record in the approval store"
	case gate.ByEnclosing:
		return "it lies inside the adopted repository at " + by.Root + ", which the gate judges its files against"
	}
	return "it lies in an adopted work tree that git cannot open"
}
