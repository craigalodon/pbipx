package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/gate"
	"github.com/craigalodon/pbipx/internal/policy"
)

// allowedSentinel is planted in the policy's [values] allow list. No output
// may carry it, or any other policy content.
const allowedSentinel = "Example, Pat"

// approvalPolicy is testPolicy with allowedSentinel allowed.
const approvalPolicy = testPolicy + "\n[values]\nallow = [\"" + allowedSentinel + "\"]\n"

// newApprovalRepo is newRepo with approvalPolicy, and both terminal checks
// passing.
func newApprovalRepo(t *testing.T) string {
	t.Helper()
	root := newRepo(t, "repo")
	writeFile(t, root, policy.FileName, approvalPolicy)
	stdoutIsTerminal = func(io.Writer) bool { return true }
	stdinIsTerminal = func(io.Reader) bool { return true }
	return root
}

// policyLeak names policy content found in the output, or "".
func policyLeak(o outcome) string {
	all := o.stdout + o.stderr
	for _, v := range []string{allowedSentinel, "Example", "protected_roots", "full_name", "cohort_year"} {
		if strings.Contains(all, v) {
			return v
		}
	}
	return o.leaks()
}

func storeStatus(t *testing.T, root string) approval.Status {
	t.Helper()
	s, err := approval.Open()
	if err != nil {
		t.Fatal(err)
	}
	id, err := gate.Identify(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Status(id, filepath.Join(root, policy.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestPolicyApproveRefused(t *testing.T) {
	tests := []struct {
		name          string
		env           string
		stdin, stdout bool
		want          string
	}{
		{"CI", "CI=true", true, true, "CI is set"},
		{"empty CI", "CI=", true, true, "CI is set"},
		{"PRE_COMMIT", "PRE_COMMIT=1", true, true, "PRE_COMMIT is set"},
		{"empty PRE_COMMIT", "PRE_COMMIT=", true, true, "PRE_COMMIT is set"},
		{"PBIPX_NO_REVEAL", "PBIPX_NO_REVEAL=1", true, true, "PBIPX_NO_REVEAL is set"},
		{"empty PBIPX_NO_REVEAL", "PBIPX_NO_REVEAL=", true, true, "PBIPX_NO_REVEAL is set"},
		{"stdin not a terminal", "", false, true, "standard input is not a terminal"},
		{"stdout not a terminal", "", true, false, "standard output is not a terminal"},
		{"neither a terminal", "", false, false, "not a terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newApprovalRepo(t)
			stdinIsTerminal = func(io.Reader) bool { return tt.stdin }
			stdoutIsTerminal = func(io.Writer) bool { return tt.stdout }
			if tt.env != "" {
				name, value, _ := strings.Cut(tt.env, "=")
				t.Setenv(name, value)
			}
			o := runIn(t, approveWord+"\n", "policy", "approve")
			if o.code != finding.ExitUsage {
				t.Errorf("exit = %d, want %d", o.code, finding.ExitUsage)
			}
			if o.stdout != "" || !strings.Contains(o.stderr, "policy approve is refused") || !strings.Contains(o.stderr, tt.want) {
				t.Errorf("stdout %q, stderr %q", o.stdout, o.stderr)
			}
			if st := storeStatus(t, root); st != approval.Unapproved {
				t.Errorf("a refused approval left the policy %s", st)
			}
		})
	}
}

func TestPolicyApprove(t *testing.T) {
	root := newApprovalRepo(t)
	o := runIn(t, approveWord+"\n", "policy", "approve")
	if o.code != finding.ExitClean {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	digest := approval.Digest([]byte(approvalPolicy))
	if !strings.Contains(o.stdout, digest) || !strings.Contains(o.stdout, "policy  "+policy.FileName+"\n") {
		t.Errorf("the prompt does not show the digest and the policy's path: %q", o.stdout)
	}
	if strings.Contains(o.stdout+o.stderr, root) {
		t.Error("the output names the repository's absolute path")
	}
	if v := policyLeak(o); v != "" {
		t.Errorf("the output carries policy content %q", v)
	}
	if st := storeStatus(t, root); st != approval.Approved {
		t.Errorf("after approval the policy is %s", st)
	}

	// The prompt shows a change since the last approval.
	writeFile(t, root, policy.FileName, approvalPolicy+"# edited\n")
	o = runIn(t, "\n", "policy", "approve")
	if !strings.Contains(o.stdout, "status  changed\n") {
		t.Errorf("the prompt does not show the change: %q", o.stdout)
	}
	if st := storeStatus(t, root); st != approval.Changed {
		t.Errorf("an aborted approval left the policy %s", st)
	}
}

func TestPolicyApproveNeedsTheWordInFull(t *testing.T) {
	for _, input := range []string{"", "\n", "y\n", "yes\n", "appro\n", "APPROVE\n", "approve it\n", "approved\n"} {
		t.Run(input, func(t *testing.T) {
			root := newApprovalRepo(t)
			o := runIn(t, input, "policy", "approve")
			if o.code != finding.ExitUsage {
				t.Errorf("exit = %d, want %d", o.code, finding.ExitUsage)
			}
			if !strings.Contains(o.stderr, "the policy was not approved") {
				t.Errorf("stderr = %q", o.stderr)
			}
			if st := storeStatus(t, root); st != approval.Unapproved {
				t.Errorf("the policy is %s", st)
			}
			if v := policyLeak(o); v != "" {
				t.Errorf("the output carries policy content %q", v)
			}
		})
	}
}

func TestPolicyApproveBrokenPolicy(t *testing.T) {
	root := newApprovalRepo(t)
	writeFile(t, root, policy.FileName, approvalPolicy+"unknown = [\""+allowedSentinel+"\"]\n")
	o := runIn(t, approveWord+"\n", "policy", "approve")
	if o.code != finding.ExitOperational || !strings.HasPrefix(o.stderr, "pbipx: policy-error:") {
		t.Errorf("exit = %d, stderr %q; want a policy-error", o.code, o.stderr)
	}
	if strings.Contains(o.stdout, approveWord) {
		t.Error("a broken policy was offered for approval")
	}
	if st := storeStatus(t, root); st != approval.Unapproved {
		t.Errorf("the policy is %s", st)
	}
	if v := policyLeak(o); v != "" {
		t.Errorf("the output carries policy content %q", v)
	}

	if err := os.Remove(filepath.Join(root, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	if o := runIn(t, approveWord+"\n", "policy", "approve"); o.code != finding.ExitOperational {
		t.Errorf("a missing policy: exit = %d", o.code)
	}
}

func TestPolicyCommandsOutsideARepository(t *testing.T) {
	isolate(t)
	stdoutIsTerminal = func(io.Writer) bool { return true }
	stdinIsTerminal = func(io.Reader) bool { return true }
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)
	for _, args := range [][]string{{"policy", "approve"}, {"policy", "status"}} {
		o := runIn(t, approveWord+"\n", args...)
		if o.code != finding.ExitOperational || !strings.HasPrefix(o.stderr, "pbipx: not-a-repository:") {
			t.Errorf("%v: exit %d, stderr %q", args, o.code, o.stderr)
		}
	}
}

func TestPolicyStatus(t *testing.T) {
	root := newApprovalRepo(t)
	status := func(want string, code int) {
		t.Helper()
		o := runIn(t, "", "policy", "status")
		if o.code != code || o.stdout != policy.FileName+": "+want+"\n" {
			t.Errorf("exit %d, stdout %q; want %d and %s", o.code, o.stdout, code, want)
		}
		if v := policyLeak(o); v != "" {
			t.Errorf("the output carries policy content %q", v)
		}
	}
	status("unapproved", finding.ExitOperational)
	if o := runIn(t, approveWord+"\n", "policy", "approve"); o.code != finding.ExitClean {
		t.Fatalf("approve: exit %d", o.code)
	}
	status("approved", finding.ExitClean)
	writeFile(t, root, policy.FileName, approvalPolicy+"\n")
	status("changed", finding.ExitOperational)
	if err := os.Remove(filepath.Join(root, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	status("missing", finding.ExitOperational)
}

func TestPolicyStatusUnreadableStore(t *testing.T) {
	newApprovalRepo(t)
	t.Setenv(approval.EnvConfigDir, "relative")
	o := runIn(t, "", "policy", "status")
	if o.code != finding.ExitOperational || !strings.Contains(o.stderr, approval.EnvConfigDir) {
		t.Errorf("exit %d, stderr %q", o.code, o.stderr)
	}
}

// TestGateRequiresApproval runs the gate command against an unapproved, an
// approved and a changed policy.
func TestGateRequiresApproval(t *testing.T) {
	root := newApprovalRepo(t)
	event, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"cwd":             root,
		"tool_name":       "Read",
		"tool_input":      map[string]string{"file_path": filepath.Join(root, "README.md")},
	})
	if err != nil {
		t.Fatal(err)
	}
	gateRun := func(reason string) {
		t.Helper()
		o := runIn(t, string(event), "gate")
		switch {
		case reason == "" && o.code != 0:
			t.Errorf("exit %d, stderr %q; want an allow", o.code, o.stderr)
		case reason != "" && (o.code != 2 || !strings.HasPrefix(o.stderr, "pbipx: "+reason+":")):
			t.Errorf("exit %d, stderr %q; want a deny with %s", o.code, o.stderr, reason)
		}
		if strings.Contains(o.stdout+o.stderr, root) {
			t.Error("the gate echoes a path from the event")
		}
		if v := policyLeak(o); v != "" {
			t.Errorf("the output carries policy content %q", v)
		}
	}
	gateRun("policy-unapproved")
	if o := runIn(t, approveWord+"\n", "policy", "approve"); o.code != finding.ExitClean {
		t.Fatalf("approve: exit %d", o.code)
	}
	gateRun("")
	writeFile(t, root, policy.FileName, approvalPolicy+"# edited\n")
	gateRun("policy-unapproved")
	writeFile(t, root, policy.FileName, "version = 2\n")
	gateRun("policy-error")
	t.Setenv(approval.EnvConfigDir, "relative")
	writeFile(t, root, policy.FileName, approvalPolicy)
	gateRun("internal-error")
}

// TestPolicyCommandsRefuseAStoreInTheRepository checks that approve and
// status exit 3, writing nothing, when PBIPX_CONFIG_DIR is inside the
// repository.
func TestPolicyCommandsRefuseAStoreInTheRepository(t *testing.T) {
	root := newApprovalRepo(t)
	dir := filepath.Join(root, "config")
	t.Setenv(approval.EnvConfigDir, dir)
	for _, args := range [][]string{{"policy", "approve"}, {"policy", "status"}} {
		o := runIn(t, approveWord+"\n", args...)
		if o.code != finding.ExitOperational || !strings.Contains(o.stderr, "inside the repository") {
			t.Errorf("%v: exit %d, stderr %q", args, o.code, o.stderr)
		}
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("an approval was written inside the repository")
	}
}

// finalMessage is what a successful unenrolment ends with.
const finalMessage = "pbipx: this clone is no longer set up for the gate on this machine. " +
	"The hook stays registered for other repositories. pbipx init or policy approve sets it up again.\n"

// enrolledRepo is newApprovalRepo with its policy approved and then removed:
// adopted by its record alone, as a person leaves it to unenroll.
func enrolledRepo(t *testing.T) string {
	t.Helper()
	root := newApprovalRepo(t)
	if o := runIn(t, approveWord+"\n", "policy", "approve"); o.code != finding.ExitClean {
		t.Fatalf("approve: exit %d, stderr %q", o.code, o.stderr)
	}
	if err := os.Remove(filepath.Join(root, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	return root
}

func storeAdopted(t *testing.T, root string) bool {
	t.Helper()
	s, err := approval.Open()
	if err != nil {
		t.Fatal(err)
	}
	by, err := (&gate.Adoption{Store: s}).AdoptedBy(root)
	if err != nil {
		t.Fatal(err)
	}
	return by != nil
}

func recordFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(os.Getenv(approval.EnvConfigDir), "approved"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestPolicyUnenrollRefused(t *testing.T) {
	tests := []struct {
		name          string
		env           string
		stdin, stdout bool
		want          string
	}{
		{"CI", "CI=true", true, true, "CI is set"},
		{"empty CI", "CI=", true, true, "CI is set"},
		{"PRE_COMMIT", "PRE_COMMIT=1", true, true, "PRE_COMMIT is set"},
		{"empty PRE_COMMIT", "PRE_COMMIT=", true, true, "PRE_COMMIT is set"},
		{"PBIPX_NO_REVEAL", "PBIPX_NO_REVEAL=1", true, true, "PBIPX_NO_REVEAL is set"},
		{"empty PBIPX_NO_REVEAL", "PBIPX_NO_REVEAL=", true, true, "PBIPX_NO_REVEAL is set"},
		{"stdin not a terminal", "", false, true, "standard input is not a terminal"},
		{"stdout not a terminal", "", true, false, "standard output is not a terminal"},
		{"neither a terminal", "", false, false, "not a terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := enrolledRepo(t)
			stdinIsTerminal = func(io.Reader) bool { return tt.stdin }
			stdoutIsTerminal = func(io.Writer) bool { return tt.stdout }
			if tt.env != "" {
				name, value, _ := strings.Cut(tt.env, "=")
				t.Setenv(name, value)
			}
			o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
			if o.code != finding.ExitUsage {
				t.Errorf("exit = %d, want %d", o.code, finding.ExitUsage)
			}
			if o.stdout != "" || !strings.Contains(o.stderr, "policy unenroll is refused") || !strings.Contains(o.stderr, tt.want) {
				t.Errorf("stdout %q, stderr %q", o.stdout, o.stderr)
			}
			if !storeAdopted(t, root) {
				t.Error("a refused unenrolment removed the adoption")
			}
		})
	}
}

func TestPolicyUnenroll(t *testing.T) {
	root := enrolledRepo(t)
	if !storeAdopted(t, root) {
		t.Fatal("not adopted to begin with")
	}
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitClean {
		t.Fatalf("exit = %d, stderr %q", o.code, o.stderr)
	}
	for _, want := range []string{
		os.Getenv(approval.EnvConfigDir), root, "digests  1\n",
		"stops protecting", "every approval", "Type \"unenroll\"",
	} {
		if !strings.Contains(o.stdout, want) {
			t.Errorf("the prompt lacks %q: %q", want, o.stdout)
		}
	}
	if !strings.HasSuffix(o.stdout, finalMessage) {
		t.Errorf("the output does not end with the final message: %q", o.stdout)
	}
	if storeAdopted(t, root) || len(recordFiles(t)) != 0 {
		t.Errorf("still adopted, or records left: %v", recordFiles(t))
	}
	if v := policyLeak(o); v != "" {
		t.Errorf("the output carries policy content %q", v)
	}
}

func TestPolicyUnenrollNeedsTheWordInFull(t *testing.T) {
	for _, input := range []string{"", "\n", "y\n", "yes\n", "unenrol\n", "UNENROLL\n", "unenroll it\n", "unenrolled\n", "approve\n"} {
		t.Run(input, func(t *testing.T) {
			root := enrolledRepo(t)
			o := runIn(t, input, "policy", "unenroll")
			if o.code != finding.ExitUsage {
				t.Errorf("exit = %d, want %d", o.code, finding.ExitUsage)
			}
			if !strings.Contains(o.stderr, "adoption was not removed") || strings.Contains(o.stdout, "no longer set up") {
				t.Errorf("stdout %q, stderr %q", o.stdout, o.stderr)
			}
			if !storeAdopted(t, root) {
				t.Error("a declined prompt removed the adoption")
			}
			if v := policyLeak(o); v != "" {
				t.Errorf("the output carries policy content %q", v)
			}
		})
	}
}

func TestPolicyUnenrollRefusesWhilePolicyIsPresent(t *testing.T) {
	root := enrolledRepo(t)
	writeFile(t, root, policy.FileName, approvalPolicy)
	before := recordFiles(t)
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitOperational {
		t.Errorf("exit = %d, want %d", o.code, finding.ExitOperational)
	}
	for _, want := range []string{policy.FileName, "adopt", "policy-unapproved"} {
		if !strings.Contains(o.stderr, want) {
			t.Errorf("stderr lacks %q: %q", want, o.stderr)
		}
	}
	if strings.Contains(o.stdout, "Type") || !slices.Equal(recordFiles(t), before) || !storeAdopted(t, root) {
		t.Errorf("something was asked or removed: stdout %q", o.stdout)
	}
	if v := policyLeak(o); v != "" {
		t.Errorf("the output carries policy content %q", v)
	}
	// A broken policy file is a file too.
	writeFile(t, root, policy.FileName, "unknown = [\""+allowedSentinel+"\"]\n")
	o = runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitOperational || !slices.Equal(recordFiles(t), before) {
		t.Errorf("a broken policy: exit %d", o.code)
	}
	if v := policyLeak(o); v != "" {
		t.Errorf("the output carries policy content %q", v)
	}
}

func TestPolicyUnenrollNeedsNoPolicy(t *testing.T) {
	// The policy is not loaded: a missing one does not block it, and the
	// work tree is not a policy's to judge.
	root := enrolledRepo(t)
	if _, err := os.Stat(filepath.Join(root, policy.FileName)); err == nil {
		t.Fatal("the policy is there")
	}
	if o := runIn(t, unenrollWord+"\n", "policy", "unenroll"); o.code != finding.ExitClean {
		t.Errorf("exit %d, stderr %q", o.code, o.stderr)
	}
}

func TestPolicyUnenrollNothingToRemove(t *testing.T) {
	root := newRepo(t, "repo")
	stdoutIsTerminal = func(io.Writer) bool { return true }
	stdinIsTerminal = func(io.Reader) bool { return true }
	if err := os.Remove(filepath.Join(root, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	o := runIn(t, "", "policy", "unenroll")
	if o.code != finding.ExitClean || !strings.Contains(o.stdout, "nothing to remove") || strings.Contains(o.stdout, "Type") {
		t.Errorf("exit %d, stdout %q, stderr %q", o.code, o.stdout, o.stderr)
	}
	if len(recordFiles(t)) != 0 {
		t.Error("a record was written")
	}
}

func TestPolicyUnenrollRefusesAStoreInTheRepository(t *testing.T) {
	root := enrolledRepo(t)
	dir := filepath.Join(root, "config")
	t.Setenv(approval.EnvConfigDir, dir)
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitOperational || !strings.Contains(o.stderr, "inside the repository") {
		t.Errorf("exit %d, stderr %q", o.code, o.stderr)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("a store was made inside the repository")
	}
}

func TestPolicyUnenrollStoreErrors(t *testing.T) {
	enrolledRepo(t)
	t.Setenv(approval.EnvConfigDir, "relative")
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitOperational || !strings.Contains(o.stderr, approval.EnvConfigDir) {
		t.Errorf("exit %d, stderr %q", o.code, o.stderr)
	}
}

func TestPolicyUnenrollStoreUnderAFile(t *testing.T) {
	enrolledRepo(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(approval.EnvConfigDir, filepath.Join(file, "pbipx"))
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitOperational || strings.Contains(o.stdout, "no longer set up") {
		t.Errorf("exit %d, stdout %q, stderr %q", o.code, o.stdout, o.stderr)
	}
}

func TestPolicyUnenrollStoreWriteFailureLeavesItAdopted(t *testing.T) {
	root := enrolledRepo(t)
	// A directory where a record belongs cannot be read as one.
	bad := filepath.Join(os.Getenv(approval.EnvConfigDir), "approved", strings.Repeat("a", 64))
	if err := os.Mkdir(bad, 0o700); err != nil { //nolint:gosec // G703: a directory in the test's own temporary store
		t.Fatal(err)
	}
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitOperational || strings.Contains(o.stdout, "no longer set up") {
		t.Errorf("exit %d, stdout %q, stderr %q", o.code, o.stdout, o.stderr)
	}
	// Not the same as storeAdopted: the unreadable index is the failure.
	s, _ := approval.Open()
	id, err := gate.Identify(root)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Adopted(id); !ok {
		t.Error("the clone's record was removed despite the error")
	}
}

func TestPolicyUnenrollLinkedWorkTrees(t *testing.T) {
	root := newApprovalRepo(t)
	if err := corpus.AddWorktree(root, ".claude/worktrees/agent"); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, ".claude", "worktrees", "agent")
	// The linked work tree has the policy file, committed. It is adopted
	// by that, whatever unenrolling here does: a warning, not a block.
	if o := runIn(t, approveWord+"\n", "policy", "approve"); o.code != finding.ExitClean {
		t.Fatalf("approve: exit %d", o.code)
	}
	// Record the linked work tree in the index too, as an approval there does.
	s, err := approval.Open()
	if err != nil {
		t.Fatal(err)
	}
	id, err := gate.Identify(linked)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Adopt(id); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitClean {
		t.Fatalf("exit %d, stdout %q, stderr %q", o.code, o.stdout, o.stderr)
	}
	for _, want := range []string{root, linked, "work trees   2"} {
		if !strings.Contains(o.stdout, want) {
			t.Errorf("the output lacks %q: %q", want, o.stdout)
		}
	}
	if !strings.Contains(o.stdout, "warning") || strings.Count(o.stdout, policy.FileName) == 0 {
		t.Errorf("no warning that the linked work tree holds a policy file: %q", o.stdout)
	}
	if len(recordFiles(t)) != 0 {
		t.Errorf("records left: %v", recordFiles(t))
	}
	roots, _, err := s.Roots()
	if err != nil || len(roots) != 0 {
		t.Errorf("Roots = %v, %v; want none", roots, err)
	}
	if v := policyLeak(o); v != "" {
		t.Errorf("the output carries policy content %q", v)
	}
}

func TestPolicyUnenrollFromALinkedWorkTree(t *testing.T) {
	root := newApprovalRepo(t)
	if o := runIn(t, approveWord+"\n", "policy", "approve"); o.code != finding.ExitClean {
		t.Fatalf("approve: exit %d", o.code)
	}
	if err := os.Remove(filepath.Join(root, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := corpus.AddWorktree(root, ".claude/worktrees/agent"); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, ".claude", "worktrees", "agent")
	// The linked one holds the committed policy; take it out of the way.
	if err := os.Remove(filepath.Join(linked, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	t.Chdir(linked)
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitClean || !strings.Contains(o.stdout, root) {
		t.Fatalf("exit %d, stdout %q, stderr %q", o.code, o.stdout, o.stderr)
	}
	if len(recordFiles(t)) != 0 {
		t.Errorf("records left: %v", recordFiles(t))
	}
}

func TestPolicyUnenrollEnclosingAdoptedRepository(t *testing.T) {
	root := newApprovalRepo(t)
	if o := runIn(t, approveWord+"\n", "policy", "approve"); o.code != finding.ExitClean {
		t.Fatalf("approve: exit %d", o.code)
	}
	nested := filepath.Join(root, "vendor", "lib")
	writeFile(t, nested, "README.md", "A vendored library.\n")
	if err := corpus.GitSetup(nested, nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	// The nested repository adopts too, then is unenrolled.
	s, err := approval.Open()
	if err != nil {
		t.Fatal(err)
	}
	id, err := gate.Identify(nested)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Adopt(id); err != nil {
		t.Fatal(err)
	}
	o := runIn(t, unenrollWord+"\n", "policy", "unenroll")
	if o.code != finding.ExitOperational || strings.Contains(o.stdout, "no longer set up") {
		t.Fatalf("exit %d, stdout %q, stderr %q", o.code, o.stdout, o.stderr)
	}
	if !strings.Contains(o.stderr, "inside") || !strings.Contains(o.stderr, "adopted") {
		t.Errorf("stderr does not name the enclosing repository: %q", o.stderr)
	}
	// Its own adoption is gone all the same: the enclosing one's record stays.
	if ok, _ := s.Adopted(id); ok {
		t.Error("the nested repository's own record is still there")
	}
	if len(recordFiles(t)) != 1 {
		t.Errorf("records = %v; want the enclosing repository's alone", recordFiles(t))
	}
}

func TestPolicyUnenrollThenInitAdoptsAgain(t *testing.T) {
	e := newInitRepo(t, "pbip/Example.Report/definition.pbir")
	stdoutIsTerminal = func(io.Writer) bool { return true }
	stdinIsTerminal = func(io.Reader) bool { return true }
	if o := runIn(t, "", "init"); o.code != 0 {
		t.Fatalf("init: exit %d, stderr %q", o.code, o.stderr)
	}
	if err := os.Remove(filepath.Join(e.root, policy.FileName)); err != nil {
		t.Fatal(err)
	}
	if o := runIn(t, unenrollWord+"\n", "policy", "unenroll"); o.code != finding.ExitClean {
		t.Fatalf("unenroll: exit %d, stderr %q", o.code, o.stderr)
	}
	event := readEvent(t, e.root)
	if o := runIn(t, event, "gate"); o.code != 0 {
		t.Errorf("gate after unenroll: exit %d, stderr %q", o.code, o.stderr)
	}
	if o := runIn(t, "", "init"); o.code != 0 {
		t.Fatalf("init again: exit %d, stderr %q", o.code, o.stderr)
	}
	if o := runIn(t, event, "gate"); o.code != 2 || !strings.HasPrefix(o.stderr, "pbipx: policy-unapproved:") {
		t.Errorf("gate after init again: exit %d, stderr %q", o.code, o.stderr)
	}
}

// TestUnenrollEndToEnd: after unenrolling, the gate allows; with the policy
// restored it denies with policy-unapproved, since no approval survives and a
// policy file adopts the repository again.
func TestUnenrollEndToEnd(t *testing.T) {
	root := enrolledRepo(t)
	opts := gate.Options{CheckPolicy: gate.RequireApproval(nil), Adoption: &gate.Adoption{}}
	ev := gate.Event{Tool: gate.Read, Path: filepath.Join(root, "README.md"), Cwd: root}
	if d := gate.Decide(ev, opts); d.Allow || d.Reason != gate.ReasonPolicyError {
		t.Fatalf("before: %+v; want a deny (adopted, policy gone)", d)
	}
	if o := runIn(t, unenrollWord+"\n", "policy", "unenroll"); o.code != finding.ExitClean {
		t.Fatalf("unenroll: exit %d, stderr %q", o.code, o.stderr)
	}
	if d := gate.Decide(ev, opts); !d.Allow {
		t.Errorf("after unenroll: %+v; want an allow", d)
	}
	writeFile(t, root, policy.FileName, approvalPolicy)
	if d := gate.Decide(ev, opts); d.Allow || d.Reason != gate.ReasonPolicyUnapproved {
		t.Errorf("after the policy is back: %+v; want policy-unapproved", d)
	}
}

// TestSubcommandsAreInTheGatesTables is the drift guard: every command the
// tool has is either on the gate's table of what an agent may run or on its
// list of a person's steps, so a new subcommand is closed to agents until
// someone decides.
func TestSubcommandsAreInTheGatesTables(t *testing.T) {
	agent := gate.AgentSubcommands()
	person := gate.PersonOnlyCommands()
	root := newRootCmd()
	have := map[string]bool{}
	for _, c := range root.Commands() {
		name := c.Name()
		have[name] = true
		switch {
		case name == "policy":
			continue
		case slices.Contains(agent, name), slices.Contains(person, name):
		default:
			t.Errorf("command %q is in neither the gate's agent table nor its person-only list", name)
		}
	}
	for _, name := range append(slices.Clone(agent), person...) {
		if !strings.HasPrefix(name, "-") && !have[name] && name != "help" { // help is cobra's, added when the command runs
			t.Errorf("the gate's tables name %q, which is not a command", name)
		}
	}
	var policyCmd *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "policy" {
			policyCmd = c
		}
	}
	if policyCmd == nil {
		t.Fatal("no policy command")
	}
	sub := map[string]bool{}
	for _, c := range policyCmd.Commands() {
		sub[c.Name()] = true
		if c.Name() == "help" {
			continue
		}
		if !slices.Contains(gate.AgentPolicySubcommands(), c.Name()) && !slices.Contains(gate.PersonOnlyPolicySubcommands(), c.Name()) {
			t.Errorf("policy %q is in neither the gate's agent table nor its person-only list", c.Name())
		}
	}
	for _, name := range append(gate.AgentPolicySubcommands(), gate.PersonOnlyPolicySubcommands()...) {
		if !sub[name] {
			t.Errorf("the gate's tables name policy %q, which is not a command", name)
		}
	}
}
