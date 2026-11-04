//go:build !windows

package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub writes an executable shell script named cygpath.exe in dir and
// returns its path.
func stub(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, cygpathName)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { //nolint:gosec // a test stub program
		t.Fatal(err)
	}
	return p
}

func withLimits(t *testing.T) {
	t.Helper()
	old := cygpathWaitDelay
	t.Cleanup(func() { cygpathWaitDelay = old })
	cygpathWaitDelay = 100 * time.Millisecond
}

func TestRunCygpath(t *testing.T) {
	withLimits(t)
	t.Setenv("MSYS", "winsymlinks:nativestrict")
	t.Setenv("LC_ALL", "de_DE.ISO-8859-1")
	t.Setenv("LANG", "de_DE")
	dir := t.TempDir()
	exe := stub(t, filepath.Join(dir, "bin"), `printf '/%s|%s|%s|%s|%s\n' "$1" "$#" "$MSYS" "$LC_ALL" "${LANG-unset}"`)
	out, err := runCygpath(exe, "/tmp/a b/*", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(out)), "/-w|2|noglob|C.UTF-8|unset"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	exe = stub(t, filepath.Join(dir, "pwd"), `pwd -P`)
	out, err = runCygpath(exe, "/tmp/x", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Dir(exe))
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("ran in %q, want %q", got, want)
	}
}

func TestRunCygpathFailures(t *testing.T) {
	withLimits(t)
	dir := t.TempDir()
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"fine", `echo /real/x`, true},
		{"non-zero exit", "echo /real/x\nexit 1", false},
		{"timeout", "sleep 5\necho /real/x", false},
		{"child holding the pipe", "sleep 3 &\necho /real/x", false},
		{"too much output", `yes /real/x | head -c 100000`, false},
	} {
		exe := stub(t, filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")), tc.body)
		start := time.Now()
		out, err := runCygpath(exe, "/tmp/x", 200*time.Millisecond)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v (output %q)", tc.name, err, out)
		}
		if time.Since(start) > 2*time.Second {
			t.Errorf("%s: took %v", tc.name, time.Since(start))
		}
	}
}

// TestMSYSStubOutputs runs stubs through a session: only one absolute,
// valid line is a placement.
func TestMSYSStubOutputs(t *testing.T) {
	withLimits(t)
	dir := t.TempDir()
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"one line", `echo /real/x`, true},
		{"garbage", `echo garbage`, false},
		{"two lines", "echo /real/x\necho /real/y", false},
		{"relative", `echo real/x`, false},
		{"invalid UTF-8", `printf '/real/\377\n'`, false},
		{"replacement character", `printf '/real/\357\277\275\n'`, false},
		{"empty", `true`, false},
		{"non-zero exit", "echo /real/x\nexit 3", false},
	} {
		exe := stub(t, filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")), tc.body)
		m := &msysSession{mode: msys, placed: map[string]placedPath{}}
		m.mode.find = func(*msysSession) (string, bool) { return exe, true }
		m.mode.run = runCygpath
		p, _, ok := m.word("/tmp/not/there")
		if ok != tc.ok {
			t.Errorf("%s: ok = %v", tc.name, ok)
		}
		if ok && p != "/real/x" {
			t.Errorf("%s: placed %q", tc.name, p)
		}
	}
}

func writeGit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // a test stub program
		t.Fatal(err)
	}
}

func TestFindCygpathOrder(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	bashDir := filepath.Join(base, "bash", "bin")
	install := filepath.Join(base, "git")
	gitDir := filepath.Join(install, "cmd")
	pathDir := filepath.Join(base, "onpath")
	if err := os.MkdirAll(pathDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeGit(t, gitDir)
	t.Setenv("PATH", gitDir+string(os.PathListSeparator)+pathDir)
	t.Setenv("CLAUDE_CODE_GIT_BASH_PATH", filepath.Join(bashDir, "bash.exe"))
	none := &msysSession{}

	beside := stub(t, bashDir, "true")
	inInstall := stub(t, filepath.Join(install, "usr", "bin"), "true")
	onPath := stub(t, pathDir, "true")

	for _, tc := range []struct {
		name   string
		remove string
		want   string
	}{
		{"beside bash first", "", beside},
		{"then the install git runs from", beside, inInstall},
		{"then PATH", inInstall, onPath},
		{"then nothing", onPath, ""},
	} {
		if tc.remove != "" {
			if err := os.Remove(tc.remove); err != nil {
				t.Fatal(err)
			}
		}
		got, ok := findCygpath(none)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: got %q %v, want %q", tc.name, got, ok, tc.want)
		}
	}
}

func TestFindCygpathWalksThreeLevels(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("CLAUDE_CODE_GIT_BASH_PATH", "")
	for _, tc := range []struct {
		install string
		git     []string
		want    bool
	}{
		{"inst1", []string{"mingw64", "bin"}, true},
		{"inst2", []string{"a", "b", "c"}, true},
		{"inst3", []string{"a", "b", "c", "d"}, false},
	} {
		root := filepath.Join(base, tc.install)
		gitDir := filepath.Join(append([]string{root}, tc.git...)...)
		writeGit(t, gitDir)
		exe := stub(t, filepath.Join(root, "usr", "bin"), "true")
		t.Setenv("PATH", gitDir)
		got, ok := findCygpath(&msysSession{})
		if ok != tc.want || (ok && got != exe) {
			t.Errorf("%s: got %q %v", tc.install, got, ok)
		}
	}
}

func TestFindCygpathRefusesARepository(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(base, "repo")
	planted := stub(t, filepath.Join(repo, "tools"), "true")
	good := stub(t, filepath.Join(base, "good"), "true")
	t.Setenv("CLAUDE_CODE_GIT_BASH_PATH", "")
	t.Setenv("PATH", filepath.Dir(planted)+string(os.PathListSeparator)+filepath.Dir(good))

	// In a directory of the call.
	m := &msysSession{roots: []string{repo}}
	if got, ok := findCygpath(m); !ok || got != good {
		t.Errorf("a cygpath in the working directory was not passed over: %q %v", got, ok)
	}
	// In any repository, whatever the call touches.
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	m = &msysSession{}
	if got, ok := findCygpath(m); !ok || got != good {
		t.Errorf("a cygpath in a repository was not passed over: %q %v", got, ok)
	}
	// Only a refused one: nothing found.
	t.Setenv("PATH", filepath.Dir(planted))
	if got, ok := findCygpath(m); ok {
		t.Errorf("found %q", got)
	}
	// A relative PATH entry is never used.
	t.Chdir(filepath.Join(base, "good"))
	t.Setenv("PATH", ".")
	if got, ok := findCygpath(&msysSession{}); ok {
		t.Errorf("found %q through a relative PATH entry", got)
	}
}
