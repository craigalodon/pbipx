package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/gate"
)

// allowedSentinel is planted in the policy's [values] allow list. Neither
// the message nor the store may hold it.
const allowedSentinel = "Example, Pat"

// approvedRepo is a clean repository whose policy allows allowedSentinel,
// approved in a fresh store.
func approvedRepo(t *testing.T) (root string, store *approval.Store) {
	t.Helper()
	policy := strings.Replace(defaultPolicy(t), `allow = ["(none selected)"]`,
		`allow = ["(none selected)", "`+allowedSentinel+`"]`, 1)
	if !strings.Contains(policy, allowedSentinel) {
		t.Fatal("the sentinel was not planted in the policy")
	}
	files := cleanFiles()
	files[".pbipx.toml"] = policy
	root = newRepo(t, files)
	store = &approval.Store{Dir: t.TempDir()}
	digest, err := approval.DigestFile(filepath.Join(root, ".pbipx.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Approve(root, digest, time.Now()); err != nil {
		t.Fatal(err)
	}
	return root, store
}

func TestRequireApproval(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, root string, store *approval.Store) *approval.Store
		reason string
	}{
		{"approved", nil, ""},
		{"unapproved", func(t *testing.T, _ string, _ *approval.Store) *approval.Store {
			return &approval.Store{Dir: t.TempDir()}
		}, gate.ReasonPolicyUnapproved},
		{"changed", func(t *testing.T, root string, store *approval.Store) *approval.Store {
			appendFile(t, filepath.Join(root, ".pbipx.toml"), "# reviewed\n")
			return store
		}, gate.ReasonPolicyUnapproved},
		{"unreadable store", func(t *testing.T, root string, store *approval.Store) *approval.Store {
			key, err := approval.Key(root)
			if err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(store.Dir, "approved"), key, "not a record ["+allowedSentinel)
			return store
		}, gate.ReasonInternalError},
		{"store is a file", func(t *testing.T, _ string, _ *approval.Store) *approval.Store {
			dir := t.TempDir()
			write(t, dir, "approved", "")
			return &approval.Store{Dir: dir}
		}, gate.ReasonInternalError},
		{"broken policy", func(t *testing.T, root string, store *approval.Store) *approval.Store {
			write(t, root, ".pbipx.toml", "version = 2\nallow = [\""+allowedSentinel+"\"]\n")
			return store
		}, gate.ReasonPolicyError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, store := approvedRepo(t)
			if tt.change != nil {
				store = tt.change(t, root, store)
			}
			for _, ev := range []gate.Event{
				{Tool: gate.Other, Cwd: root},
				{Tool: gate.Read, Path: "README.md", Cwd: root},
				{Tool: gate.Shell, Command: "ls", Cwd: root},
			} {
				d := gate.Decide(ev, gate.Options{CheckPolicy: gate.RequireApproval(store)})
				check(t, d, tt.reason)
				if strings.Contains(d.Message, allowedSentinel) || strings.Contains(d.Message, root) ||
					strings.Contains(d.Message, store.Dir) {
					t.Error("the message echoes the policy or a path")
				}
			}
		})
	}
}

// TestRequireApprovalFindsTheStore checks the store found from the
// environment, as the gate command uses it.
func TestRequireApprovalFindsTheStore(t *testing.T) {
	root, store := approvedRepo(t)
	t.Setenv(approval.EnvConfigDir, store.Dir)
	check(t, gate.Decide(gate.Event{Tool: gate.Other, Cwd: root}, gate.Options{CheckPolicy: gate.RequireApproval(nil)}), "")

	t.Setenv(approval.EnvConfigDir, t.TempDir())
	check(t, gate.Decide(gate.Event{Tool: gate.Other, Cwd: root}, gate.Options{CheckPolicy: gate.RequireApproval(nil)}),
		gate.ReasonPolicyUnapproved)

	t.Setenv(approval.EnvConfigDir, "relative")
	check(t, gate.Decide(gate.Event{Tool: gate.Other, Cwd: root}, gate.Options{CheckPolicy: gate.RequireApproval(nil)}),
		gate.ReasonInternalError)
}

// TestApprovalStoreHoldsNoPolicy checks that nothing in the store holds the
// policy's content.
func TestApprovalStoreHoldsNoPolicy(t *testing.T) {
	_, store := approvedRepo(t)
	err := filepath.WalkDir(store.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // G304: the test's own store
		if err != nil {
			return err
		}
		if strings.Contains(string(data), allowedSentinel) || strings.Contains(string(data), "protected_roots") {
			t.Error("the store holds policy content")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // G304: the test's own repository
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
