package gate

import (
	"slices"
	"strings"
	"testing"
)

// render writes simple commands as "a b | c >t", one per element, with a
// redirection target marked by ">".
func render(cmds []simple) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		words := make([]string, len(c))
		for i, w := range c {
			words[i] = w.text
			if w.redirect {
				words[i] = ">" + w.text
			}
		}
		out = append(out, strings.Join(words, " "))
	}
	return out
}

func TestSplitCommands(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"ls", []string{"ls"}},
		{"a; b && c || d | e |& f & g", []string{"a", "b", "c", "d", "e", "f", "g"}},
		{"(a) \n b", []string{"a", "b"}},
		{"echo 'x; y' \"p && q\"", []string{"echo x; y p && q"}},
		{`echo "a \"b\" \$c" d\ e`, []string{`echo a "b" $c d e`}},
		{"cmd > out 2>&1", []string{"cmd >out"}},
		{"cmd 2> err >> log < in", []string{"cmd >err >log >in"}},
		{"cmd &> all", []string{"cmd >all"}},
		{"cmd >&2", []string{"cmd"}},
		{"cmd >| forced", []string{"cmd >forced"}},
		{"a>b", []string{"a >b"}},
		{"echo x # a comment; rm y", []string{"echo x"}},
		{"echo a#b", []string{"echo a#b"}},
		{"line \\\n continued", []string{"line continued"}},
		{"pbipx scan $(echo pbip)", []string{"pbipx scan $", "echo pbip"}},
		// Unbalanced quotes fall back to whitespace.
		{"cat 'pbip/x && rm y", []string{"cat 'pbip/x", "rm y"}},
		{`echo "open`, []string{`echo "open`}},
		{"trailing \\", []string{"trailing \\"}},
		{"", nil},
	} {
		if got := render(splitCommands(tc.line)); !slices.Equal(got, tc.want) {
			t.Errorf("splitCommands(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestQuotedWords(t *testing.T) {
	cmds := splitCommands(`ls p* 'q*' "r*" s\*`)
	if len(cmds) != 1 {
		t.Fatalf("got %d commands", len(cmds))
	}
	var quoted []bool
	for _, w := range cmds[0] {
		quoted = append(quoted, w.quoted)
	}
	if want := []bool{false, false, true, true, true}; !slices.Equal(quoted, want) {
		t.Errorf("quoted = %v, want %v", quoted, want)
	}
}

func TestProgram(t *testing.T) {
	for _, tc := range []struct {
		line, name string
	}{
		{"git diff", "git"},
		{"A=1 B=2 git diff", "git"},
		{"env -i A=1 git diff", "git"},
		{"/usr/bin/git diff", "git"},
		{"nohup time git diff", "git"},
		{"timeout 5 rg x", "rg"},
		{"xargs grep -r x", "grep"},
		{"A=1", ""},
	} {
		name, _ := program(splitCommands(tc.line)[0].args())
		if name != tc.name {
			t.Errorf("program(%q) = %q, want %q", tc.line, name, tc.name)
		}
	}
}

func TestSearchPaths(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string // nil: not a recursive search
	}{
		{"grep x .", nil},
		{"grep -r x", []string{"."}},
		{"grep -rn x a b", []string{"a", "b"}},
		{"grep -nr -A 3 x a", []string{"a"}},
		{"grep -A3 -r x a", []string{"a"}},
		{"grep -r -e x a", []string{"a"}},
		{"grep -re x a", []string{"a"}},
		{"grep -r --regexp x a", []string{"a"}},
		{"grep -r --regexp=x a", []string{"a"}},
		{"grep -r -f patterns.txt", []string{"."}},
		{"grep --include '*.md' -r x a", []string{"a"}},
		{"grep --directories=recurse x a", []string{"a"}},
		{"grep -r -- -x a", []string{"a"}},
		{"fgrep -R x", []string{"."}},
		{"rg x", []string{"."}},
		{"rg -r y x a", []string{"a"}},
		{"rg -g '*.md' -t md x a", []string{"a"}},
		{"rg --files a", []string{"a"}},
		{"rg --files", []string{"."}},
		{"rg -uuu x", []string{"."}},
		{"ag -G md x a", []string{"a"}},
		{"ack -f a", []string{"a"}},
		{"ack --match x a", []string{"a"}},
		{"cat x", nil},
	} {
		got, ok := searchPaths(splitCommands(tc.line)[0].args())
		if !ok {
			got = nil
		}
		if !slices.Equal(got, tc.want) || ok != (tc.want != nil) {
			t.Errorf("searchPaths(%q) = %q %v, want %q", tc.line, got, ok, tc.want)
		}
	}
}

func TestNamesWord(t *testing.T) {
	for _, tc := range []struct {
		line, name string
		want       bool
	}{
		{"ls pbip", "pbip", true},
		{"cat pbip/x", "pbip", true},
		{"cat ./pbip", "pbip", true},
		{"git diff -- ':!pbip'", "pbip", true},
		{"pbipx scan", "pbip", false},
		{"ls my-pbip pbip_old pbip.bak", "pbip", false},
		{"ls pbipx pbip", "pbip", true},
		{"ls warehouse/models/x", "warehouse/models", true},
		{"ls warehouse/models2", "warehouse/models", false},
	} {
		if got := namesWord(tc.line, tc.name); got != tc.want {
			t.Errorf("namesWord(%q, %q) = %v", tc.line, tc.name, got)
		}
	}
}

func TestRevPath(t *testing.T) {
	for _, tc := range []struct {
		arg, path string
		ok        bool
	}{
		{"HEAD:a/b", "a/b", true},
		{"HEAD:", "", true},
		{":a/b", "a/b", true},
		{":2:a/b", "a/b", true},
		{"HEAD", "", false},
		{"abc123", "", false},
	} {
		p, ok := revPath(tc.arg)
		if p != tc.path || ok != tc.ok {
			t.Errorf("revPath(%q) = %q %v", tc.arg, p, ok)
		}
	}
}
