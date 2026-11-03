package gate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// render writes simple commands as "a b | c >t", one per element, with a
// redirection target marked by ">".
func render(cmds []simple) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		words := make([]string, len(c.words))
		for i, w := range c.words {
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
		{"pbipx scan $(echo pbip)", []string{"pbipx scan $(echo pbip)"}},
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
	for _, w := range cmds[0].words {
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

func TestSplitCommandsQuotingAndSubstitution(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{`echo $'a\'b' c`, []string{"echo a'b c"}},
		{`echo $'\x41\101B\n'`, []string{"echo AAB\n"}},
		{`pbipx scan $'\'' ; cat x #'`, []string{"pbipx scan '", "cat x"}},
		{`echo $"a b"`, []string{"echo a b"}},
		{`echo "$(cd x; ls)" y`, []string{"echo $(cd x; ls) y"}},
		{"echo `ls` y", []string{"echo `ls` y"}},
		{`echo ${HOME}/x $PWD ~/y ~`, []string{"echo ${HOME}/x $PWD ~/y ~"}},
		{"echo a~b", []string{"echo a~b"}},
	} {
		if got := render(splitCommands(tc.line)); !slices.Equal(got, tc.want) {
			t.Errorf("splitCommands(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
	cmds := splitCommands("echo \"$(cd x; ls)\" `pwd`; a | b |& c || d")
	if len(cmds) != 5 || !slices.Equal(cmds[0].subs, []string{"cd x; ls", "pwd"}) {
		t.Fatalf("substitutions = %q", cmds[0].subs)
	}
	var pipes []bool
	for _, c := range cmds {
		pipes = append(pipes, c.pipeIn)
	}
	if want := []bool{false, false, true, true, false}; !slices.Equal(pipes, want) {
		t.Errorf("pipeIn = %v, want %v", pipes, want)
	}
}

func TestWordParts(t *testing.T) {
	w := splitCommands(`x ~/a$HOME"$PWD"'$X'${Y}$1`)[0].words[1]
	var kinds []partKind
	for _, p := range w.parts {
		kinds = append(kinds, p.kind)
	}
	want := []partKind{tildePart, litPart, paramPart, litPart, paramPart, litPart, paramPart, opaquePart}
	if !slices.Equal(kinds, want) {
		t.Errorf("parts = %v, want %v", kinds, want)
	}
}

func TestStripKeywordsAndCommand(t *testing.T) {
	for _, tc := range []struct {
		line, name string
		under      []string
	}{
		{"if git diff", "git", nil},
		{"! time git diff", "git", []string{"time"}},
		{"{ rg x", "rg", nil},
		{"function f { rg x", "rg", nil},
		{"for f in a b", "", nil},
		{"xargs -n 1 -I {} cat {}", "cat", []string{"xargs"}},
		{"sudo -u nobody env A=1 cat x", "cat", []string{"sudo", "env"}},
	} {
		name, _, under := command(splitCommands(tc.line)[0].args())
		if name != tc.name || !slices.Equal(under, tc.under) {
			t.Errorf("command(%q) = %q %q, want %q %q", tc.line, name, under, tc.name, tc.under)
		}
	}
}

func TestResolvePath(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"a/b/c", "x"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join("..", "a", "b", "c"), filepath.Join(dir, "x", "lnk")); err != nil {
		t.Skip("symlinks are not available")
	}
	raw := dir + "/x/lnk/../c"
	want := filepath.Join(dir, "a", "b", "c")
	if got := resolvePath(raw); got != want {
		t.Errorf("resolvePath = %q, want %q", got, want)
	}
	// filepath.EvalSymlinks resolves the link before the "..", too.
	if got, err := filepath.EvalSymlinks(raw); err != nil || got != want {
		t.Errorf("EvalSymlinks = %q %v, want %q", got, err, want)
	}
	// A missing tail is applied lexically.
	if got := resolvePath(dir + "/x/lnk/../missing/../d"); got != filepath.Join(dir, "a", "b", "d") {
		t.Errorf("resolvePath with a missing tail = %q", got)
	}
}

func TestDisplayPath(t *testing.T) {
	for in, want := range map[string]string{
		"pbip/A.Report/x.json":   "pbip/A.Report/x.json",
		"pbip/Doe, Jane/.pbi":    "pbip/{?}/.pbi",
		"a/jane.doe@example.com": "a/{?}",
		"a/b\x07c":               "a/{?}",
		".":                      ".",
	} {
		if got := displayPath(in); got != want {
			t.Errorf("displayPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeredocs(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"cat > notes.md <<'EOF'\nIt's done\nEOF\ngrep -rn x", []string{"cat >notes.md", "grep -rn x"}},
		{"cat <<EOF\nrm -rf /\nEOF", []string{"cat"}},
		{"cat <<-\"END\"\n\tbody\n\tEND\nls", []string{"cat", "ls"}},
		{"a <<X; b <<Y\none\nX\ntwo\nY\nc", []string{"a", "b", "c"}},
		{"cat <<<'here string'", []string{"cat >here string"}},
		{"cat <<EOF", []string{"cat"}},
		{"cat <<EOF\nno delimiter line\nls", []string{"cat"}},
		{"git commit -m \"$(cat <<'EOF'\nIt's the git show fix\nEOF\n)\"",
			[]string{"git commit -m $(cat <<'EOF'\nIt's the git show fix\nEOF\n)"}},
	} {
		if got := render(splitCommands(tc.line)); !slices.Equal(got, tc.want) {
			t.Errorf("splitCommands(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
	cmds := splitCommands("cat <<EOF\n$(rg x) and `ls`\nEOF\n")
	if len(cmds) != 1 || len(cmds[0].docs) != 1 || !slices.Equal(cmds[0].docs[0].subs, []string{"rg x", "ls"}) {
		t.Fatalf("unquoted body substitutions: %+v", cmds)
	}
	cmds = splitCommands("cat <<'EOF'\n$(rg x)\nEOF\n")
	if d := cmds[0].docs[0]; !d.quoted || len(d.subs) != 0 || d.body != "$(rg x)\n" {
		t.Errorf("quoted body = %+v", d)
	}
}

func TestFallbackSplits(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"echo 'open\ngrep -r x", []string{"echo 'open", "grep -r x"}},
		{"echo \"a;cat x|wc", []string{"echo \"a", "cat x", "wc"}},
		{"echo 'a&&rg y", []string{"echo 'a", "rg y"}},
	} {
		cmds := splitCommands(tc.line)
		if got := render(cmds); !slices.Equal(got, tc.want) {
			t.Errorf("splitCommands(%q) = %q, want %q", tc.line, got, tc.want)
		}
		for _, c := range cmds {
			if !c.fallback {
				t.Errorf("splitCommands(%q): a command is not marked fallback", tc.line)
			}
		}
	}
}

func TestWrappers(t *testing.T) {
	for _, tc := range []struct{ line, name string }{
		{"unbuffer rg x", "rg"},
		{"taskset -c 1 rg x", "rg"},
		{"taskset 0x1 cat x", "cat"},
		{"flock /tmp/l cat x", "cat"},
		{"flock -w 5 /tmp/l cat x", "cat"},
		{"coproc cat x", "cat"},
		{"coproc NAME { cat x", "cat"},
		{"chronic cat x", "cat"},
		{"ionice -c 3 cat x", "cat"},
		{"nice -n 10 cat x", "cat"},
		{"nice --adjustment 10 cat x", "cat"},
		{"stdbuf -oL cat x", "cat"},
		{"stdbuf --output L cat x", "cat"},
		{"setsid -f cat x", "cat"},
		{"caffeinate -t 5 cat x", "cat"},
		{"xvfb-run -s '-screen 0' cat x", "cat"},
		{"timeout --signal KILL 5 cat x", "cat"},
		{"timeout -k5 5 cat x", "cat"},
		{"busybox sh -c x", "sh"},
		{"sudo --user nobody cat x", "cat"},
		{"mywrapper -q cat x", "cat"},
		{"mywrapper -q git diff", "git"},
		{"command -v rg", ""},
		{"which rg", "which"},
		{"echo git diff", "echo"},
		{"man grep", "man"},
	} {
		if name, _ := program(splitCommands(tc.line)[0].args()); name != tc.name {
			t.Errorf("program(%q) = %q, want %q", tc.line, name, tc.name)
		}
	}
}

func TestShellString(t *testing.T) {
	for _, tc := range []struct {
		line string
		want int
	}{
		{"-c x", 1},
		{"-c -- x", 2},
		{"--rcfile f -c x", 3},
		{"--init-file f -lc x", 3},
		{"-o pipefail -c x", 3},
		{"script.sh", -1},
		{"-- script.sh", -1},
	} {
		if got := shellString(strings.Fields(tc.line)); got != tc.want {
			t.Errorf("shellString(%q) = %d, want %d", tc.line, got, tc.want)
		}
	}
}

func TestMayWriteAll(t *testing.T) {
	s := &setup{}
	for _, line := range []string{
		"cat a b > c", "ls ls", "nice -n 5 cat x", "cp a b", "timeout 5 grep -r x .", "sed -i s/a/b/ f",
		"cat", "> x", "env A=1 cat a", "less +s x",
	} {
		v := s.viewOf(splitCommands(line)[0], cwdState{dirs: []string{"/"}}, nil)
		all := mayWriteAll(v)
		for i, xw := range v.words {
			if all[i] != mayWrite(v, xw) {
				t.Errorf("%q word %d: mayWriteAll = %v, mayWrite = %v", line, i, all[i], !all[i])
			}
		}
	}
}

func TestMatchStar(t *testing.T) {
	for _, tc := range []struct {
		pat, path string
		want      bool
	}{
		{"**/x", "x", true},
		{"**/x", "a/b/x", true},
		{"a/**/x", "a/x", true},
		{"a/**/x", "b/x", false},
		{"**/*.md", "a/b.md", true},
		{"**/**/**/y", "a/b/c/y", true},
		{"a/*/x", "a/b/c/x", false},
	} {
		if got := matchStar(strings.Split(tc.pat, "/"), strings.Split(tc.path, "/")); got != tc.want {
			t.Errorf("matchStar(%q, %q) = %v", tc.pat, tc.path, got)
		}
	}
	// Many "**" against a long path that does not match: linear in each,
	// not exponential.
	pats := append(slices.Repeat([]string{"**"}, 60), "nomatch")
	if matchStar(pats, slices.Repeat([]string{"d"}, 200)) {
		t.Error("matched")
	}
}
