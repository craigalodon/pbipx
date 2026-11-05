package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
)

// gateEvent is a gate event that reads a file in the working copy copy/.
const gateEvent = `{
  "hook_event_name": "PreToolUse",
  "cwd": "{root}",
  "tool_name": "Read",
  "tool_input": { "file_path": "{work}/copy/.pbi/cache.abf" }
}
`

func TestValidateWorkingCopiesRules(t *testing.T) {
	withCopy := func(t *testing.T, dir string) {
		appendTo("case.toml", "working-copies = [\"copy\"]\n")(t, dir)
		write("stdin", gateEvent)(t, dir)
	}
	tests := []struct {
		name   string
		id     string
		mutate func(t *testing.T, dir string)
		want   string // "" for a case that must validate
	}{
		{"a gate case with one", gate, withCopy, ""},
		{"a gate case with two", gate, func(t *testing.T, dir string) {
			withCopy(t, dir)
			replace("case.toml", `["copy"]`, `["copy", "other/copy"]`)(t, dir)
		}, ""},
		{"outside a gate case", slicer, appendTo("case.toml", "working-copies = [\"copy\"]\n"), "working-copies is set, but only a gate case registers one"},
		{"in a collect case", collect, appendTo("case.toml", "working-copies = [\"copy2\"]\n"), "working-copies is set, but only a gate case registers one"},
		{"unclean path", gate, appendTo("case.toml", "working-copies = [\"../copy\"]\n"), "working-copies[0] is not a clean relative path"},
		{"absolute path", gate, appendTo("case.toml", "working-copies = [\"/copy\"]\n"), "working-copies[0] is not a clean relative path"},
		{"the base itself", gate, appendTo("case.toml", "working-copies = [\".\"]\n"), "working-copies[0] is not a clean relative path"},
		{"listed twice", gate, appendTo("case.toml", "working-copies = [\"copy\", \"copy\"]\n"), "working-copies[1] is listed twice"},
		{"one inside another", gate, appendTo("case.toml", "working-copies = [\"copy\", \"copy/inner\"]\n"), "working-copies[1] is inside working-copies[0]"},
		{"work in stdin without working copies", gate, write("stdin", gateEvent), "stdin uses {work}, which only a stage or collect case, or a gate case with working-copies, has"},
		{"empty list", gate, appendTo("case.toml", "working-copies = []\n"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestCorpus(t)
			tt.mutate(t, filepath.Join(dir, filepath.FromSlash(tt.id)))
			c := openTest(t, dir)
			var all []string
			for _, cs := range c.Cases {
				for _, e := range c.Validate(cs) {
					all = append(all, e.Error())
				}
			}
			if tt.want == "" {
				if len(all) != 0 {
					t.Errorf("problems: %q", all)
				}
				return
			}
			found := false
			for _, e := range all {
				found = found || strings.Contains(e, tt.want)
			}
			if !found {
				t.Errorf("no problem contains %q; got %q", tt.want, all)
			}
		})
	}
}

func TestPrepareRegistersWorkingCopies(t *testing.T) {
	needGit(t)
	dir := copyTestCorpus(t)
	caseDir := filepath.Join(dir, filepath.FromSlash(gate))
	appendTo("case.toml", "working-copies = [\"copy\", \"deep/er/copy\"]\n")(t, caseDir)
	write("stdin", gateEvent)(t, caseDir)
	c := openTest(t, dir)
	cs := findCase(t, c, gate)
	if errs := c.Validate(cs); len(errs) > 0 {
		t.Fatal(errs)
	}
	root, config := t.TempDir(), t.TempDir()
	p, err := c.Prepare(cs, root, config, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Work == "" {
		t.Fatal("no working-copy base was made")
	}
	if rel, err := filepath.Rel(root, p.Work); err == nil && !strings.HasPrefix(rel, "..") {
		t.Errorf("the working-copy base is inside the root")
	}
	got, err := (&approval.Store{Dir: config}).WorkingCopies()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(p.Work, "copy"), filepath.Join(p.Work, "deep", "er", "copy")}
	if len(got) != len(want) {
		t.Fatalf("registered %d working copies, want %d", len(got), len(want))
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g.Path == w
		}
		if !found {
			t.Errorf("working copy %s is not registered", w)
		}
		if info, err := os.Stat(w); err != nil || !info.IsDir() {
			t.Errorf("working copy %s is not a directory", w)
		}
	}
	stdin := string(cs.StdinWith(p))
	if strings.Contains(stdin, "{work}") || strings.Contains(stdin, "{root}") || !strings.Contains(stdin, filepath.ToSlash(p.Work)) && filepath.Separator == '/' {
		t.Errorf("stdin placeholders were not replaced: %s", stdin)
	}
}

func TestPrepareWithoutWorkingCopiesMakesNoBase(t *testing.T) {
	needGit(t)
	c := openTest(t, testCorpusDir)
	cs := findCase(t, c, gate)
	root, config := t.TempDir(), t.TempDir()
	p, err := c.Prepare(cs, root, config, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Work != "" {
		t.Errorf("a case with no working copies got a base")
	}
	if got, err := (&approval.Store{Dir: config}).WorkingCopies(); err != nil || len(got) != 0 {
		t.Errorf("working copies = %v, %v", got, err)
	}
}

// TestStdinWithEscapesWork checks {work} in a gate event is written as JSON
// string content, as {root} is.
func TestStdinWithEscapesWork(t *testing.T) {
	cs := Case{Spec: Spec{Command: commandGate}, HasStdin: true, Stdin: []byte(`{"cwd": "{root}", "p": "{work}/x"}`)}
	p := &Prepared{Root: `C:\r "q"`, Work: `C:\w "q"`}
	got := string(cs.StdinWith(p))
	if want := `{"cwd": "C:\\r \"q\"", "p": "C:\\w \"q\"/x"}`; got != want {
		t.Errorf("stdin = %s, want %s", got, want)
	}
}
