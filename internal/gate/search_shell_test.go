package gate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// isolateSearch gives the test an empty home directory and clears the
// variables that configure a search program.
func isolateSearch(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"GREP_OPTIONS", "RIPGREP_CONFIG_PATH", "ACKRC", "ACK_OPTIONS"} {
		t.Setenv(name, "")
	}
	return home
}

// TestShellSearchDefault checks that a search reads recursively under its
// working directory unless it is confined to the files it names.
func TestShellSearchDefault(t *testing.T) {
	isolateSearch(t)
	runBoth(t, []both{
		// ugrep, which can be installed as grep, recurses in ways a flag's
		// shape does not show.
		{"grep --depth", sh("grep --depth=99 student"), "", gate.ReasonFinding},
		{"grep --depth apart", sh("grep --depth 99 student"), "", gate.ReasonFinding},
		{"grep -NUM", sh("grep -9 student"), "", gate.ReasonFinding},
		{"grep -d recurse", sh("grep -d recurse student"), "", gate.ReasonFinding},
		{"grep --directories=recurse", sh("grep --directories=recurse student"), "", gate.ReasonFinding},
		{"grep -g", sh("grep -g / student"), "", gate.ReasonFinding},
		{"ugrep --glob", sh("ugrep --glob='*.json' student"), "", gate.ReasonFinding},
		{"grep --include", sh("grep --include='*.json' student"), "", gate.ReasonFinding},
		{"grep --exclude", sh("grep --exclude='*.md' student"), "", gate.ReasonFinding},
		{"grep --index", sh("grep --index student"), "", gate.ReasonFinding},
		{"grep --from", shIn("sql", "grep --from=files.txt student"), "", gate.ReasonFinding},
		{"ugrep -f", sh("ugrep -f notes/clean.md"), "", gate.ReasonFinding},
		{"grep -f naming a root file", sh("grep -fpbip/B.Report/" + leakInReport + " README.md"), "", gate.ReasonFinding},
		{"ugrep --filter", shIn("sql", "ugrep --filter=json:cat student clean.sql"), "", gate.ReasonFinding},
		{"ugrep -Q", shIn("sql", "ugrep -Q"), "", gate.ReasonFinding},
		{"ugrep --config", shIn("sql", "ugrep --config student clean.sql"), "", gate.ReasonFinding},
		{"an unknown option", sh("grep --brand-new student README.md"), "", gate.ReasonFinding},
		{"an unknown expansion as an option", shIn("sql", "grep -$O student clean.sql"), "", gate.ReasonFinding},
		{"GREP_OPTIONS in the line", sh("GREP_OPTIONS=-r grep student"), "", gate.ReasonFinding},
		{"rg --pre", shIn("sql", "rg --pre ./p student clean.sql"), "", gate.ReasonFinding},
		{"rg", sh("rg TODO"), "", gate.ReasonFinding},
		{"rg away", shIn("sql", "rg TODO"), "", ""},
		{"ag", sh("ag --depth 3 student"), "", gate.ReasonFinding},
		{"ack", sh("ack student"), "", gate.ReasonFinding},
		{"ack --files-from", shIn("sql", "ack --files-from=list student"), "", gate.ReasonFinding},
		{"ack-grep", sh("ack-grep student"), "", gate.ReasonFinding},
		{"ugrep-indexer", sh("ugrep-indexer"), "", gate.ReasonFinding},
		{"rgrep", sh("rgrep student"), "", gate.ReasonFinding},
		{"ripgrep-all", sh("ripgrep-all student"), "", gate.ReasonFinding},
		{"grep over a directory", sh("grep -rn TODO pbip"), "", gate.ReasonFinding},
		{"grep over a directory away", sh("grep -rn TODO sql"), "", ""},
		{"grep --depth away", shIn("sql", "grep --depth=3 x"), "", ""},
		// Confined to the files named, or reading the input.
		{"grep a file", sh("grep -n Doe sql/clean.sql"), "", ""},
		{"rg a file", sh("rg -n Doe sql/clean.sql"), "", ""},
		{"ugrep a file", sh("ugrep -i student README.md"), "", ""},
		{"ag a file", sh("ag -i student README.md"), "", ""},
		{"ack a file", sh("ack -i student README.md"), "", ""},
		{"grep its input", sh("cat README.md | grep -i student"), "", ""},
		{"grep a pattern file", sh("echo x | grep -f notes/clean.md"), "", ""},
		{"grep a root file", sh("grep x pbip/B.Report/" + leakInReport), "", gate.ReasonFinding},
	})
}

// TestShellSearchConfig checks that a configuration file or variable that
// could change what a search reads widens it to its working directory.
func TestShellSearchConfig(t *testing.T) {
	home := isolateSearch(t)
	files := leakyFiles()
	files[".ugrep"] = "recursive\n"
	root := newRepo(t, files)
	run(t, root, []decideCase{
		{".ugrep in the working directory, grep", sh("grep -n student README.md"), gate.ReasonFinding},
		{".ugrep in the working directory, ugrep", sh("ugrep -i student README.md"), gate.ReasonFinding},
		{".ugrep in the working directory, input", sh("cat README.md | grep x"), gate.ReasonFinding},
		{".ugrep does not configure rg", sh("rg -n student README.md"), ""},
	})

	leaky := newRepo(t, leakyFiles())
	write(t, home, ".ugrep", "recursive\n")
	run(t, leaky, []decideCase{{"~/.ugrep", sh("ugrep -i student README.md"), gate.ReasonFinding}})
	if err := os.Remove(filepath.Join(home, ".ugrep")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GREP_OPTIONS", "-r")
	run(t, leaky, []decideCase{{"GREP_OPTIONS", sh("cat README.md | grep x"), gate.ReasonFinding}})
	t.Setenv("GREP_OPTIONS", "")
	t.Setenv("RIPGREP_CONFIG_PATH", filepath.Join(home, "rg.conf"))
	run(t, leaky, []decideCase{
		{"RIPGREP_CONFIG_PATH", sh("rg -n student README.md"), gate.ReasonFinding},
		{"RIPGREP_CONFIG_PATH with --no-config", sh("rg --no-config -n student README.md"), ""},
	})
}
