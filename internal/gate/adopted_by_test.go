package gate_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/gate"
)

func adoptedBy(t *testing.T, store *approval.Store, root string) *gate.AdoptedBy {
	t.Helper()
	got, err := (&gate.Adoption{Store: store}).AdoptedBy(root)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAdoptedBy(t *testing.T) {
	ctl := newControlDirs(t)
	store := &approval.Store{Dir: ctl.store}

	plain := notAdoptedRepo(t, cleanFiles())
	if got := adoptedBy(t, store, plain); got != nil {
		t.Errorf("a repository that never adopted: %+v", got)
	}

	withPolicy := newRepo(t, cleanFiles())
	if got := adoptedBy(t, store, withPolicy); got == nil || got.Kind != gate.ByPolicyFile {
		t.Errorf("a policy file: %+v", got)
	}

	// A record of the clone, no policy file.
	approveIn(t, store, withPolicy)
	removePolicy(t, withPolicy)
	if got := adoptedBy(t, store, withPolicy); got == nil || got.Kind != gate.ByIndex {
		t.Errorf("a record: %+v", got)
	}

	// The work tree in another record's roots alone.
	other := notAdoptedRepo(t, cleanFiles())
	key, err := approval.Key(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(store.Dir, "approved")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key), []byte("version = 3\nroots = ['"+other+"']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := adoptedBy(t, store, other); got == nil || got.Kind != gate.ByIndex {
		t.Errorf("an indexed work tree: %+v", got)
	}
}

func TestAdoptedByEnclosing(t *testing.T) {
	ctl := newControlDirs(t)
	store := &approval.Store{Dir: ctl.store}
	root := newRepo(t, cleanFiles())
	approveIn(t, store, root)
	nested := filepath.Join(root, "vendor", "lib")
	write(t, nested, "README.md", "A vendored library.\n")
	if err := corpus.GitSetup(nested, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := adoptedBy(t, store, nested)
	if got == nil || got.Kind != gate.ByEnclosing || got.Root != root {
		t.Errorf("a nested repository: %+v, want enclosed by the root", got)
	}
}

func TestAdoptedByAnUnreadableIndex(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	root := notAdoptedRepo(t, cleanFiles())
	if _, err := (&gate.Adoption{Store: &approval.Store{Dir: filepath.Join(file, "x")}}).AdoptedBy(root); err == nil {
		t.Error("no error for a store under a file")
	}
}

func TestWorkTrees(t *testing.T) {
	root := newRepo(t, cleanFiles())
	if err := corpus.AddWorktree(root, ".claude/worktrees/agent"); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, ".claude", "worktrees", "agent")
	for _, from := range []string{root, linked} {
		got := gate.WorkTrees(from)
		if got[0] != from || len(got) != 2 || !slices.Contains(got, root) || !slices.Contains(got, linked) {
			t.Errorf("WorkTrees(%s) = %v", filepath.Base(from), got)
		}
	}
}

func TestExportedTables(t *testing.T) {
	if !slices.Contains(gate.AgentSubcommands(), "scan") || slices.Contains(gate.AgentSubcommands(), "init") {
		t.Errorf("AgentSubcommands = %v", gate.AgentSubcommands())
	}
	if !slices.Equal(gate.PersonOnlyCommands(), []string{"gate", "init"}) {
		t.Errorf("PersonOnlyCommands = %v", gate.PersonOnlyCommands())
	}
	if !slices.Equal(gate.PersonOnlyPolicySubcommands(), []string{"approve", "unenroll"}) {
		t.Errorf("PersonOnlyPolicySubcommands = %v", gate.PersonOnlyPolicySubcommands())
	}
	if !slices.Equal(gate.AgentPolicySubcommands(), []string{"status"}) {
		t.Errorf("AgentPolicySubcommands = %v", gate.AgentPolicySubcommands())
	}
}
