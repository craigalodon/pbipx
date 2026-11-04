package gate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// This file places the absolute Git Bash paths that are not drive paths
// (/tmp/x, /usr/x, a custom mount): Git Bash resolves them through its
// mount table, and Git Bash's own cygpath -w says where. It is not
// build-tagged so every rule can be tested off Windows, through msys.

// msysMode is how the gate places such paths. Only Windows turns it on.
type msysMode struct {
	on bool
	// find says where cygpath is, passing over what m refuses.
	find func(m *msysSession) (exe string, ok bool)
	// run runs exe -w arg for at most limit and returns its standard
	// output.
	run func(exe, arg string, limit time.Duration) ([]byte, error)
	// perRun, total and maxRuns bound one decision's runs.
	perRun, total time.Duration
	maxRuns       int
}

// msys is the gate's mode. Tests replace it.
var msys = msysMode{
	on:      windowsPaths,
	find:    findCygpath,
	run:     runCygpath,
	perRun:  2 * time.Second,
	total:   10 * time.Second,
	maxRuns: 32,
}

// cygpathName is the program's file name, on every platform.
const cygpathName = "cygpath.exe"

// msysKind says what a path needs placing.
type msysKind int

const (
	// msysNone: read as written, nothing runs.
	msysNone msysKind = iota
	// msysPlace: place it with cygpath.
	msysPlace
	// msysNever: it cannot be placed: the root, or a form cygpath would
	// read differently from the gate.
	msysNever
)

// msysClass says how the path p, a word or the value of --flag=value, is
// placed. A path that does not start with a single slash is not one of
// these (C:/x, C:\x and //server/share are Windows paths).
func msysClass(p string) msysKind {
	if len(p) == 0 || p[0] != '/' || strings.HasPrefix(p, "//") {
		return msysNone
	}
	if p == "/" {
		return msysNever
	}
	segs := msysSegments(p)
	if slices.Contains(segs, "..") {
		// What the dots lead to decides whether it is a virtual file.
		cs := msysSegments(path.Clean(strings.ReplaceAll(p, "\\", "/")))
		if len(cs) > 0 && virtualRoot(cs[0]) && !virtualFile(cs) {
			return msysNever
		}
	} else if len(segs) > 0 {
		first := segs[0]
		switch {
		case virtualRoot(first):
			// /dev and /proc are Git Bash's own: only the standard
			// devices and descriptors are known to be no file in a
			// repository. /proc/self/cwd and /proc/self/root lead into
			// one, so the rest cannot be told.
			if virtualFile(segs) {
				return msysNone
			}
			return msysNever
		case len(first) == 1 && isASCIILetter(first[0]):
			// A drive: today's reading.
			return msysNone
		}
	}
	if strings.ContainsAny(p, "\\\x00\r\n") {
		return msysNever
	}
	return msysPlace
}

// msysSegments are the segments of p, split at either separator.
func msysSegments(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
}

// virtualRoot reports whether the first segment names /dev or /proc, as
// Git Bash matches a mount: without regard to case.
func virtualRoot(seg string) bool {
	return strings.EqualFold(seg, "dev") || strings.EqualFold(seg, "proc")
}

func allDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// virtualFile reports whether the segments are one of the few files under
// /dev and /proc that are no file in a repository: the standard devices and
// descriptors (/dev/null, /dev/fd/N, /proc/self/fd/N, /proc/N/fd/N).
func virtualFile(segs []string) bool {
	switch {
	case strings.EqualFold(segs[0], "dev"):
		if len(segs) == 3 {
			return strings.EqualFold(segs[1], "fd") && allDigits(segs[2])
		}
		if len(segs) != 2 {
			return false
		}
		for _, n := range []string{"null", "stdin", "stdout", "stderr", "tty", "zero", "urandom", "random"} {
			if strings.EqualFold(segs[1], n) {
				return true
			}
		}
	case len(segs) == 4:
		return (strings.EqualFold(segs[1], "self") || allDigits(segs[1])) && strings.EqualFold(segs[2], "fd") && allDigits(segs[3])
	}
	return false
}

// msysSplit splits p at its first glob character: dir is the path before
// the last slash ahead of it, rest the remainder from that slash. A path
// with no glob character is all dir.
func msysSplit(p string) (dir, rest string) {
	i := strings.IndexAny(p, "*?[")
	if i < 0 {
		return p, ""
	}
	j := strings.LastIndexByte(p[:i], '/')
	return p[:j], p[j:]
}

// msysCands are the parts of the word text that are absolute Git Bash
// paths: the text, and the value of --flag=value.
func msysCands(text string) []string {
	var out []string
	if strings.HasPrefix(text, "/") {
		out = append(out, text)
	}
	if _, v, ok := strings.Cut(text, "="); ok && strings.HasPrefix(v, "/") {
		out = append(out, v)
	}
	return out
}

// msysSession is one decision's placing: the cygpath found, the runs made
// and their results, failures included. A nil session places nothing.
type msysSession struct {
	mode msysMode
	// roots are the directories of the call, where a cygpath may not be.
	roots []string

	exe      string
	exeKnown bool
	runs     int
	spent    time.Duration
	placed   map[string]placedPath
}

type placedPath struct {
	path string
	ok   bool
}

// newMSYSSession is a session for a decision, nil when the mode is off.
func newMSYSSession() *msysSession {
	if !msys.on {
		return nil
	}
	return &msysSession{mode: msys, placed: map[string]placedPath{}}
}

// msysNotFoundNote is added to a deny when a path could not be placed
// because Git Bash's cygpath was not found.
const msysNotFoundNote = "Git Bash's cygpath.exe was not found, so paths such as /tmp/x could not be placed; " +
	"set CLAUDE_CODE_GIT_BASH_PATH to Git Bash's bash.exe."

// notFound reports whether a path needed placing and cygpath was not found.
func (m *msysSession) notFound() bool {
	return m != nil && m.exeKnown && m.exe == ""
}

// addRepoOf records the root of the repository that holds dir: the
// nearest directory above it, itself included, with a .git in it.
func (m *msysSession) addRepoOf(dir string) {
	if m == nil {
		return
	}
	for d := dir; d != ""; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			m.addRoot(d)
			return
		}
		parent := filepath.Dir(d)
		if parent == d {
			return
		}
		d = parent
	}
}

// addRoot records a directory the call touches.
func (m *msysSession) addRoot(dir string) {
	if m != nil && dir != "" {
		m.roots = append(m.roots, dir)
	}
}

// word places p, a word or a part of one, for the rules. needed is false
// when p is read as written; ok is false when it needs placing and cannot
// be (the word is then unknown). placed is a Windows path; for a word
// with a glob it is the placed directory and the pattern after it.
func (m *msysSession) word(p string) (placed string, needed, ok bool) {
	if m == nil {
		return "", false, true
	}
	kind := msysClass(p)
	if kind == msysNone {
		return "", false, true
	}
	if kind == msysNever {
		return "", true, false
	}
	dir, rest := msysSplit(p)
	if rest != "" {
		// Only the directory is placed. A ".." after a glob would climb
		// out of what was placed.
		if slices.Contains(strings.Split(rest, "/"), "..") || dir == "" || msysClass(dir) == msysNever {
			return "", true, false
		}
		if msysClass(dir) == msysNone {
			return "", false, true
		}
	}
	pd, ok := m.place(dir)
	if !ok {
		return "", true, false
	}
	return pd + rest, true, true
}

// place runs cygpath -w for the path p, once per decision.
func (m *msysSession) place(p string) (string, bool) {
	if r, ok := m.placed[p]; ok {
		return r.path, r.ok
	}
	path, ok := m.run(p)
	m.placed[p] = placedPath{path, ok}
	return path, ok
}

func (m *msysSession) run(arg string) (string, bool) {
	if m.runs >= m.mode.maxRuns || m.spent >= m.mode.total {
		return "", false
	}
	if !m.exeKnown {
		m.exeKnown = true
		if m.mode.find != nil {
			m.exe, _ = m.mode.find(m)
		}
	}
	if m.exe == "" || m.mode.run == nil {
		return "", false
	}
	m.runs++
	limit := min(m.mode.perRun, m.mode.total-m.spent)
	start := time.Now()
	out, err := m.mode.run(m.exe, arg, limit)
	m.spent += time.Since(start)
	if err != nil {
		return "", false
	}
	return parseCygpath(out)
}

// refuse reports whether the candidate cygpath is inside a repository the
// call touches: a program there could be planted by the repository. That
// is a directory of the call (its working directory, the repository that
// holds it, each repository loaded for it).
func (m *msysSession) refuse(exe string) bool {
	cands := []string{exe}
	if r, err := filepath.EvalSymlinks(exe); err == nil && r != exe {
		cands = append(cands, r)
	}
	for _, p := range cands {
		for _, r := range m.roots {
			roots := []string{r}
			if rr, err := filepath.EvalSymlinks(r); err == nil && rr != r {
				roots = append(roots, rr)
			}
			for _, root := range roots {
				if rel, err := filepath.Rel(root, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return true
				}
			}
		}
	}
	return false
}

// parseCygpath is the one line cygpath printed, when it is one absolute,
// valid UTF-8 path.
func parseCygpath(out []byte) (string, bool) {
	s := string(out)
	if !utf8.ValidString(s) || strings.ContainsRune(s, utf8.RuneError) {
		return "", false
	}
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
		return "", false
	}
	if !filepath.IsAbs(s) {
		return "", false
	}
	return s, true
}

// findCygpath looks for cygpath: beside CLAUDE_CODE_GIT_BASH_PATH, then in
// the installation the git that runs is in (walking up at most three
// levels to usr\bin), then on PATH. A candidate m refuses is passed over.
func findCygpath(m *msysSession) (string, bool) {
	try := func(p string) (string, bool) {
		if !filepath.IsAbs(p) {
			return "", false
		}
		if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() || m.refuse(p) { //nolint:gosec // G703: a stat of a candidate program name
			return "", false
		}
		return p, true
	}
	walk := func(dir string) (string, bool) {
		for range 4 {
			if p, ok := try(filepath.Join(dir, "usr", "bin", cygpathName)); ok {
				return p, true
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		return "", false
	}
	if bash := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); bash != "" {
		dir := filepath.Dir(bash)
		if p, ok := try(filepath.Join(dir, cygpathName)); ok {
			return p, true
		}
		if p, ok := walk(dir); ok {
			return p, true
		}
	}
	if g, err := exec.LookPath("git"); err == nil {
		if g, err = filepath.Abs(g); err == nil {
			if p, ok := walk(filepath.Dir(g)); ok {
				return p, true
			}
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if p, ok := try(filepath.Join(dir, cygpathName)); ok {
			return p, true
		}
	}
	return "", false
}

// cygpathWaitDelay is how long a child that holds the output pipe after
// cygpath has exited is waited for.
var cygpathWaitDelay = 250 * time.Millisecond

// maxCygpathOutput bounds what a run may print.
const maxCygpathOutput = 8192

// limitedBuffer collects up to maxCygpathOutput bytes; more is an error.
type limitedBuffer struct {
	b    []byte
	over bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if len(l.b)+len(p) > maxCygpathOutput {
		l.over = true
		return 0, errors.New("output too long")
	}
	l.b = append(l.b, p...)
	return len(p), nil
}

// runCygpath runs exe -w arg: an argv and no shell, the glob of cygpath's
// own runtime off, the output UTF-8, in the program's own directory, and
// not waited for past limit nor, once it exits, for a child holding its
// output.
func runCygpath(exe, arg string, limit time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-w", arg) //nolint:gosec // G204: the program is found by findCygpath and the argv is fixed
	cmd.Dir = filepath.Dir(exe)
	cmd.Env = cygpathEnv(os.Environ())
	cmd.WaitDelay = cygpathWaitDelay
	var out limitedBuffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if out.over {
		return nil, errors.New("output too long")
	}
	return out.b, nil
}

// cygpathEnv is env with MSYS=noglob and LC_ALL=C.UTF-8 and no other
// locale or MSYS setting.
func cygpathEnv(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(name, "MSYS") || strings.EqualFold(name, "LANG") || strings.HasPrefix(strings.ToUpper(name), "LC_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "MSYS=noglob", "LC_ALL=C.UTF-8")
}
