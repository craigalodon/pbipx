// Package source lists the files a scan covers and reads their content,
// through git (docs/design/behavior.md, "Commands, output and exit codes"):
// the index (--staged), a commit's tree (--rev), the files git would commit
// plus everything on disk under a protected root (--all), or paths named on
// the command line.
//
// git runs from os/exec with an argument vector, never a shell. Its standard
// error is discarded, never put into an error: it can quote paths, refs and
// content. Errors here carry the git subcommand and its exit status only.
package source

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ErrNotARepository is returned by Open when the directory is not inside a
// git work tree.
var ErrNotARepository = errors.New("not in a git repository")

// ErrBadRevision is returned when --rev does not name a commit.
var ErrBadRevision = errors.New("the revision does not name a commit")

// GitError is a git command that failed. It names the subcommand and the
// exit status, never git's output.
type GitError struct {
	Op   string // the git subcommand, such as "ls-files"
	Exit int    // git's exit status; -1 when git did not run
}

func (e *GitError) Error() string {
	if e.Exit < 0 {
		return fmt.Sprintf("git %s could not run", e.Op)
	}
	return fmt.Sprintf("git %s failed (exit %d)", e.Op, e.Exit)
}

// PathError is a path named on the command line that cannot be scanned. It
// says why, and does not quote the path: a command line can carry a value.
type PathError struct {
	Index int    // the position of the path among those given, from 0
	Msg   string // "is outside the repository", "does not exist"
}

func (e *PathError) Error() string {
	return fmt.Sprintf("path %d %s", e.Index+1, e.Msg)
}

// Repo is a git work tree.
type Repo struct {
	// Root is the work tree's absolute path, with symlinks resolved.
	Root string
}

// Open finds the work tree that dir is in.
func Open(dir string) (*Repo, error) {
	out, err := git(dir, "rev-parse", "rev-parse", "--show-toplevel")
	if err != nil {
		var ge *GitError
		if errors.As(err, &ge) && ge.Exit > 0 {
			return nil, ErrNotARepository
		}
		return nil, err
	}
	top := strings.TrimRight(string(out), "\r\n")
	if top == "" {
		return nil, ErrNotARepository
	}
	root, err := filepath.EvalSymlinks(filepath.FromSlash(top))
	if err != nil {
		return nil, ErrNotARepository
	}
	return &Repo{Root: root}, nil
}

// defaultComment is git's default comment character.
const defaultComment = "#"

// CommentChar is the comment string git uses for this repository's commit
// messages: core.commentString, else core.commentChar, else "#". "auto"
// (git picks a character the message does not use) and any failure to read
// the configuration give "#": a scissors line written with another
// character then does not stop a scan, which fails towards scanning more.
func (r *Repo) CommentChar() string {
	for _, key := range []string{"core.commentString", "core.commentChar"} {
		out, err := git(r.Root, "config", "config", "--get", key)
		if err != nil {
			continue
		}
		v := strings.TrimRight(string(out), "\r\n")
		switch {
		case v == "auto":
			return defaultComment
		case v != "" && !strings.ContainsAny(v, "\r\n"):
			return v
		}
	}
	return defaultComment
}

// Set is the files of one scan: their repository-relative paths, sorted,
// with forward slashes, and a way to read each.
type Set struct {
	Paths []string
	read  func(path string) ([]byte, error)
	close func() error
}

// Read returns the content of one of the set's paths.
func (s *Set) Read(path string) ([]byte, error) { return s.read(path) }

// Close releases what reading needed (a git process, an open root).
func (s *Set) Close() error {
	if s.close == nil {
		return nil
	}
	return s.close()
}

// Staged is the content of the index, with no fallback to the working tree.
// Each entry is read as git stored it.
func (r *Repo) Staged() (*Set, error) {
	out, err := git(r.Root, "ls-files", "ls-files", "-z", "--stage")
	if err != nil {
		return nil, err
	}
	objects := map[string]string{}
	for rec := range strings.SplitSeq(string(out), "\x00") {
		if rec == "" {
			continue
		}
		// <mode> SP <object> SP <stage> TAB <path>
		meta, path, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, &GitError{Op: "ls-files", Exit: 0}
		}
		// During a merge one path has several stages; any of them is index
		// content, and stage 2 ("ours") or the only one is kept. Each is
		// read, so a conflicting stage is never skipped.
		key := path
		if _, dup := objects[key]; dup {
			key = path + "\x00" + fields[2]
		}
		objects[key] = fields[1]
	}
	return r.objectSet(objects)
}

// Rev is the tree of the commit rev names.
func (r *Repo) Rev(rev string) (*Set, error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return nil, ErrBadRevision
	}
	out, err := git(r.Root, "rev-parse", "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		var ge *GitError
		if errors.As(err, &ge) && ge.Exit > 0 {
			return nil, ErrBadRevision
		}
		return nil, err
	}
	commit := strings.TrimSpace(string(out))
	out, err = git(r.Root, "ls-tree", "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	objects := map[string]string{}
	for rec := range strings.SplitSeq(string(out), "\x00") {
		if rec == "" {
			continue
		}
		// <mode> SP <type> SP <object> TAB <path>
		meta, path, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, &GitError{Op: "ls-tree", Exit: 0}
		}
		objects[path] = fields[2]
	}
	return r.objectSet(objects)
}

// ErrBadRange is returned when --commits does not name commits git can
// list.
var ErrBadRange = errors.New("the range does not name commits")

// ErrIncompleteHistory is returned when --commits cannot be listed whole:
// the repository is a shallow clone, or a listed commit's parent is not in
// it. git would list only the commits it has, and a range scanned in part
// must not pass as scanned.
var ErrIncompleteHistory = errors.New("the repository's history is incomplete")

// Commit is one commit's message.
type Commit struct {
	ID      string // the full object name
	Message []byte // as git stores it: the subject, a blank line, the body
}

// commitID is a full SHA-1 or SHA-256 object name.
var commitID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// CommitMessages is the message of every commit in rng, a range as git log
// takes it (base..head), newest first. The range is passed after
// --end-of-options, and one that starts with "-" is refused before git
// runs. A shallow clone, or a listed commit whose parent is missing, is
// ErrIncompleteHistory: the range could hold commits git cannot list.
func (r *Repo) CommitMessages(rng string) ([]Commit, error) {
	if rng == "" || strings.HasPrefix(rng, "-") {
		return nil, ErrBadRange
	}
	shallow, err := git(r.Root, "rev-parse", "rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(shallow)) != "false" {
		return nil, ErrIncompleteHistory
	}
	// -z ends each record with a NUL, and the format puts a NUL between
	// the object name, the parents and the message. A signature check
	// would print to standard output, so it is turned off whatever the
	// configuration says.
	out, err := git(r.Root, "log", "log", "-z", "--no-show-signature", "--no-color",
		"--format=%H%x00%P%x00%B", "--end-of-options", rng, "--")
	if err != nil {
		var ge *GitError
		if errors.As(err, &ge) && ge.Exit > 0 {
			return nil, ErrBadRange
		}
		return nil, err
	}
	fields := strings.Split(string(out), "\x00")
	if len(fields)%3 != 1 || fields[len(fields)-1] != "" {
		return nil, &GitError{Op: "log", Exit: 0}
	}
	commits := make([]Commit, 0, len(fields)/3)
	var parents []string
	for i := 0; i+2 < len(fields); i += 3 {
		// A message holding a NUL would shift every field after it; that
		// shows as a field that is not an object name, and nothing is
		// returned rather than a misread message.
		if !commitID.MatchString(fields[i]) {
			return nil, &GitError{Op: "log", Exit: 0}
		}
		for _, p := range strings.Fields(fields[i+1]) {
			if !commitID.MatchString(p) {
				return nil, &GitError{Op: "log", Exit: 0}
			}
			parents = append(parents, p)
		}
		commits = append(commits, Commit{ID: fields[i], Message: []byte(fields[i+2])})
	}
	if err := r.haveCommits(parents); err != nil {
		return nil, err
	}
	return commits, nil
}

// haveCommits returns ErrIncompleteHistory unless every id names a commit
// in the object store.
func (r *Repo) haveCommits(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	cmd := exec.Command("git", "cat-file", "--batch-check=%(objectname) %(objecttype)")
	cmd.Dir = r.Root
	cmd.Stderr = nil
	cmd.Stdin = strings.NewReader(strings.Join(ids, "\n") + "\n")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return gitError("cat-file", err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != len(ids) {
		return &GitError{Op: "cat-file", Exit: 0}
	}
	for _, l := range lines {
		// "<oid> commit", or "<oid> missing".
		if f := strings.Fields(l); len(f) != 2 || f[1] != "commit" {
			return ErrIncompleteHistory
		}
	}
	return nil
}

// objectSet is a set read from git's object store. A key may carry a stage
// after a NUL; the path is the part before it.
func (r *Repo) objectSet(objects map[string]string) (*Set, error) {
	keys := make([]string, 0, len(objects))
	for k := range objects {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var cat *catFile
	paths := make([]string, 0, len(keys))
	byPath := map[string][]string{}
	for _, k := range keys {
		path, _, _ := strings.Cut(k, "\x00")
		if len(byPath[path]) == 0 {
			paths = append(paths, path)
		}
		byPath[path] = append(byPath[path], objects[k])
	}
	s := &Set{Paths: paths}
	s.read = func(path string) ([]byte, error) {
		oids, ok := byPath[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		if cat == nil {
			c, err := startCatFile(r.Root)
			if err != nil {
				return nil, err
			}
			cat = c
		}
		// Several stages of one path are read as one text, so each is
		// scanned and none is skipped.
		var data []byte
		for i, oid := range oids {
			b, err := cat.read(oid)
			if err != nil {
				return nil, err
			}
			if i > 0 {
				data = append(data, '\n')
			}
			data = append(data, b...)
		}
		return data, nil
	}
	s.close = func() error {
		if cat == nil {
			return nil
		}
		return cat.close()
	}
	return s, nil
}

// All is every file git would commit (tracked, plus untracked files that are
// not ignored) and every file on disk under a protected root, ignored or
// not. Symlinks are not followed. A tracked file deleted from the working
// tree has no content and is left out. Files are read from disk.
func (r *Repo) All(protectedRoots []string) (*Set, error) {
	out, err := git(r.Root, "ls-files", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	d, err := r.disk()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p == "" {
			continue
		}
		info, lerr := d.root.Lstat(filepath.FromSlash(p))
		if lerr != nil && errors.Is(lerr, fs.ErrNotExist) {
			continue
		}
		if lerr == nil && info.IsDir() {
			// A submodule. Its files are not this repository's; under a
			// protected root the walk below finds them on disk.
			continue
		}
		seen[p] = true
	}
	for _, pr := range protectedRoots {
		if err := d.walk(pr, seen); err != nil {
			_ = d.root.Close()
			return nil, err
		}
	}
	return d.set(seen), nil
}

// Under is every file on disk under the given repository-relative
// directories, ignored or not, without following symlinks. A directory that
// does not exist adds nothing. Files are read from disk.
func (r *Repo) Under(dirs []string) (*Set, error) {
	d, err := r.disk()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if err := d.walk(dir, seen); err != nil {
			_ = d.root.Close()
			return nil, err
		}
	}
	return d.set(seen), nil
}

// Paths is the files under the given paths, which are relative to dir or
// absolute: a file is itself, and a directory expands to every file under it
// on disk, without following symlinks. Every path must exist and be inside
// the work tree.
func (r *Repo) Paths(dir string, args []string) (*Set, error) {
	d, err := r.disk()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i, a := range args {
		rel, perr := r.Rel(dir, a)
		if perr != nil {
			_ = d.root.Close()
			return nil, &PathError{Index: i, Msg: "is outside the repository"}
		}
		info, lerr := d.root.Lstat(filepath.FromSlash(rel))
		if lerr != nil {
			_ = d.root.Close()
			return nil, &PathError{Index: i, Msg: "does not exist or cannot be read"}
		}
		if !info.IsDir() {
			seen[rel] = true
			continue
		}
		if err := d.walk(rel, seen); err != nil {
			_ = d.root.Close()
			return nil, err
		}
	}
	return d.set(seen), nil
}

// Rel is path, relative to dir or absolute, as a clean repository-relative
// path with forward slashes ("." for the root itself). It fails when the path
// is outside the work tree. Symlinks in the existing part of the path are
// resolved, so a work tree reached through a link still matches.
func (r *Repo) Rel(dir, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(resolved, filepath.Base(path))
	}
	rel, err := filepath.Rel(r.Root, path)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", errors.New("outside the work tree")
	}
	return rel, nil
}

// disk reads files of the work tree from disk, confined to it.
type disk struct {
	root *os.Root
}

func (r *Repo) disk() (*disk, error) {
	root, err := os.OpenRoot(r.Root)
	if err != nil {
		return nil, err
	}
	return &disk{root: root}, nil
}

// walk adds every file under rel to seen, skipping .git and not following
// symlinks. A missing rel adds nothing.
func (d *disk) walk(rel string, seen map[string]bool) error {
	start := filepath.FromSlash(rel)
	if _, err := d.root.Lstat(start); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fs.WalkDir(d.root.FS(), filepath.ToSlash(start), func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory hides what is under it; report the
			// directory itself, which then fails to read as a file.
			seen[p] = true
			if e != nil && e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if e.Name() == ".git" {
			if e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			return nil
		}
		seen[p] = true
		return nil
	})
}

func (d *disk) set(seen map[string]bool) *Set {
	paths := make([]string, 0, len(seen))
	for p := range seen {
		if p != "." {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	return &Set{Paths: paths, read: d.read, close: d.root.Close}
}

// read returns a file's content as git would commit it: a regular file's
// bytes, or a symlink's target. Anything else (a directory, a pipe, a
// device) is not read, because opening it could block or mean nothing.
func (d *disk) read(path string) ([]byte, error) {
	name := filepath.FromSlash(path)
	info, err := d.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	switch {
	case info.Mode().IsRegular():
		return d.root.ReadFile(name)
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := d.root.Readlink(name)
		if err != nil {
			return nil, err
		}
		return []byte(target), nil
	}
	return nil, errors.New("not a regular file or symlink")
}

// git runs one git command in dir and returns its standard output. op names
// the subcommand for errors. Standard error is discarded.
func git(dir, op string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed git subcommands; operands are repository paths and a checked revision, passed as argv
	cmd.Dir = dir
	cmd.Stderr = nil
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, gitError(op, err)
	}
	return out.Bytes(), nil
}

func gitError(op string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		return &GitError{Op: op, Exit: exitErr.ExitCode()}
	}
	return &GitError{Op: op, Exit: -1}
}
