package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/finding"
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
	st, err := s.Status(root, filepath.Join(root, policy.FileName))
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
