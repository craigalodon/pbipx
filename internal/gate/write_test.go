package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

const (
	agentSettings      = ".claude/settings.json"
	agentLocalSettings = "tools/.claude/settings.local.json"
)

// settingsFiles is a clean repository with Claude Code settings at the
// root and in a subdirectory.
func settingsFiles() map[string]string {
	files := cleanFiles()
	files[agentSettings] = "{}\n"
	files[agentLocalSettings] = "{}\n"
	return files
}

func TestWrite(t *testing.T) {
	root := newRepo(t, settingsFiles())
	outside := t.TempDir()
	write := func(p string) gate.Event { return gate.Event{Tool: gate.Write, Path: p} }
	in := func(dir, p string) gate.Event { return gate.Event{Tool: gate.Write, Path: p, Cwd: dir} }
	run(t, root, []decideCase{
		{"settings at the root", write(agentSettings), gate.ReasonAgentSettings},
		{"settings by absolute path", write(filepath.Join(root, filepath.FromSlash(agentSettings))), gate.ReasonAgentSettings},
		{"local settings in a subdirectory", write(agentLocalSettings), gate.ReasonAgentSettings},
		{"local settings, not yet there", write(".claude/settings.local.json"), gate.ReasonAgentSettings},
		{"settings in a new subdirectory", write("a/b/.claude/settings.json"), gate.ReasonAgentSettings},
		{"relative to a subdirectory", in("tools", ".claude/settings.local.json"), gate.ReasonAgentSettings},
		{"from inside .claude", in(".claude", "settings.json"), gate.ReasonAgentSettings},
		{"climbing back in", in("tools", "../.claude/settings.json"), gate.ReasonAgentSettings},
		{"another case", write(".Claude/Settings.JSON"), gate.ReasonAgentSettings},

		{"empty path", write(""), ""},
		{"a note", write(cleanNote), ""},
		{"a report file", write(reportJSON), ""},
		{"a leaking file", write(leakNote), ""},
		{"another file in .claude", write(".claude/commands/x.md"), ""},
		{"settings not in .claude", write("settings.json"), ""},
		{"settings in a directory named like .claude", write("x.claude/settings.json"), ""},
		{"a backup beside the settings", write(".claude/settings.json.bak"), ""},
		{"settings outside the repository", write(filepath.Join(outside, ".claude", "settings.json")), ""},
	})
}

func TestWriteSymlinks(t *testing.T) {
	root := newRepo(t, settingsFiles())
	if err := os.Symlink(filepath.Join(root, filepath.FromSlash(agentSettings)), filepath.Join(root, "notes", "link.json")); err != nil {
		t.Skip("symlinks are not available")
	}
	// A directory that is .claude by another name.
	if err := os.Symlink(filepath.Join(root, ".claude"), filepath.Join(root, "conf")); err != nil {
		t.Fatal(err)
	}
	// A link from outside the repository into it.
	outside := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "tools"), filepath.Join(outside, "tools")); err != nil {
		t.Fatal(err)
	}
	write := func(p string) gate.Event { return gate.Event{Tool: gate.Write, Path: p} }
	run(t, root, []decideCase{
		{"link to the settings", write("notes/link.json"), gate.ReasonAgentSettings},
		{"through a linked directory", write("conf/settings.json"), gate.ReasonAgentSettings},
		{"new file through a linked directory", write("conf/settings.local.json"), gate.ReasonAgentSettings},
		{"from outside through a link", write(filepath.Join(outside, "tools", ".claude", "settings.local.json")), gate.ReasonAgentSettings},
		{"another file through a linked directory", write("conf/other.json"), ""},
	})
}

func TestShellAgentSettings(t *testing.T) {
	root := newRepo(t, settingsFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	shIn := func(dir, cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd, Cwd: dir} }
	run(t, root, []decideCase{
		{"redirect into the settings", sh(`echo '{"disableAllHooks": true}' > .claude/settings.json`), gate.ReasonAgentSettings},
		{"append to the settings", sh("echo x >> " + agentLocalSettings), gate.ReasonAgentSettings},
		{"redirect into new local settings", sh("printf x > .claude/settings.local.json"), gate.ReasonAgentSettings},
		{"as an argument", sh("cp /tmp/x.json .claude/settings.json"), gate.ReasonAgentSettings},
		{"quoted", sh(`sed -i 's/a/b/' ".claude/settings.json"`), gate.ReasonAgentSettings},
		{"as a flag value", sh("jq . in.json --output=.claude/settings.json"), gate.ReasonAgentSettings},
		{"dd of=", sh("dd if=in.json of=tools/.claude/settings.local.json"), gate.ReasonAgentSettings},
		{"by glob", sh("sed -i s/a/b/ .claude/set*"), gate.ReasonAgentSettings},
		{"by glob in a subdirectory", sh("rm */.claude/*.json"), gate.ReasonAgentSettings},
		{"relative to the cwd", shIn("tools", "tee .claude/settings.local.json < in.json"), gate.ReasonAgentSettings},
		{"later in a chain", sh("ls && cat in.json > .claude/settings.json"), gate.ReasonAgentSettings},
		{"after the tool alone", sh("pbipx scan --all > .claude/settings.json"), gate.ReasonAgentSettings},
		{"a read names it too", sh("cat .claude/settings.json"), gate.ReasonAgentSettings},

		{"a quoted glob is not expanded", sh("ls '.claude/set*'"), ""},
		{"another file in .claude", sh("cat .claude/commands/x.md"), ""},
		{"the .claude directory", sh("ls .claude"), ""},
		{"plain command", sh("go test ./..."), ""},
	})
}

// TestAgentSettingsMessages checks that a deny names no command and no
// content: the command carries a planted value, and the path a typed
// directory that does not exist.
func TestAgentSettingsMessages(t *testing.T) {
	root := newRepo(t, settingsFiles())
	for _, ev := range []gate.Event{
		{Tool: gate.Shell, Command: `echo 'Doe, Jane' > .claude/settings.json`, Cwd: root},
		{Tool: gate.Shell, Command: `echo x > 'Doe, Jane/.claude/settings.json'`, Cwd: root},
	} {
		d := gate.Decide(ev, gate.Options{})
		check(t, d, gate.ReasonAgentSettings)
		if strings.Contains(d.Message, "echo") || strings.Contains(d.Message, ">") {
			t.Errorf("the message echoes the command: %s", d.Message)
		}
	}
	d := gate.Decide(gate.Event{Tool: gate.Write, Path: agentSettings, Cwd: root}, gate.Options{})
	check(t, d, gate.ReasonAgentSettings)
	if !strings.Contains(d.Message, agentSettings) {
		t.Errorf("the message does not name the file: %s", d.Message)
	}
}
