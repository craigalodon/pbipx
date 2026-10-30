package gate

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// searchEnv clears the variables that configure a search program.
func searchEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"GREP_OPTIONS", "RIPGREP_CONFIG_PATH", "ACKRC", "ACK_OPTIONS"} {
		t.Setenv(name, "")
	}
}

// everywhere is what searchReadsOf returns for a search that can read
// files the line does not name.
const everywhere = "*"

func TestSearchReads(t *testing.T) {
	searchEnv(t)
	cwd, home := t.TempDir(), t.TempDir()
	for _, tc := range []struct {
		line string
		want []string // nil: reads no path
	}{
		// Confined: options in the allowlist, file operands.
		{"grep x a.txt", []string{"a.txt"}},
		{"grep -n -i x a.txt b.txt", []string{"a.txt", "b.txt"}},
		{"grep -A3 -e x a", []string{"a"}},
		{"grep -c --color=always x a", []string{"a"}},
		{"ugrep -i x a", []string{"a"}},
		{"rg -n x a", []string{"a"}},
		{"rg --no-config -n x a", []string{"a"}},
		{"ag -i x a", []string{"a"}},
		{"ack -i x a", []string{"a"}},
		// GNU grep with no file reads its input; a pattern file is a read.
		{"grep x", nil},
		{"grep -v -e x", nil},
		{"grep -f p.txt", []string{"p.txt"}},
		{"grep -fp.txt x a", []string{"x", "a", "p.txt"}},
		{"grep --file=p.txt x a", []string{"x", "a", "p.txt"}},
		{"grep -x -- x -", nil},
		// Recursive under the operands.
		{"grep -r x", []string{"."}},
		{"grep -rn x a b", []string{"a", "b"}},
		{"grep -nr -A 3 x a", []string{"a"}},
		{"grep -r -e x a", []string{"a"}},
		{"grep -re x a", []string{"a"}},
		{"grep -r --regexp x a", []string{"a"}},
		{"grep -r -f patterns.txt", []string{".", "patterns.txt"}},
		{"grep --include '*.md' -r x a", []string{"a"}},
		{"grep --directories=recurse x a", []string{"a"}},
		{"grep -r -- -x a", []string{"a"}},
		{"fgrep -R x", []string{"."}},
		{"rgrep x", []string{"."}},
		{"rg x", []string{"."}},
		{"rg -r y x a", []string{"a"}},
		{"rg -g '*.md' -t md x a", []string{"a"}},
		{"rg --files a", []string{"a"}},
		{"rg --files", []string{"."}},
		{"rg -uuu x", []string{"."}},
		{"rga x", []string{"."}},
		{"ripgrep-all x", []string{"."}},
		{"ag -G md x a", []string{"a"}},
		{"ag x", []string{"."}},
		{"ack -f a", []string{"a"}},
		{"ack --match x a", []string{"a"}},
		{"ack x", []string{"."}},
		{"ack-grep x", []string{"."}},
		{"ugrep x", []string{"."}},
		{"ug x", []string{"."}},
		{"ugrep-indexer", []string{"."}},
		{"ugrep-indexer a", []string{"a"}},
		{"sift x", []string{"."}},
		{"pt x", []string{"."}},
		// Recursion that grep's flag shape does not show (grep can be
		// ugrep).
		{"grep --depth=99 x", []string{"."}},
		{"grep -3 x", []string{"."}},
		{"grep -d recurse x", []string{"."}},
		{"grep -g / x", []string{"."}},
		{"grep --include=*.json x", []string{"."}},
		{"grep --index x", []string{"."}},
		// An unknown option or a glob widens to the working directory.
		{"grep --some-new-option x a", []string{"a", "."}},
		{"grep -k x a", []string{"a", "."}},
		{"grep --depth=3 x a", []string{"a"}},
		{"grep --depth 99 x", []string{"."}},
		{"grep x '*.md'", []string{"*.md", "."}},
		// Options that read files the line does not name, or run a
		// command; an argument that cannot be told.
		{"grep --from=list.txt x", []string{everywhere}},
		{"ugrep --config x a", []string{everywhere}},
		{"ugrep --config=u.conf x a", []string{everywhere}},
		{"ugrep --filter=txt:cat x a", []string{everywhere}},
		{"ugrep -Q", []string{everywhere}},
		{"rg --pre ./p x a", []string{everywhere}},
		{"ack -x x", []string{everywhere}},
		{"ack --files-from=l x", []string{everywhere}},
		{"ack --files-f=l x", []string{everywhere}},
		{"grep x $F", []string{everywhere}},
		{"grep -$O x a", []string{everywhere}},
		{"cat x", nil},
	} {
		got := searchReadsOf(t, cwd, home, tc.line)
		if !slices.Equal(got, tc.want) {
			t.Errorf("searchReads(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestSearchConfig(t *testing.T) {
	searchEnv(t)
	cwd, home := t.TempDir(), t.TempDir()
	expect := func(line string, want ...string) {
		t.Helper()
		if got := searchReadsOf(t, cwd, home, line); !slices.Equal(got, want) {
			t.Errorf("searchReads(%q) = %q, want %q", line, got, want)
		}
	}
	t.Setenv("GREP_OPTIONS", "-r")
	expect("grep x", ".")
	t.Setenv("GREP_OPTIONS", "")
	expect("GREP_OPTIONS=-r grep x", ".")
	expect("HOME=/elsewhere grep x", ".")

	t.Setenv("RIPGREP_CONFIG_PATH", filepath.Join(home, "rg.conf"))
	expect("rg x a", "a", ".")
	expect("rg --no-config x a", "a")
	t.Setenv("RIPGREP_CONFIG_PATH", "")

	t.Setenv("ACK_OPTIONS", "--follow")
	expect("ack -i x a", "a", ".")
	expect("ack --noenv -i x a", "a")
	t.Setenv("ACK_OPTIONS", "")

	for _, dir := range []string{cwd, home} {
		f := filepath.Join(dir, ".ugrep")
		if err := os.WriteFile(f, []byte("recursive\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		expect("grep x", ".")
		expect("ugrep -i x a", "a", ".")
		expect("ug x a", "a", ".")
		expect("rg x a", "a")
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	expect("grep x")
	if got := searchReadsOf(t, cwd, "", "grep x"); !slices.Equal(got, []string{"."}) {
		t.Errorf("grep x with no home = %q, want [.]", got)
	}
}

// searchReadsOf is searchReads of the first search in line, run from cwd
// with home as the home directory: everywhere alone when it can read files
// the line does not name.
func searchReadsOf(t *testing.T, cwd, home, line string) []string {
	t.Helper()
	w := &walker{s: &setup{cwd: cwd, home: home}, cache: newSpellCache(), probe: true}
	st := cwdState{dirs: []string{cwd}}
	w.commands(line, splitCommands(line), &st, 0)
	if len(w.views) == 0 {
		t.Fatalf("%q holds no command", line)
	}
	v := w.views[0]
	for _, x := range w.views {
		if _, ok := searchers[x.name]; ok {
			v = x
			break
		}
	}
	paths, all := w.searchReads(v)
	if all {
		return []string{everywhere}
	}
	return paths
}
