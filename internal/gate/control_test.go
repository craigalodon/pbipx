package gate_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/gate"
	"github.com/craigalodon/pbipx/internal/setup"
)

// controlDirs points every directory that controls the gate at a fresh
// temporary one, so no test reads or names the real user's: the approval
// store (PBIPX_CONFIG_DIR, and the user configuration directory the default
// store is in), Claude Code's configuration directory and the home
// directory.
type controlDirs struct {
	store, xdg, claude, home string
}

func newControlDirs(t *testing.T) controlDirs {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := controlDirs{
		store:  filepath.Join(base, "store"),
		xdg:    filepath.Join(base, "xdg"),
		claude: filepath.Join(base, "claude-config"),
		home:   filepath.Join(base, "home"),
	}
	for _, d := range []string{c.store, c.xdg, c.claude, filepath.Join(c.home, ".claude")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(approval.EnvConfigDir, c.store)
	t.Setenv("XDG_CONFIG_HOME", c.xdg)
	t.Setenv("CLAUDE_CONFIG_DIR", c.claude)
	t.Setenv("HOME", c.home)
	t.Setenv("USERPROFILE", c.home)
	t.Setenv("APPDATA", c.xdg)
	return c
}

func TestWriteControlFiles(t *testing.T) {
	c := newControlDirs(t)
	root := newRepo(t, cleanFiles())
	userConfig, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	relStore, err := filepath.Rel(root, filepath.Join(c.store, "approved", "x"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(p string) gate.Event { return gate.Event{Tool: gate.Write, Path: p} }
	managed := setup.ManagedSettingsDir(runtime.GOOS)
	cases := []decideCase{
		{"an approval record", write(filepath.Join(c.store, "approved", strings.Repeat("0", 64))), gate.ReasonAgentSettings},
		{"the store itself", write(c.store), gate.ReasonAgentSettings},
		{"a new directory in the store", write(filepath.Join(c.store, "new", "x")), gate.ReasonAgentSettings},
		{"relative to the repository", write(relStore), gate.ReasonAgentSettings},
		{"the default store", write(filepath.Join(userConfig, "pbipx", "approved", "x")), gate.ReasonAgentSettings},
		{"CLAUDE_CONFIG_DIR settings", write(filepath.Join(c.claude, "settings.json")), gate.ReasonAgentSettings},
		{"CLAUDE_CONFIG_DIR local settings", write(filepath.Join(c.claude, "settings.local.json")), gate.ReasonAgentSettings},
		{"another case", write(filepath.Join(c.claude, "Settings.JSON")), gate.ReasonAgentSettings},
		{"home settings", write(filepath.Join(c.home, ".claude", "settings.json")), gate.ReasonAgentSettings},
		{"home settings, climbing", write(filepath.Join(c.home, ".claude", "x", "..", "settings.local.json")), gate.ReasonAgentSettings},
		{"managed settings", write(filepath.Join(managed, "managed-settings.json")), gate.ReasonAgentSettings},
		{"a managed drop-in", write(filepath.Join(managed, "managed-settings.d", "pbipx.json")), gate.ReasonAgentSettings},

		{"beside the store", write(c.store + "-other"), ""},
		{"another file in the Claude directory", write(filepath.Join(c.claude, "commands", "x.md")), ""},
		{"settings one level down", write(filepath.Join(c.claude, "x", "settings.json")), ""},
		{"a JSON file not named settings", write(filepath.Join(c.home, ".claude", "keybindings.json")), ""},
	}
	run(t, root, cases)

	d := gate.Decide(gate.Event{Tool: gate.Write, Path: filepath.Join(c.store, "approved", "x"), Cwd: root}, gate.Options{})
	if strings.Contains(d.Message, c.store) || strings.Contains(d.Message, "approved") {
		t.Error("the message names a path outside the repository")
	}
}

func TestWriteControlFilesThroughLinks(t *testing.T) {
	c := newControlDirs(t)
	root := newRepo(t, cleanFiles())
	links := t.TempDir()
	if err := os.Symlink(c.store, filepath.Join(links, "store")); err != nil {
		t.Skip("symlinks are not available")
	}
	if err := os.Symlink(filepath.Join(c.home, ".claude"), filepath.Join(root, "notes", "claude")); err != nil {
		t.Fatal(err)
	}
	// A store reached through a link, configured by the link's path.
	linked := t.TempDir()
	if err := os.Symlink(linked, filepath.Join(links, "linked-store")); err != nil {
		t.Fatal(err)
	}
	write := func(p string) gate.Event { return gate.Event{Tool: gate.Write, Path: p} }
	run(t, root, []decideCase{
		{"into the store through a link", write(filepath.Join(links, "store", "approved", "x")), gate.ReasonAgentSettings},
		{"a new directory through a link", write(filepath.Join(links, "store", "a", "b", "c")), gate.ReasonAgentSettings},
		{"home settings through a link in the repository", write("notes/claude/settings.json"), gate.ReasonAgentSettings},
		{"another file through that link", write("notes/claude/other.json"), ""},
	})
	t.Setenv(approval.EnvConfigDir, filepath.Join(links, "linked-store"))
	run(t, root, []decideCase{
		{"the store's target, by its own path", write(filepath.Join(linked, "approved", "x")), gate.ReasonAgentSettings},
	})
}
