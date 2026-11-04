package gate

import (
	"slices"
	"testing"
)

// gitView is a view of the command words argv, all known.
func gitView(argv ...string) view {
	v := view{argv: argv}
	for _, a := range argv {
		v.words = append(v.words, xword{text: a, pattern: a, known: true})
		v.argvKnown = append(v.argvKnown, true)
		v.argvShapes = append(v.argvShapes, argShape{})
	}
	v.name, v.rest, v.under = command(argv)
	v.restKnown = v.argvKnown[len(v.argvKnown)-len(v.rest):]
	return v
}

// TestPlacedViewChangesOnlyItsCopy checks what the git rule is also given:
// each absolute Git Bash path, and the value of a --flag=value, placed;
// the program word, drive paths, devices and relative words as written;
// and the line's own view untouched.
func TestPlacedViewChangesOnlyItsCopy(t *testing.T) {
	var runs int
	m := fakeSession(func(arg string) ([]byte, error) { return fakeOut(arg), nil }, &runs)
	m.mode.maxRuns = 10
	s := &setup{msys: m}
	v := gitView("/usr/bin/git", "-C", "/tmp/r", "log", "--output=/tmp/o", "--", "/c/x", "/dev/null", "rel", "/tmp/d/*.md")
	orig := slices.Clone(v.argv)
	pv, ok := s.placedView(v)
	if !ok {
		t.Fatal("no word was placed")
	}
	place := func(p string) string { got, _ := parseCygpath(fakeOut(p)); return got }
	want := []string{
		"/usr/bin/git", "-C", place("/tmp/r"), "log", "--output=" + place("/tmp/o"), "--", "/c/x", "/dev/null", "rel", place("/tmp/d") + "/*.md",
	}
	if !slices.Equal(pv.argv, want) {
		t.Errorf("argv = %q, want %q", pv.argv, want)
	}
	if pv.name != "git" || len(pv.rest) != len(want)-1 {
		t.Errorf("name %q, rest %q", pv.name, pv.rest)
	}
	if !slices.Equal(v.argv, orig) {
		t.Errorf("the line's own argv changed: %q", v.argv)
	}
	if v.words[2].text != "/tmp/r" {
		t.Errorf("the line's own word changed: %q", v.words[2].text)
	}

	// Nothing to place, or nothing that can be: no copy.
	if _, ok := s.placedView(gitView("git", "-C", "/c/x", "log", "--", "rel", "/dev/null")); ok {
		t.Error("a view with no word to place changed")
	}
	bad := gitView("git", "log", "--", "/tmp/r")
	bad.words[3].known = false
	if _, ok := s.placedView(bad); ok {
		t.Error("an unknown word was placed")
	}
	if _, ok := (&setup{}).placedView(v); ok {
		t.Error("a view was placed with the reading off")
	}
	if _, ok := s.placedView(gitView("cat", "/tmp/r")); ok {
		t.Error("a view of another program was placed")
	}
}
