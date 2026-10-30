package gate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// On Windows, Claude Code runs Bash commands through Git Bash, an MSYS2
// shell. It spells an absolute path with the drive as its first segment
// (/c/Users/me/repo), and reads /cygdrive/c/... the same way; any other
// absolute path (/usr/bin, /tmp) is under the MSYS2 root or one of its
// mounts. Go's filepath does not know either spelling: /c/x has no volume,
// so it would be joined onto the working directory and judged as a file
// Bash never reads. msysMode turns such a word into the path Windows
// names, wherever the gate reads a shell word or a git pathspec as a path.

// msysMode is how the gate reads an MSYS2 path. Off, a word is a path as
// it is written. Tests turn it on, with drive and mount of their own, to
// run the reading on any platform.
type msysMode struct {
	on bool
	// drive is the root of the drive with letter l, upper case, as the
	// host spells it, ending in a separator: C:\ on Windows.
	drive func(l byte) string
	// mount is an absolute MSYS2 path that is not on a drive (/usr/x) as
	// a host path; ok is false when it cannot be told.
	mount func(p string) (string, bool)
}

// msys is the gate's mode: on, on Windows.
var msys = hostMSYS()

func hostMSYS() msysMode {
	if runtime.GOOS != "windows" {
		return msysMode{}
	}
	return msysMode{
		on:    true,
		drive: func(l byte) string { return string(l) + `:\` },
		mount: cygpathMount,
	}
}

// msysPath is the shell word w as the host names the path it stands for.
// Off, or for a word that is not an MSYS2 absolute path (a relative one,
// C:/x, C:\x, //server/share/x, which Windows reads as Bash does), it is w.
// ok is false when w is an absolute MSYS2 path that cannot be placed: the
// gate then judges it as a word it cannot tell.
func msysPath(w string) (p string, ok bool) {
	n, repl, changed, ok := msysPrefix(w)
	if !ok {
		return "", false
	}
	if !changed {
		return w, true
	}
	return repl + filepath.FromSlash(w[n:]), true
}

// msysPrefix is how msysPath rewrites w: its first n bytes are replaced
// by repl, and the separators of the rest made the host's. changed is
// false when w is kept as it is.
func msysPrefix(w string) (n int, repl string, changed, ok bool) {
	if !msys.on || !strings.HasPrefix(w, "/") || strings.HasPrefix(w, "//") {
		return 0, "", false, true
	}
	if n, l, ok := msysDrive(w); ok {
		return n, msys.drive(l), true, true
	}
	for _, d := range []string{"/dev", "/proc"} {
		if w == d || strings.HasPrefix(w, d+"/") {
			// Devices and process files, never a file in a repository:
			// kept as written, as the rules that know /dev/stdin and
			// /proc/self/fd/0 read them.
			return 0, "", false, true
		}
	}
	// The rest is under the MSYS2 root. A glob, quote or brace is not
	// given to cygpath, whose runtime would expand it: only the directory
	// before the first one is placed.
	k := len(w)
	if i := strings.IndexAny(w, "*?[]{}'\"\\"); i >= 0 {
		k = strings.LastIndexByte(w[:i], '/')
	}
	head := w[:k]
	if head == "" {
		head = "/"
	}
	r, ok := msys.mount(head)
	if !ok {
		return 0, "", false, false
	}
	r = strings.TrimRight(r, `\/`)
	if k == len(w) && (r == "" || strings.HasSuffix(r, ":")) {
		r += string(filepath.Separator) // a root
	}
	return k, r, true, true
}

// msysDrive reads w as /x or /x/... (or /cygdrive/x...), x a drive
// letter in either case: n is how much of w names the drive and its
// separator, l the letter in upper case.
func msysDrive(w string) (n int, l byte, ok bool) {
	rest, off := w, 0
	if len(w) >= len("/cygdrive/x") && strings.EqualFold(w[:len("/cygdrive/")], "/cygdrive/") {
		rest, off = w[len("/cygdrive"):], len("/cygdrive")
	}
	if len(rest) < 2 || rest[0] != '/' {
		return 0, 0, false
	}
	c := rest[1]
	if ('a' > c || c > 'z') && ('A' > c || c > 'Z') {
		return 0, 0, false
	}
	switch {
	case len(rest) == 2:
		return off + 2, c &^ 0x20, true
	case rest[2] == '/' || rest[2] == '\\':
		return off + 3, c &^ 0x20, true
	}
	return 0, 0, false
}

// msysToolPath is a path from a Read, Grep or Write event as the host
// names it, when it is spelled /c/... or /cygdrive/c/...; ok is false
// for any other path. Such paths normally arrive as Windows paths, and
// one in MSYS2 form is judged both ways.
func msysToolPath(p string) (string, bool) {
	if !msys.on {
		return "", false
	}
	n, l, ok := msysDrive(p)
	if !ok {
		return "", false
	}
	return msys.drive(l) + filepath.FromSlash(p[n:]), true
}

// hostHome is the home directory h as the host names it: HOME is
// /c/Users/me when the gate runs under Git Bash. "" when it cannot be
// placed.
func hostHome(h string) string {
	p, ok := msysPath(h)
	if !ok {
		return ""
	}
	return p
}

// cygpath limits: how long one run may take, and how many runs one
// process makes. Past either, a path it would place cannot be placed.
const (
	cygpathTimeout = 2 * time.Second
	maxCygpathRuns = 64
)

var cygpathState struct {
	sync.Mutex
	exe    string
	found  bool
	broken bool
	runs   int
	cache  map[string]string
}

// cygpathMount places an MSYS2 path with Git Bash's cygpath -w, run with
// argv only, globbing off and a short timeout. ok is false when there is
// no cygpath, it fails, or it prints anything but one absolute path.
func cygpathMount(p string) (string, bool) {
	st := &cygpathState
	st.Lock()
	defer st.Unlock()
	if r, ok := st.cache[p]; ok {
		return r, true
	}
	if !st.found {
		st.exe, st.found = findCygpath(), true
	}
	if st.exe == "" || st.broken || st.runs >= maxCygpathRuns {
		return "", false
	}
	st.runs++
	ctx, cancel := context.WithTimeout(context.Background(), cygpathTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, st.exe, "-w", p) //nolint:gosec // G204: cygpath found on PATH or beside git, the word passed as one argument
	cmd.Env = append(os.Environ(), "MSYS=noglob", "CYGWIN=noglob")
	out, err := cmd.Output()
	if err != nil {
		st.broken = true
		return "", false
	}
	r := strings.TrimRight(string(out), "\r\n")
	if r == "" || strings.ContainsAny(r, "\r\n") || !filepath.IsAbs(r) {
		return "", false
	}
	if st.cache == nil {
		st.cache = map[string]string{}
	}
	st.cache[p] = r
	return r, true
}

// findCygpath is Git Bash's cygpath: on PATH, beside the bash Claude Code
// is set to use (CLAUDE_CODE_GIT_BASH_PATH), or in the Git for Windows
// installation git runs from. The hook runs under PowerShell, whose PATH
// rarely holds Git's usr\bin. "" when there is none.
func findCygpath() string {
	if p, err := exec.LookPath("cygpath"); err == nil {
		return p
	}
	var starts []string
	if b := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); b != "" && filepath.IsAbs(b) {
		starts = append(starts, filepath.Dir(b))
	}
	if g, err := exec.LookPath("git"); err == nil {
		if a, err := filepath.Abs(g); err == nil {
			starts = append(starts, filepath.Dir(a))
		}
	}
	for _, d := range starts {
		// bin\, cmd\, usr\bin\ and mingw64\bin\ are at most two levels
		// below the installation's root.
		for range 3 {
			c := filepath.Join(d, "usr", "bin", "cygpath.exe")
			info, err := os.Stat(c) //nolint:gosec // G703: a fixed file below git's own directory or the configured bash's, only looked at
			if err == nil && info.Mode().IsRegular() {
				return c
			}
			d = filepath.Dir(d)
		}
	}
	return ""
}
