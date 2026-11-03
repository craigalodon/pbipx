package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellInterpreterNamesSettings: the name test for Claude Code's
// settings files reads the code text an interpreter is given, as the
// root-name test does: python3 -c, node -e, perl -e, ruby -e, and a
// here-document fed to one.
func TestShellInterpreterNamesSettings(t *testing.T) {
	root := newRepo(t, settingsFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	settings := gate.ReasonAgentSettings
	run(t, root, []decideCase{
		{"python3 -c", sh(`python3 -c "open('.claude/settings.json','w').write('{}')"`), settings},
		{"python -c, double-quoted path", sh(`python -c 'open(".claude/settings.local.json","w")'`), settings},
		{"node -e", sh(`node -e "require('fs').writeFileSync('.claude/settings.json','{}')"`), settings},
		{"perl -e", sh(`perl -e 'open(F,">.claude/settings.json")'`), settings},
		{"ruby -e", sh(`ruby -e "File.write('.claude/settings.json','{}')"`), settings},
		{"a nested directory", sh(`python3 -c "open('tools/.claude/settings.json','w')"`), settings},
		{"an absolute path", sh(`python3 -c "open('` + root + `/.claude/settings.json','w')"`), settings},
		{"another case", sh(`python3 -c "open('.Claude/Settings.JSON','w')"`), settings},
		{"a here-document to python3", sh("python3 - <<'EOF'\nopen('.claude/settings.json','w')\nEOF"), settings},
		{"a here-document to node", sh("node <<'EOF'\nrequire('fs').writeFileSync('.claude/settings.json','x')\nEOF"), settings},
		{"python3 -c with other code", sh(`python3 -c "print(1)"`), ""},
		{"python3 -c naming a note", sh(`python3 -c "open('notes/clean.md').read()"`), ""},
		{"python3 -c naming settings.json elsewhere", sh(`python3 -c "open('settings.json')"`), ""},
		{"python3 -c naming a .claude command", sh(`python3 -c "open('.claude/commands/x.md')"`), ""},
		{"echo of the path", sh(`echo ".claude/settings.json"`), settings},
	})
}
