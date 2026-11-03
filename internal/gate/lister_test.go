package gate

import (
	"strings"
	"testing"
)

func TestLister(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"ls", true},
		{"ls -laR a", true},
		{"stat -c %s a", true},
		{"du -sh a", true},
		{"du --files0-from=list", false},
		{"du -X pats a", false},
		{"du -- -X", true},
		{"tree -a -L 2 a", true},
		{"tree -f a", true},
		{"tree --fromfile list", false},
		{"tree -o out a", false},
		{"tree -aR -H . a", false},
		{"find", true},
		{"find a b -name '*.json' -type f", true},
		{"find -L a -maxdepth 2 ( -name x -o -iname y ) -print0", true},
		{"find a ! -path '*/.git/*' -ls", true},
		{"find a -newermt 2020-01-01 -size +1k", true},
		{"find a -printf '%p %s %TY-%Tm %10f\\n'", true},
		{"find a -printf '%Z\\n'", false},
		{"find a -printf", false},
		{"find a -exec cat {} +", false},
		{"find a -execdir cat {} ;", false},
		{"find a -ok cat {} ;", false},
		{"find a -fprint out", false},
		{"find a -fprintf out %p", false},
		{"find a -delete", false},
		{"find a -name", false},
		{"find a -files0-from list", false},
		{"file a", false},
		{"wc -l a", false},
		{"cat a", false},
	} {
		name, rest := program(splitCommands(tc.line)[0].args())
		if got := lister(name, rest); got != tc.want {
			t.Errorf("lister(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestNamesRootOutsideListers(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"ls -R pbip", false},
		{"find pbip -name '*.json'", false},
		{"ls -R pbip; cat README.md", false},
		{"ls -R pbip | xargs cat", false},
		{"ls pbip && cat pbip/x", true},
		{"ls $(cat pbip/x)", true},
		{"echo $(ls pbip)", false},
		{"ls <<EOF\npbip\nEOF", true},
		{"ls 'pbip", true},
		{"wc -c pbip", true},
		{"ls sql", false},
	} {
		if got := namesRootOutsideListers(tc.line, "pbip", true); got != tc.want {
			t.Errorf("namesRootOutsideListers(%q) = %v, want %v", strings.ReplaceAll(tc.line, "\n", `\n`), got, tc.want)
		}
	}
}
