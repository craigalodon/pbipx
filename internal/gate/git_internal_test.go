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
