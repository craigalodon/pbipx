package gate

import (
	"path/filepath"
	"testing"
)

// fakeMSYS turns the Git Bash reading on with Windows-style drive roots
// and a mount table of two entries, for the test.
func fakeMSYS(t *testing.T) {
	t.Helper()
	old := msys
	t.Cleanup(func() { msys = old })
	mounts := map[string]string{"/": `C:\Git`, "/usr": `C:\Git\usr`, "/tmp": `C:\Temp`, "/tmp/a": `C:\Temp\a`}
	msys = msysMode{
		on:    true,
		drive: func(l byte) string { return string(l) + `:\` },
		mount: func(p string) (string, bool) { r, ok := mounts[p]; return r, ok },
	}
}

func TestMSYSPath(t *testing.T) {
	fakeMSYS(t)
	sl := filepath.FromSlash
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"/c/Users/me/repo", `C:\` + sl("Users/me/repo"), true},
		{"/C/Users", `C:\` + "Users", true},
		{"/d", `D:\`, true},
		{"/d/", `D:\`, true},
		{"/z/a/../b", `Z:\` + sl("a/../b"), true},
		{"/cygdrive/c/x", `C:\x`, true},
		{"/CYGDRIVE/e/x/y", `E:\` + sl("x/y"), true},
		{"/c/a/*.json", `C:\` + sl("a/*.json"), true},
		{"/usr", `C:\Git\usr`, true},
		{"/tmp/a", `C:\Temp\a`, true},
		{"/tmp/*.md", `C:\Temp` + sl("/*.md"), true},
		{"/", `C:\Git`, true},
		{"/dev/null", "/dev/null", true},
		{"/dev/stdin", "/dev/stdin", true},
		{"/proc/self/fd/0", "/proc/self/fd/0", true},
		{"/etc/hosts", "", false},
		{"/cygdrive", "", false},
		{"/cc/x", "", false},
		{"/1/x", "", false},
		// Not MSYS2 absolute paths: as written.
		{"//server/share/x", "//server/share/x", true},
		{"C:/Users/me", "C:/Users/me", true},
		{`C:\Users\me`, `C:\Users\me`, true},
		{"pbip/x.json", "pbip/x.json", true},
		{"c/x", "c/x", true},
		{"", "", true},
	} {
		got, ok := msysPath(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("msysPath(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMSYSOff(t *testing.T) {
	old := msys
	t.Cleanup(func() { msys = old })
	msys = msysMode{}
	for _, w := range []string{"/c/x", "/usr/x", "/dev/null"} {
		if got, ok := msysPath(w); got != w || !ok {
			t.Errorf("msysPath(%q) = %q, %v with the mode off", w, got, ok)
		}
		if _, ok := msysToolPath(w); ok {
			t.Errorf("msysToolPath(%q) placed with the mode off", w)
		}
	}
}

func TestMSYSToolPath(t *testing.T) {
	fakeMSYS(t)
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"/c/repo/x.json", `C:\` + filepath.FromSlash("repo/x.json"), true},
		{"/cygdrive/D/x", `D:\x`, true},
		{`C:\repo\x.json`, "", false},
		{"/usr/x", "", false},
		{"pbip/x", "", false},
	} {
		got, ok := msysToolPath(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("msysToolPath(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestHostWord(t *testing.T) {
	fakeMSYS(t)
	xw := xword{text: "/c/r/p*/x", pattern: "/c/r/p*/x", known: true, glob: true}
	hostWord(&xw)
	if want := `C:\` + filepath.FromSlash("r/p*/x"); xw.text != want || xw.pattern != want || !xw.known {
		t.Errorf("hostWord = %q, %q, known %v; want %q", xw.text, xw.pattern, xw.known, want)
	}
	xw = xword{text: "/etc/x", pattern: "/etc/x", known: true}
	hostWord(&xw)
	if xw.known || !xw.unplaced || xw.text != "/etc/x" {
		t.Errorf("an unplaceable word is %q, known %v, unplaced %v", xw.text, xw.known, xw.unplaced)
	}
}
