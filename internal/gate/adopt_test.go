package gate_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/gate"
)

// adoptionOptions are the gate command's options, on store.
func adoptionOptions(store *approval.Store) gate.Options {
	return gate.Options{CheckPolicy: gate.RequireApproval(store), Adoption: &gate.Adoption{Store: store}}
}

// approveIn approves root's policy in store.
func approveIn(t *testing.T, store *approval.Store, root string) {
	t.Helper()
	digest, err := approval.DigestFile(filepath.Join(root, ".pbipx.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Approve(identify(t, root), digest, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// notAdoptedRepo is newRepo with no policy in the work tree.
func notAdoptedRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := newRepo(t, files)
	if err := os.Remove(filepath.Join(root, ".pbipx.toml")); err != nil {
		t.Fatal(err)
	}
	return root
}

// adoption is a machine with an adopted repository whose protected tree
// leaks, approved in the store; a repository with the same files and no
// policy and no record; and a directory outside every repository.
type adoption struct {
	ctl                     controlDirs
	store                   *approval.Store
	adopted, plain, outside string
}

func newAdoption(t *testing.T) adoption {
	t.Helper()
	a := adoption{ctl: newControlDirs(t)}
	a.store = &approval.Store{Dir: a.ctl.store}
	a.adopted = newRepo(t, leakyFiles())
	approveIn(t, a.store, a.adopted)
	a.plain = notAdoptedRepo(t, leakyFiles())
	a.outside = t.TempDir()
	write(t, a.outside, "notes.txt", "Nothing personal here.\n")
	return a
}

func (a adoption) decide(ev gate.Event) gate.Decision {
	return gate.Decide(ev, adoptionOptions(a.store))
}

// TestAdoptionScopesTheGate checks that the gate acts for adopted
// repositories only, and judges a call that reaches into one from anywhere
// against it.
func TestAdoptionScopesTheGate(t *testing.T) {
	a := newAdoption(t)
	in := func(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	link := filepath.Join(a.outside, "link")
	if err := os.Symlink(in(a.adopted, "pbip"), link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		// Not adopted: the gate does not act.
		{"not adopted, read of a leak", gate.Event{Tool: gate.Read, Path: leakVisual, Cwd: a.plain}, ""},
		{"not adopted, search", gate.Event{Tool: gate.Search, Cwd: a.plain}, ""},
		{"not adopted, cat of a leak", gate.Event{Tool: gate.Shell, Command: "cat " + leakVisual, Cwd: a.plain}, ""},
		{"not adopted, git log -p", gate.Event{Tool: gate.Shell, Command: "git log -p", Cwd: a.plain}, ""},
		{"not adopted, other tool", gate.Event{Tool: gate.Other, Cwd: a.plain}, ""},
		{"not adopted, unknown word in echo", gate.Event{Tool: gate.Shell, Command: "echo $X", Cwd: a.plain}, ""},
		{"outside, read", gate.Event{Tool: gate.Read, Path: "notes.txt", Cwd: a.outside}, ""},
		{"outside, ls", gate.Event{Tool: gate.Shell, Command: "ls -la", Cwd: a.outside}, ""},
		{"outside, missing working directory", gate.Event{Tool: gate.Other, Cwd: filepath.Join(a.outside, "gone")}, ""},
		{"no working directory", gate.Event{Tool: gate.Other}, gate.ReasonEventError},

		// Adopted: as before.
		{"adopted, read of a leak", gate.Event{Tool: gate.Read, Path: leakVisual, Cwd: a.adopted}, gate.ReasonFinding},
		{"adopted, read of a clean note", gate.Event{Tool: gate.Read, Path: cleanNote, Cwd: a.adopted}, ""},

		// Into an adopted repository from elsewhere: judged there.
		{"absolute read from a plain repository", gate.Event{Tool: gate.Read, Path: in(a.adopted, leakVisual), Cwd: a.plain}, gate.ReasonFinding},
		{"absolute read of a clean note", gate.Event{Tool: gate.Read, Path: in(a.adopted, cleanNote), Cwd: a.plain}, ""},
		{"relative read up into it", gate.Event{Tool: gate.Read, Path: filepath.Join("..", filepath.Base(a.adopted), leakVisual), Cwd: a.plain}, gate.ReasonFinding},
		{"read through a symlink", gate.Event{Tool: gate.Read, Path: filepath.Join(link, "B.Report/definition/pages/0000/visuals/0001/visual.json"), Cwd: a.outside}, gate.ReasonFinding},
		{"search of it", gate.Event{Tool: gate.Search, Path: a.adopted, Cwd: a.outside}, gate.ReasonFinding},
		{"search of a directory holding it", gate.Event{Tool: gate.Search, Path: filepath.Dir(a.adopted), Cwd: a.outside}, gate.ReasonFinding},
		{"cat of an absolute path", gate.Event{Tool: gate.Shell, Command: "cat " + bashPath(in(a.adopted, leakVisual)), Cwd: a.outside}, gate.ReasonFinding},
		{"cd into it, then cat", gate.Event{Tool: gate.Shell, Command: "cd " + bashPath(a.adopted) + " && cat " + leakVisual, Cwd: a.outside}, gate.ReasonFinding},
		{"grep -r of a directory holding it", gate.Event{Tool: gate.Shell, Command: "grep -r Doe " + bashPath(filepath.Dir(a.adopted)), Cwd: a.outside}, gate.ReasonFinding},
		{"cat of a word that cannot be placed", gate.Event{Tool: gate.Shell, Command: `cat "$X"`, Cwd: a.plain}, gate.ReasonFinding},
		{"cat of a clean note in it", gate.Event{Tool: gate.Shell, Command: "cat " + bashPath(in(a.adopted, cleanNote)), Cwd: a.outside}, ""},
		{"write into its agent settings", gate.Event{Tool: gate.Write, Path: in(a.adopted, ".claude/settings.json"), Cwd: a.outside}, gate.ReasonAgentSettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check(t, a.decide(tc.ev), tc.reason)
		})
	}
}

// TestAdoptionStaysClosed checks that an adopted repository cannot be
// switched out of the gate's reach from inside it.
func TestAdoptionStaysClosed(t *testing.T) {
	t.Run("approved policy deleted", func(t *testing.T) {
		a := newAdoption(t)
		removePolicy(t, a.adopted)
		check(t, a.decide(gate.Event{Tool: gate.Read, Path: "README.md", Cwd: a.adopted}), gate.ReasonPolicyError)
		check(t, a.decide(gate.Event{Tool: gate.Read, Path: filepath.Join(a.adopted, "README.md"), Cwd: a.outside}), gate.ReasonPolicyError)
	})
	t.Run("adopted by init, unapproved", func(t *testing.T) {
		a := newAdoption(t)
		root := newRepo(t, cleanFiles())
		if err := a.store.Adopt(identify(t, root)); err != nil {
			t.Fatal(err)
		}
		check(t, a.decide(gate.Event{Tool: gate.Other, Cwd: root}), gate.ReasonPolicyUnapproved)
		removePolicy(t, root)
		check(t, a.decide(gate.Event{Tool: gate.Other, Cwd: root}), gate.ReasonPolicyError)
		// A call elsewhere that does not touch it is not its to deny.
		check(t, a.decide(gate.Event{Tool: gate.Read, Path: "notes.txt", Cwd: a.outside}), "")
	})
	t.Run("git directory removed", func(t *testing.T) {
		a := newAdoption(t)
		if err := os.RemoveAll(filepath.Join(a.adopted, ".git")); err != nil {
			t.Fatal(err)
		}
		check(t, a.decide(gate.Event{Tool: gate.Read, Path: leakVisual, Cwd: a.adopted}), gate.ReasonNotARepository)
		check(t, a.decide(gate.Event{Tool: gate.Shell, Command: "cat " + bashPath(filepath.Join(a.adopted, leakVisual)), Cwd: a.outside}),
			gate.ReasonNotARepository)
	})
	t.Run("working directory in the git directory", func(t *testing.T) {
		a := newAdoption(t)
		check(t, a.decide(gate.Event{Tool: gate.Other, Cwd: filepath.Join(a.adopted, ".git")}), gate.ReasonNotARepository)
	})
	t.Run("working directory removed", func(t *testing.T) {
		a := newAdoption(t)
		check(t, a.decide(gate.Event{Tool: gate.Other, Cwd: filepath.Join(a.adopted, "gone")}), gate.ReasonNotARepository)
	})
	t.Run("linked work tree found by its clone's record", func(t *testing.T) {
		a := newAdoption(t)
		if err := corpus.AddWorktree(a.adopted, "linked"); err != nil {
			t.Fatal(err)
		}
		linked := filepath.Join(a.adopted, "linked")
		removePolicy(t, linked)
		check(t, a.decide(gate.Event{Tool: gate.Other, Cwd: linked}), gate.ReasonPolicyError)
	})
	t.Run("unreadable record", func(t *testing.T) {
		a := newAdoption(t)
		key, err := approval.Key(identify(t, a.plain).GitDir)
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(a.store.Dir, "approved"), key, "not a record")
		// The clone counts as adopted, and has no policy.
		check(t, a.decide(gate.Event{Tool: gate.Other, Cwd: a.plain}), gate.ReasonPolicyError)
	})
	t.Run("unreadable index", func(t *testing.T) {
		a := newAdoption(t)
		store := &approval.Store{Dir: t.TempDir()}
		write(t, store.Dir, "approved", "")
		check(t, gate.Decide(gate.Event{Tool: gate.Other, Cwd: a.outside}, adoptionOptions(store)), gate.ReasonInternalError)
	})
}

// TestAdoptionInnermost checks that a path is judged by the innermost
// adopted work tree that holds it: a linked work tree of the clone alone,
// as before adoption; and the adopted repository around a nested one that
// has not adopted pbipx.
func TestAdoptionInnermost(t *testing.T) {
	ctl := newControlDirs(t)
	store := &approval.Store{Dir: ctl.store}
	root := newRepo(t, cleanFiles())
	approveIn(t, store, root)
	if err := corpus.AddWorktree(root, ".claude/worktrees/agent"); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, ".claude", "worktrees", "agent")
	// A leak in the main work tree, not committed: the linked one is clean.
	write(t, root, leakVisual, slicer("student", "full_name", "Doe, Jane"))
	vendored := filepath.Join(root, "vendor", "lib")
	write(t, vendored, "README.md", "A vendored library.\n")
	if err := corpus.GitSetup(vendored, nil, nil); err != nil {
		t.Fatal(err)
	}
	opts := adoptionOptions(store)
	for _, tc := range []struct {
		name   string
		ev     gate.Event
		reason string
	}{
		{"main work tree, cat", gate.Event{Tool: gate.Shell, Command: "cat " + reportJSON, Cwd: root}, gate.ReasonFinding},
		{"linked work tree, cat", gate.Event{Tool: gate.Shell, Command: "cat " + reportJSON, Cwd: linked}, ""},
		{"linked work tree, search", gate.Event{Tool: gate.Search, Cwd: linked}, ""},
		{"nested repository, read up into the root", gate.Event{Tool: gate.Read, Path: filepath.Join("..", "..", leakVisual), Cwd: vendored}, gate.ReasonFinding},
		{"nested repository, cat up into the root", gate.Event{Tool: gate.Shell, Command: "cat ../../" + reportJSON, Cwd: vendored}, gate.ReasonFinding},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check(t, gate.Decide(tc.ev, opts), tc.reason)
		})
	}
}

func removePolicy(t *testing.T, root string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, ".pbipx.toml")); err != nil {
		t.Fatal(err)
	}
}

// TestControlsFromAnywhere checks that a call made outside every adopted
// repository still may not change what controls the gate.
func TestControlsFromAnywhere(t *testing.T) {
	a := newAdoption(t)
	userSettings := filepath.Join(a.ctl.claude, "settings.json")
	for _, cwd := range []string{a.plain, a.outside} {
		for _, tc := range []struct {
			name   string
			ev     gate.Event
			reason string
		}{
			{"write user settings", gate.Event{Tool: gate.Write, Path: userSettings}, gate.ReasonAgentSettings},
			{"write into the store", gate.Event{Tool: gate.Write, Path: filepath.Join(a.store.Dir, "approved", "x")}, gate.ReasonAgentSettings},
			{"redirect into user settings", gate.Event{Tool: gate.Shell, Command: "echo '{}' > " + bashPath(userSettings)}, gate.ReasonAgentSettings},
			{"run the approval", gate.Event{Tool: gate.Shell, Command: "pbipx policy approve"}, gate.ReasonAgentSettings},
			{"run the unenrolment", gate.Event{Tool: gate.Shell, Command: "pbipx policy unenroll"}, gate.ReasonAgentSettings},
			{"run the unenrolment by path", gate.Event{Tool: gate.Shell, Command: "/opt/bin/pbipx.exe policy unenroll"}, gate.ReasonAgentSettings},
			{"run it under script", gate.Event{Tool: gate.Shell, Command: "script -qc 'pbipx policy unenroll' /dev/null"}, gate.ReasonAgentSettings},
			{"run it under winpty", gate.Event{Tool: gate.Shell, Command: "winpty pbipx policy unenroll"}, gate.ReasonAgentSettings},
			{"policy and a word it cannot tell", gate.Event{Tool: gate.Shell, Command: "pbipx policy $SUB"}, gate.ReasonAgentSettings},
			{"run it in PowerShell", gate.Event{Tool: gate.PowerShell, Command: "pbipx policy unenroll"}, gate.ReasonAgentSettings},
			{"policy status", gate.Event{Tool: gate.Shell, Command: "pbipx policy status"}, ""},
			{"name it in a message", gate.Event{Tool: gate.Shell, Command: `git commit -m "pbipx policy unenroll"`}, ""},
			{"read user settings", gate.Event{Tool: gate.Read, Path: userSettings}, ""},
			{"write elsewhere", gate.Event{Tool: gate.Write, Path: "notes.txt"}, ""},
		} {
			t.Run(filepath.Base(cwd)+"/"+tc.name, func(t *testing.T) {
				ev := tc.ev
				ev.Cwd = cwd
				d := a.decide(ev)
				check(t, d, tc.reason)
				if strings.Contains(d.Message, a.ctl.claude) || strings.Contains(d.Message, a.store.Dir) {
					t.Error("the message names a path outside the repository")
				}
			})
		}
	}
}

// TestAdoptionWithoutAStore checks that with no store to be found, a
// repository is adopted by its policy alone, and a call elsewhere allows.
func TestAdoptionWithoutAStore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a relative PBIPX_CONFIG_DIR is the way to have no store")
	}
	isolateGit(t)
	t.Setenv(approval.EnvConfigDir, "relative")
	opts := gate.Options{CheckPolicy: gate.RequireApproval(nil), Adoption: &gate.Adoption{}}
	plain := notAdoptedRepo(t, leakyFiles())
	check(t, gate.Decide(gate.Event{Tool: gate.Read, Path: leakVisual, Cwd: plain}, opts), "")
	withPolicy := newRepo(t, leakyFiles())
	check(t, gate.Decide(gate.Event{Tool: gate.Read, Path: leakVisual, Cwd: withPolicy}, opts), gate.ReasonInternalError)
}
