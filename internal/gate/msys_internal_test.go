package gate

import (
	"testing"
	"time"
)

func TestMSYSClass(t *testing.T) {
	for _, tc := range []struct {
		p    string
		want msysKind
	}{
		{"", msysNone},
		{"x/y", msysNone},
		{"C:/x", msysNone},
		{`C:\x`, msysNone},
		{"//server/share/x", msysNone},
		{"/", msysNever},
		{"/dev/null", msysNone},
		{"/dev", msysNone},
		{"/proc/self/fd/0", msysNone},
		{"/dev/../tmp/x", msysPlace},
		{"/proc/../tmp/x", msysPlace},
		{"/c", msysNone},
		{"/c/", msysNone},
		{"/C/Users/me", msysNone},
		{"/c/Users/me/repo", msysNone},
		{"/c/../d/x", msysPlace},
		{"/c/x/..", msysPlace},
		{"/tmp/x", msysPlace},
		{"/tmp", msysPlace},
		{"/usr/bin/git", msysPlace},
		{"/cygdrive/c/x", msysPlace},
		{"/cygdrive", msysPlace},
		{"/cc/x", msysPlace},
		{"/./tmp/x", msysPlace},
		{`/tmp\x`, msysNever},
		{"/tmp/x\ny", msysNever},
	} {
		if got := msysClass(tc.p); got != tc.want {
			t.Errorf("msysClass(%q) = %d, want %d", tc.p, got, tc.want)
		}
	}
}

func TestMSYSSplit(t *testing.T) {
	for _, tc := range []struct{ p, dir, rest string }{
		{"/tmp/x", "/tmp/x", ""},
		{"/tmp/x/*.md", "/tmp/x", "/*.md"},
		{"/tmp/x/a?/b", "/tmp/x", "/a?/b"},
		{"/tmp/x/[ab]", "/tmp/x", "/[ab]"},
		{"/tmp/x*/b", "/tmp", "/x*/b"},
		{"/*", "", "/*"},
	} {
		if dir, rest := msysSplit(tc.p); dir != tc.dir || rest != tc.rest {
			t.Errorf("msysSplit(%q) = %q, %q; want %q, %q", tc.p, dir, rest, tc.dir, tc.rest)
		}
	}
}

func TestMSYSCands(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"/tmp/x", []string{"/tmp/x"}},
		{"--file=/tmp/x", []string{"/tmp/x"}},
		{"-o=x", nil},
		{"x/y", nil},
		{"/a=/b", []string{"/a=/b", "/b"}},
	} {
		got := msysCands(tc.text)
		if len(got) != len(tc.want) {
			t.Errorf("msysCands(%q) = %q, want %q", tc.text, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("msysCands(%q) = %q, want %q", tc.text, got, tc.want)
			}
		}
	}
}

func TestParseCygpath(t *testing.T) {
	abs := "/real/x"
	if windowsPaths {
		abs = `C:\real\x`
	}
	for _, tc := range []struct {
		name string
		out  string
		ok   bool
	}{
		{"one line", abs + "\n", true},
		{"no newline", abs, true},
		{"crlf", abs + "\r\n", true},
		{"empty", "", false},
		{"only a newline", "\n", false},
		{"two lines", abs + "\n" + abs + "\n", false},
		{"a blank line after", abs + "\n\n", false},
		{"relative", "real/x\n", false},
		{"invalid UTF-8", "/real/\xff\n", false},
		{"replacement character", "/real/\ufffd\n", false},
		{"control character", "/real/\x01\n", false},
		{"carriage return inside", "/real/a\rb\n", false},
		{"a drive-relative path", "C:x\n", false},
	} {
		if windowsPaths && (tc.name == "invalid UTF-8" || tc.name == "replacement character" || tc.name == "control character" || tc.name == "carriage return inside") {
			tc.out = "C:" + tc.out[len("/real"):]
		}
		got, ok := parseCygpath([]byte(tc.out))
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.ok)
		}
		if ok && got != abs {
			t.Errorf("%s: got %q", tc.name, got)
		}
	}
}

// fakeSession is a session over a fake runner that counts its runs.
func fakeSession(run func(arg string) ([]byte, error), counts *int) *msysSession {
	return &msysSession{
		mode: msysMode{
			on:      true,
			find:    func(*msysSession) (string, bool) { return "/fake/cygpath.exe", true },
			run:     func(_, arg string, _ time.Duration) ([]byte, error) { *counts++; return run(arg) },
			perRun:  time.Second,
			total:   time.Second,
			maxRuns: 3,
		},
		placed: map[string]placedPath{},
	}
}

// fakeOut is what the fake prints for arg: an absolute path on any platform.
func fakeOut(arg string) []byte {
	if windowsPaths {
		return []byte(`C:\real` + arg + "\n")
	}
	return []byte("/real" + arg + "\n")
}

func TestMSYSWordPlacesOncePerDecision(t *testing.T) {
	var runs int
	m := fakeSession(func(arg string) ([]byte, error) { return fakeOut(arg), nil }, &runs)
	want, _ := parseCygpath(fakeOut("/tmp/x"))
	for range 3 {
		p, needed, ok := m.word("/tmp/x")
		if p != want || !needed || !ok {
			t.Fatalf("word = %q %v %v", p, needed, ok)
		}
	}
	if runs != 1 {
		t.Errorf("%d runs for one path", runs)
	}
	// A glob places the directory before it, and keeps the pattern.
	wantDir, _ := parseCygpath(fakeOut("/tmp/x"))
	if p, needed, ok := m.word("/tmp/x/*.md"); p != wantDir+"/*.md" || !needed || !ok {
		t.Errorf("glob word = %q %v %v", p, needed, ok)
	}
	// The root, and a ".." after a glob, cannot be placed; nothing runs.
	before := runs
	for _, w := range []string{"/", "/*", "/tmp/*/../x"} {
		if _, needed, ok := m.word(w); !needed || ok {
			t.Errorf("word(%q): needed %v ok %v", w, needed, ok)
		}
	}
	if runs != before {
		t.Errorf("an unplaceable form ran cygpath")
	}
}

func TestMSYSWordKeepsCommonFormsAsWritten(t *testing.T) {
	var runs int
	m := fakeSession(func(arg string) ([]byte, error) { return fakeOut(arg), nil }, &runs)
	for _, w := range []string{"/c/x", "/c", "/c/Users/me/*.md", "/dev/null", "/dev/stdin", "/proc/self/fd/0",
		"C:/x", `C:\x`, "//server/share/x", "rel/x", "-v", ""} {
		if _, needed, ok := m.word(w); needed || !ok {
			t.Errorf("word(%q): needed %v ok %v", w, needed, ok)
		}
	}
	var nilSession *msysSession
	if _, needed, ok := nilSession.word("/tmp/x"); needed || !ok {
		t.Errorf("a nil session placed")
	}
	if runs != 0 {
		t.Errorf("%d runs for the common forms", runs)
	}
}

func TestMSYSRunBudget(t *testing.T) {
	var runs int
	m := fakeSession(func(arg string) ([]byte, error) { return fakeOut(arg), nil }, &runs)
	for i, p := range []string{"/aa/1", "/aa/2", "/aa/3", "/aa/4"} {
		_, _, ok := m.word(p)
		if want := i < 3; ok != want {
			t.Errorf("run %d: ok = %v", i, ok)
		}
	}
	if _, _, ok := m.word("/aa/4"); ok || runs != 3 {
		t.Errorf("the budget failure was not remembered (%d runs)", runs)
	}
}

func TestMSYSTotalTime(t *testing.T) {
	var runs int
	m := fakeSession(func(arg string) ([]byte, error) {
		time.Sleep(30 * time.Millisecond)
		return fakeOut(arg), nil
	}, &runs)
	m.mode.total, m.mode.maxRuns = 50*time.Millisecond, 100
	var oks []bool
	for _, p := range []string{"/aa/1", "/aa/2", "/aa/3", "/aa/4"} {
		_, _, ok := m.word(p)
		oks = append(oks, ok)
	}
	if !oks[0] || !oks[1] || oks[3] || runs > 3 {
		t.Errorf("oks %v, runs %d", oks, runs)
	}
}

func TestMSYSFailureIsRemembered(t *testing.T) {
	var runs int
	m := fakeSession(func(string) ([]byte, error) { return []byte("garbage\n"), nil }, &runs)
	for range 3 {
		if _, needed, ok := m.word("/tmp/x"); !needed || ok {
			t.Fatal("garbage was accepted")
		}
	}
	if runs != 1 {
		t.Errorf("%d runs", runs)
	}
}

func TestMSYSNoCygpath(t *testing.T) {
	var runs int
	m := fakeSession(func(string) ([]byte, error) { return nil, nil }, &runs)
	m.mode.find = func(*msysSession) (string, bool) { return "", false }
	if _, needed, ok := m.word("/tmp/x"); !needed || ok || runs != 0 {
		t.Errorf("needed %v ok %v runs %d", needed, ok, runs)
	}
}

func TestMSYSOffByDefaultOffWindows(t *testing.T) {
	if windowsPaths {
		t.Skip("Windows")
	}
	if msys.on || newMSYSSession() != nil {
		t.Error("the Git Bash reading is on off Windows")
	}
}
