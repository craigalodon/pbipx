package gate

import (
	"slices"
	"testing"
)

func TestSplitAlias(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
		ok   bool
	}{
		{"log --oneline", []string{"log", "--oneline"}, true},
		{"  log\t-p  ", []string{"log", "-p"}, true},
		{`log --format='%h %s'`, []string{"log", "--format=%h %s"}, true},
		{`log --grep="a b"`, []string{"log", "--grep=a b"}, true},
		{`log a\ b`, []string{"log", "a b"}, true},
		{`log ''`, []string{"log", ""}, true},
		{`log 'open`, nil, false},
		{`log trailing\`, nil, false},
		{"", nil, true},
	} {
		got, ok := splitAlias(tc.in)
		if ok != tc.ok || !slices.Equal(got, tc.want) {
			t.Errorf("splitAlias(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSafeConfig(t *testing.T) {
	for kv, want := range map[string]bool{
		"color.ui=never":         true,
		"Core.QuotePath=off":     true,
		"user.name=x":            true,
		"pager.log=false":        true,
		"pager.log=cat":          true,
		"pager.log=less":         false,
		"core.pager=cat":         true,
		"core.pager=less -R":     false,
		"core.editor=true":       true,
		"core.editor=vi":         false,
		"alias.x=status":         false,
		"diff.external=cat":      false,
		"stash.showPatch=true":   false,
		"log.showSignature=true": false,
		"diff.json.textconv=cat": false,
		"core.fsmonitor=x":       false,
		"filter.x.smudge=cat":    false,
		"include.path=x":         false,
		"pager.log=\x00":         false,
		"":                       false,
	} {
		if got := safeConfig(kv); got != want {
			t.Errorf("safeConfig(%q) = %v, want %v", kv, got, want)
		}
	}
}

func TestUnsafeEnv(t *testing.T) {
	for _, tc := range []struct {
		words []string
		want  bool
	}{
		{nil, false},
		{[]string{"GIT_PAGER=cat"}, false},
		{[]string{"PAGER="}, false},
		{[]string{"GIT_PAGER=less"}, true},
		{[]string{"GIT_EDITOR=true", "GIT_AUTHOR_NAME=x", "GIT_TRACE=1"}, false},
		{[]string{"GIT_DIR=.git"}, true},
		{[]string{"env", "-i", "GIT_CONFIG_COUNT=1"}, true},
		{[]string{"LESSOPEN=|cat %s"}, true},
		{[]string{"LANG=C"}, false},
	} {
		if got := unsafeEnv(tc.words); got != tc.want {
			t.Errorf("unsafeEnv(%q) = %v, want %v", tc.words, got, tc.want)
		}
	}
}

func TestHasShort(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"-p"}, true},
		{[]string{"-ap"}, true},
		{[]string{"--patch"}, true},
		{[]string{"-mpatch"}, false},
		{[]string{"-m", "patch"}, false},
		{[]string{"--prune"}, false},
		{[]string{"patch"}, false},
		{[]string{"-"}, false},
	} {
		if got := hasShort(tc.args, 'p', "--patch", "m"); got != tc.want {
			t.Errorf("hasShort(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestBundleToOutput(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"create", "-", "--all"}, true},
		{[]string{"create", "-q", "-", "--all"}, true},
		{[]string{"create", "/dev/stdout", "--all"}, true},
		{[]string{"create", "all.bundle", "--all"}, false},
		{[]string{"list-heads", "-"}, false},
		{nil, false},
	} {
		if got := bundleToOutput(tc.args); got != tc.want {
			t.Errorf("bundleToOutput(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestRevPathBraces(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"HEAD:a/b", "a/b", true},
		{":a", "a", true},
		{":2:a", "a", true},
		{"HEAD", "", false},
		{"HEAD@{10:00}", "", false},
		{"HEAD@{2000-01-01 10:00}:a", "a", true},
		{"main@{1}:x:y", "x:y", true},
		{"HEAD^{tree}", "", false},
	} {
		got, ok := revPath(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("revPath(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestStatusPaths(t *testing.T) {
	for _, tc := range []struct {
		st       string
		combined bool
		want     int
		ok       bool
	}{
		{"M", false, 1, true},
		{"A", false, 1, true},
		{"R100", false, 2, true},
		{"C075", false, 2, true},
		{"MM", true, 3, true},
		{"RM", true, 3, true},
		{"MMM", true, 4, true},
		{"MM", false, 0, false},
		{"R1000", false, 0, false},
		{"xM", false, 0, false},
		{"", false, 0, false},
		{"notes/a", false, 0, false},
	} {
		got, ok := statusPaths(tc.st, tc.combined)
		if got != tc.want || ok != tc.ok {
			t.Errorf("statusPaths(%q, %v) = %d, %v; want %d, %v", tc.st, tc.combined, got, ok, tc.want, tc.ok)
		}
	}
}

func TestVerbosity(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want int
	}{
		{nil, 0},
		{[]string{"-v"}, 1},
		{[]string{"-vv"}, 2},
		{[]string{"-v", "--verbose"}, 2},
		{[]string{"-sv"}, 1},
		{[]string{"-v", "--no-verbose"}, 0},
		{[]string{"-uv"}, 0}, // -u takes the rest of the cluster
		{[]string{"--porcelain"}, 0},
	} {
		if got := verbosity(tc.args, "u"); got != tc.want {
			t.Errorf("verbosity(%q) = %d, want %d", tc.args, got, tc.want)
		}
	}
}

func TestFilterDiffArgs(t *testing.T) {
	for _, tc := range []struct {
		args, want []string
	}{
		{[]string{"-p", "HEAD"}, []string{"HEAD"}},
		{[]string{"--stat=80", "-U5", "--unified=2", "HEAD"}, []string{"HEAD"}},
		{[]string{"--output", "x.patch", "--", "a"}, []string{"--", "a"}},
		{[]string{"--output=x.patch", "HEAD"}, []string{"HEAD"}},
		{[]string{"--line-prefix", "> ", "-M", "HEAD"}, []string{"-M", "HEAD"}},
		{[]string{"--format=%B", "--oneline", "--graph", "-n", "1"}, []string{"-n", "1"}},
		{[]string{"--color=always", "--no-color", "--relative=sub", "-c"}, []string{"-c"}},
		{[]string{"-s", "--no-patch", "--name-only", "-W"}, nil},
	} {
		if got := filterDiffArgs(tc.args); !slices.Equal(got, tc.want) {
			t.Errorf("filterDiffArgs(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
	if got := filterDiffArgs([]string{"-u", "-p"}, "-u"); !slices.Equal(got, []string{"-u"}) {
		t.Errorf("filterDiffArgs keeping -u = %q", got)
	}
}

func TestPositionalOf(t *testing.T) {
	for _, tc := range []struct {
		args, want []string
	}{
		{[]string{"--max-count", "1", "HEAD"}, []string{"HEAD"}},
		{[]string{"-n", "1", "HEAD"}, []string{"HEAD"}},
		{[]string{"--max-count=1", "HEAD"}, []string{"HEAD"}},
		{[]string{"--author", "x", "--grep", "y", "a", "b"}, []string{"a", "b"}},
		{[]string{"-U", "3", "HEAD"}, []string{"3", "HEAD"}}, // -U takes no separate value
		{[]string{"-", "x"}, []string{"-", "x"}},
	} {
		if got := positionalOf(tc.args, gitRevValueOpts); !slices.Equal(got, tc.want) {
			t.Errorf("positionalOf(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
