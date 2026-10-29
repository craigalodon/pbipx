package source

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
)

// The value planted in these tests. It must never appear in an error.
const sentinel = "Doe, Jane"

// isolateGit removes every GIT_ variable for the test (a pre-commit hook
// sets GIT_INDEX_FILE, which would point git at another index) and turns
// off user and system configuration.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") {
			t.Setenv(name, value) // restored after the test
			_ = os.Unsetenv(name)
		}
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
}

// newRepo writes files into a fresh directory and commits them, leaving
// untracked those listed.
func newRepo(t *testing.T, files map[string]string, untracked ...string) string {
	t.Helper()
	isolateGit(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		write(t, root, name, content)
	}
	if err := corpus.GitSetup(root, untracked, nil); err != nil {
		t.Fatal(err)
	}
	return root
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: the tests' own fixed git commands
	cmd.Dir = dir
	cmd.Env = corpus.GitEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v", args[0], err)
	}
}

func open(t *testing.T, root string) *Repo {
	t.Helper()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func readAll(t *testing.T, s *Set) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range s.Paths {
		data, err := s.Read(p)
		if err != nil {
			out[p] = "<unreadable>"
			continue
		}
		out[p] = string(data)
	}
	return out
}

func TestOpenNotARepository(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	if _, err := Open(dir); !errors.Is(err, ErrNotARepository) {
		t.Errorf("Open outside a repository: %v, want ErrNotARepository", err)
	}
}

func TestOpenFromSubdirectory(t *testing.T) {
	root := newRepo(t, map[string]string{"notes/a.md": "x\n"})
	r := open(t, filepath.Join(root, "notes"))
	if r.Root != root {
		t.Errorf("Root = %q, want the work tree's root", r.Root)
	}
}

func TestAll(t *testing.T) {
	root := newRepo(t, map[string]string{
		".gitignore":                "*.log\nbuild/\n",
		"notes/tracked.md":          "tracked\n",
		"notes/untracked.md":        "untracked\n",
		"notes/ignored.log":         "ignored outside a root\n",
		"pbip/A.Report/report.json": "{}",
		"pbip/A.Report/debug.log":   "ignored under a root\n",
		"pbip/build/cache.bin":      "ignored directory under a root\n",
		"gone.md":                   "deleted from the working tree\n",
	}, "notes/untracked.md", "pbip/A.Report/debug.log")
	if err := os.Remove(filepath.Join(root, "gone.md")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	write(t, outside, "secret.md", sentinel)
	if err := os.Symlink(outside, filepath.Join(root, "pbip", "linked")); err != nil {
		t.Skip("symlinks are not available")
	}

	set, err := open(t, root).All([]string{"pbip"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	want := []string{
		".gitignore",
		"notes/tracked.md",
		"notes/untracked.md",
		"pbip/A.Report/debug.log",
		"pbip/A.Report/report.json",
		"pbip/build/cache.bin",
		"pbip/linked",
	}
	if !slices.Equal(set.Paths, want) {
		t.Errorf("paths = %v, want %v", set.Paths, want)
	}
	got := readAll(t, set)
	if got["pbip/linked"] != outside {
		t.Errorf("a symlink reads as its target path, not through it")
	}
	if got["notes/untracked.md"] != "untracked\n" {
		t.Errorf("an untracked file reads from disk")
	}
}

func TestUnder(t *testing.T) {
	root := newRepo(t, map[string]string{
		".gitignore":                "*.log\n",
		"notes/tracked.md":          "outside every directory given\n",
		"pbip/A.Report/report.json": "{}",
		"pbip/A.Report/debug.log":   "ignored, still on disk\n",
		"other/x.sql":               "select 1\n",
	})
	outside := t.TempDir()
	write(t, outside, "secret.md", sentinel)
	if err := os.Symlink(outside, filepath.Join(root, "pbip", "linked")); err != nil {
		t.Skip("symlinks are not available")
	}
	set, err := open(t, root).Under([]string{"pbip", "missing", "other"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	want := []string{"other/x.sql", "pbip/A.Report/debug.log", "pbip/A.Report/report.json", "pbip/linked"}
	if !slices.Equal(set.Paths, want) {
		t.Errorf("paths = %v, want %v", set.Paths, want)
	}
	if got := readAll(t, set); got["pbip/linked"] != outside {
		t.Errorf("a symlink reads as its target path, not through it")
	}
}

func TestStagedReadsTheIndex(t *testing.T) {
	root := newRepo(t, map[string]string{"notes/a.md": "committed\n"})
	write(t, root, "notes/a.md", "staged\n")
	write(t, root, "notes/b.md", "new and staged\n")
	gitIn(t, root, "add", "--", "notes/a.md", "notes/b.md")
	write(t, root, "notes/a.md", "working tree only\n")
	write(t, root, "notes/c.md", "never staged\n")

	set, err := open(t, root).Staged()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	got := readAll(t, set)
	want := map[string]string{"notes/a.md": "staged\n", "notes/b.md": "new and staged\n"}
	if len(got) != len(want) || got["notes/a.md"] != want["notes/a.md"] || got["notes/b.md"] != want["notes/b.md"] {
		t.Errorf("staged content = %q, want %q", got, want)
	}
}

func TestRev(t *testing.T) {
	root := newRepo(t, map[string]string{"notes/a.md": "first\n", "notes/sub/b.md": "nested\n"})
	write(t, root, "notes/a.md", "second\n")
	r := open(t, root)

	set, err := r.Rev("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	got := readAll(t, set)
	if got["notes/a.md"] != "first\n" || got["notes/sub/b.md"] != "nested\n" {
		t.Errorf("HEAD's tree = %q", got)
	}
	if _, err := set.Read("notes/missing.md"); err == nil {
		t.Error("a path not in the tree was read")
	}

	for _, rev := range []string{sentinel, "--output=" + sentinel, ""} {
		_, err := r.Rev(rev)
		if !errors.Is(err, ErrBadRevision) {
			t.Errorf("a bad revision: %v, want ErrBadRevision", err)
		}
		if err != nil && strings.Contains(err.Error(), "Doe") {
			t.Error("the error quotes the revision")
		}
	}
}

func TestPaths(t *testing.T) {
	root := newRepo(t, map[string]string{
		"notes/a.md":     "a\n",
		"notes/sub/b.md": "b\n",
		"other/c.md":     "c\n",
	})
	r := open(t, root)
	set, err := r.Paths(filepath.Join(root, "notes"), []string{".", "../other/c.md"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	want := []string{"notes/a.md", "notes/sub/b.md", "other/c.md"}
	if !slices.Equal(set.Paths, want) {
		t.Errorf("paths = %v, want %v", set.Paths, want)
	}

	for _, arg := range []string{"../" + sentinel, filepath.Join(root, "notes", sentinel)} {
		_, err := r.Paths(root, []string{"notes/a.md", arg})
		var pe *PathError
		if !errors.As(err, &pe) || pe.Index != 1 {
			t.Errorf("a bad path: %v, want a PathError for path 2", err)
		}
		if err != nil && strings.Contains(err.Error(), "Doe") {
			t.Error("the error quotes the path")
		}
	}
}

func TestPathsSkipsGitDirectory(t *testing.T) {
	root := newRepo(t, map[string]string{"notes/a.md": "a\n"})
	set, err := open(t, root).Paths(root, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	for _, p := range set.Paths {
		if strings.HasPrefix(p, ".git/") {
			t.Fatalf("the walk entered .git: %s", p)
		}
	}
}

func TestSpecialFileIsNotRead(t *testing.T) {
	root := newRepo(t, map[string]string{"pbip/a.json": "{}"})
	if err := os.MkdirAll(filepath.Join(root, "pbip", "dir.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	d, err := open(t, root).disk()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.root.Close() }()
	if _, err := d.read("pbip/dir.json"); err == nil {
		t.Error("a directory was read as a file")
	}
}

func TestCommentChar(t *testing.T) {
	root := newRepo(t, map[string]string{"notes/a.md": "a\n"})
	r := open(t, root)
	if got := r.CommentChar(); got != "#" {
		t.Errorf("default = %q, want #", got)
	}
	gitIn(t, root, "config", "core.commentChar", ";")
	if got := r.CommentChar(); got != ";" {
		t.Errorf("core.commentChar = %q, want ;", got)
	}
	gitIn(t, root, "config", "core.commentChar", "auto")
	if got := r.CommentChar(); got != "#" {
		t.Errorf("auto = %q, want #", got)
	}
}

func TestCommitMessages(t *testing.T) {
	root := newRepo(t, map[string]string{"notes/a.md": "a\n"})
	gitIn(t, root, "tag", "base")
	gitIn(t, root, "commit", "--allow-empty", "-m", "first subject\n\nbody "+sentinel)
	gitIn(t, root, "commit", "--allow-empty", "-m", "second subject\n\n# a line git -m keeps")
	r := open(t, root)

	got, err := r.CommitMessages("base..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2", len(got))
	}
	if !strings.HasPrefix(string(got[0].Message), "second subject\n\n# a line git -m keeps") ||
		!strings.HasPrefix(string(got[1].Message), "first subject\n\nbody "+sentinel) {
		t.Errorf("the messages are not read whole, newest first")
	}
	for _, c := range got {
		if !commitID.MatchString(c.ID) {
			t.Errorf("commit id %q is not a full object name", c.ID)
		}
	}

	none, err := r.CommitMessages("HEAD..HEAD")
	if err != nil || len(none) != 0 {
		t.Errorf("an empty range: %d commits, %v", len(none), err)
	}

	for _, rng := range []string{sentinel + "..HEAD", "--output=" + sentinel, "-n1", ""} {
		_, err := r.CommitMessages(rng)
		if !errors.Is(err, ErrBadRange) {
			t.Errorf("a bad range: %v, want ErrBadRange", err)
		}
		if err != nil && strings.Contains(err.Error(), "Doe") {
			t.Error("the error quotes the range")
		}
	}
}

// shallowClone commits four messages, the oldest holding the sentinel, and
// clones them two deep: the clone lacks the two oldest commits.
func shallowClone(t *testing.T) string {
	t.Helper()
	origin := newRepo(t, map[string]string{"notes/a.md": "a\n"})
	gitIn(t, origin, "commit", "--allow-empty", "-m", "subject\n\nbody "+sentinel)
	gitIn(t, origin, "commit", "--allow-empty", "-m", "third")
	gitIn(t, origin, "commit", "--allow-empty", "-m", "fourth")
	clone, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, clone, "clone", "--quiet", "--depth=2", "file://"+filepath.ToSlash(origin), ".")
	return clone
}

func TestCommitMessagesRefusesShallowClone(t *testing.T) {
	r := open(t, shallowClone(t))
	for _, rng := range []string{"HEAD~1..HEAD", "HEAD"} {
		got, err := r.CommitMessages(rng)
		if !errors.Is(err, ErrIncompleteHistory) {
			t.Errorf("%s in a shallow clone: %d commits, %v; want ErrIncompleteHistory", rng, len(got), err)
		}
		if err != nil && strings.Contains(err.Error(), "HEAD") {
			t.Error("the error quotes the range")
		}
	}
}

func TestCommitMessagesRefusesMissingParent(t *testing.T) {
	root := shallowClone(t)
	// Without its shallow file the clone no longer says it is shallow, and
	// its oldest commit's parent is simply missing. git log stops at it (a
	// bad range) or the parent check does (incomplete history); either way
	// nothing is listed.
	if err := os.Remove(filepath.Join(root, ".git", "shallow")); err != nil {
		t.Fatal(err)
	}
	r := open(t, root)
	got, err := r.CommitMessages("HEAD")
	if err == nil {
		t.Fatalf("a range whose parent is missing listed %d commits", len(got))
	}
	if !errors.Is(err, ErrIncompleteHistory) && !errors.Is(err, ErrBadRange) {
		t.Errorf("a missing parent: %v", err)
	}
}

func TestGitErrorQuotesNothing(t *testing.T) {
	err := &GitError{Op: "ls-files", Exit: 128}
	if err.Error() != "git ls-files failed (exit 128)" {
		t.Errorf("message = %q", err.Error())
	}
}
