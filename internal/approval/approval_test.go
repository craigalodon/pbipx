package approval

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sentinel is planted in the policy. No record may hold it.
const sentinel = "Doe, Jane"

const testPolicy = "version = 1\nprotected_roots = [\"pbip\"]\n\n[values]\nallow = [\"" + sentinel + "\"]\n"

// setup is a repository with a policy, and an empty store. Its git
// directory is a plain directory named .git: the package does not run git.
func setup(t *testing.T) (r Repository, policyPath string, s *Store) {
	t.Helper()
	r = newRepository(t)
	policyPath = filepath.Join(r.Root, ".pbipx.toml")
	if err := os.WriteFile(policyPath, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	return r, policyPath, &Store{Dir: filepath.Join(t.TempDir(), "pbipx")}
}

// newRepository is an empty work tree with a .git directory.
func newRepository(t *testing.T) Repository {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return Repository{GitDir: filepath.Join(root, ".git"), Root: root}
}

func approve(t *testing.T, s *Store, r Repository, policyPath string) {
	t.Helper()
	digest, err := DigestFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(r, digest, time.Date(2100, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func status(t *testing.T, s *Store, r Repository, policyPath string) Status {
	t.Helper()
	st, err := s.Status(r, policyPath)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestRoundTrip(t *testing.T) {
	r, policyPath, s := setup(t)
	if got := status(t, s, r, policyPath); got != Unapproved {
		t.Fatalf("before approval: %s", got)
	}
	approve(t, s, r, policyPath)
	if got := status(t, s, r, policyPath); got != Approved {
		t.Fatalf("after approval: %s", got)
	}
	recorded, ok, err := s.Recorded(r)
	if err != nil || !ok {
		t.Fatalf("Recorded = %v, %v", ok, err)
	}
	if want := []string{Digest([]byte(testPolicy))}; !slices.Equal(recorded, want) {
		t.Errorf("recorded digests = %v, want %v", recorded, want)
	}

	// A one-byte edit, even in a comment, changes the digest.
	if err := os.WriteFile(policyPath, []byte(testPolicy+"# a comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, r, policyPath); got != Changed {
		t.Errorf("after an edit: %s", got)
	}
	approve(t, s, r, policyPath)
	if got := status(t, s, r, policyPath); got != Approved {
		t.Errorf("after re-approval: %s", got)
	}
	// The policy approved first stays approved: a work tree on a branch
	// that has it still runs.
	if err := os.WriteFile(policyPath, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, r, policyPath); got != Approved {
		t.Errorf("the policy approved first, after another was approved: %s", got)
	}

	if err := os.Remove(policyPath); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, r, policyPath); got != Missing {
		t.Errorf("without a policy: %s", got)
	}
}

func TestRecordFormat(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	key, err := Key(r.GitDir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "approved", key)) //nolint:gosec // G304: the test's own store
	if err != nil {
		t.Fatal(err)
	}
	want := "version = 3\ndigests = ['" + Digest([]byte(testPolicy)) + "']\nroots = ['" + r.Root + "']\n" +
		"approved = 2100-01-02T03:04:05Z\n"
	if string(data) != want {
		t.Errorf("record = %q, want %q", data, want)
	}
	for _, leak := range []string{sentinel, "Doe", "allow", "protected_roots"} {
		if strings.Contains(string(data), leak) {
			t.Errorf("the record holds %q", leak)
		}
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	key, _ := Key(r.GitDir)
	for path, want := range map[string]os.FileMode{
		s.Dir:                                 0o700,
		filepath.Join(s.Dir, "approved"):      0o700,
		filepath.Join(s.Dir, "approved", key): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode %o, want %o", filepath.Base(path), got, want)
		}
	}
}

// TestAtomicWrite checks that a write leaves no temporary file behind, and
// that a failed write leaves the old record whole.
func TestAtomicWrite(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	approve(t, s, r, policyPath)
	dir := filepath.Join(s.Dir, "approved")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the store holds %d files, want 1 (a temporary file was left)", len(entries))
	}

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write")
	}
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // G302: a read-only directory, to make the write fail
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // G302: restored for cleanup
	if err := s.Approve(r, Digest([]byte("other")), time.Now()); err == nil {
		t.Fatal("Approve wrote into a read-only directory")
	}
	if got := status(t, s, r, policyPath); got != Approved {
		t.Errorf("after a failed write: %s, want the old record", got)
	}
}

func TestKeyResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symlinks are not available:", err)
	}
	a, err := Key(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{link, link + string(filepath.Separator), filepath.Join(link, "sub", "..")} {
		if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
		b, err := Key(other)
		if err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Errorf("the key differs through a symlink or an uncleaned path")
		}
	}
	if len(a) != 64 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("the key is not a hex SHA-256")
	}
	other, err := Key(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if other == a {
		t.Error("two roots share a key")
	}
	if _, err := Key(filepath.Join(root, "absent")); err == nil {
		t.Error("a root that does not exist has a key")
	}
}

func TestApprovalIsPerClone(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	clone := newRepository(t)
	clonePolicy := filepath.Join(clone.Root, ".pbipx.toml")
	if err := os.WriteFile(clonePolicy, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, clone, clonePolicy); got != Unapproved {
		t.Errorf("a second clone with the same policy is %s", got)
	}
}

// TestWorkTreesShareApprovals checks that a linked work tree, whose root
// differs but whose git directory is the clone's, is approved by an
// approval in the main work tree, and the other way round.
func TestWorkTreesShareApprovals(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	linked := Repository{GitDir: r.GitDir, Root: filepath.Join(r.Root, ".claude", "worktrees", "wt")}
	if err := os.MkdirAll(linked.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedPolicy := filepath.Join(linked.Root, ".pbipx.toml")
	if err := os.WriteFile(linkedPolicy, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, linked, linkedPolicy); got != Approved {
		t.Errorf("a linked work tree with the approved policy is %s", got)
	}
	// A branch with another policy, approved once in its work tree.
	if err := os.WriteFile(linkedPolicy, []byte(testPolicy+"# branch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, linked, linkedPolicy); got != Changed {
		t.Errorf("a linked work tree with another policy is %s", got)
	}
	approve(t, s, linked, linkedPolicy)
	if got := status(t, s, linked, linkedPolicy); got != Approved {
		t.Errorf("the linked work tree after its approval is %s", got)
	}
	if got := status(t, s, r, policyPath); got != Approved {
		t.Errorf("the main work tree after the linked one's approval is %s", got)
	}
}

func TestMostRecentDigestsKept(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	first := Digest([]byte(testPolicy))
	var digests []string
	for i := range MaxDigests {
		d := Digest(fmt.Appendf(nil, "policy %d", i))
		digests = append(digests, d)
		if err := s.Approve(r, d, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := s.Recorded(r)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, digests) {
		t.Errorf("after %d more approvals the record keeps %d digests; want the %d most recent, oldest first",
			MaxDigests, len(got), MaxDigests)
	}
	if st, _ := s.Check(r, first); st != Changed {
		t.Errorf("the digest pushed out is %s, want changed", st)
	}
	// Approving one already there makes it the most recent.
	if err := s.Approve(r, digests[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Recorded(r)
	if len(got) != MaxDigests || got[len(got)-1] != digests[0] {
		t.Error("a re-approved digest did not move to the end")
	}
}

// TestLegacyRecord checks that a version 1 record, keyed by the work tree,
// still approves its digest, and that the next approval merges it into the
// shared record and removes it.
func TestLegacyRecord(t *testing.T) {
	r, policyPath, s := setup(t)
	legacyKey, err := Key(r.Root)
	if err != nil {
		t.Fatal(err)
	}
	old := Digest([]byte(testPolicy))
	writeRecord(t, s, legacyKey, "version = 1\ndigest = '"+old+"'\napproved = 2100-01-01T00:00:00Z\n")
	if got := status(t, s, r, policyPath); got != Approved {
		t.Fatalf("a legacy record of the policy: %s", got)
	}
	other := Digest([]byte("other policy"))
	if st, _ := s.Check(r, other); st != Changed {
		t.Errorf("another policy against a legacy record: %s, want changed", st)
	}
	if err := s.Approve(r, other, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "approved", legacyKey)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the legacy record is still there after an approval: %v", err)
	}
	got, _, err := s.Recorded(r)
	if err != nil || !slices.Equal(got, []string{old, other}) {
		t.Errorf("Recorded = %d digests, %v; want the legacy one, then the new one", len(got), err)
	}
}

func writeRecord(t *testing.T, s *Store, key, content string) {
	t.Helper()
	dir := filepath.Join(s.Dir, "approved")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBadRecord(t *testing.T) {
	many := strings.Repeat("'"+Digest(nil)+"', ", MaxDigests+1)
	var roots []string
	for i := range MaxRoots + 1 {
		roots = append(roots, fmt.Sprintf("'%s'", filepath.Join(string(filepath.Separator), fmt.Sprint(i))))
	}
	manyRoots := strings.Join(roots, ", ")
	for name, content := range map[string]string{
		"not toml":           "version = \n",
		"other version":      "version = 4\ndigests = ['" + Digest(nil) + "']\napproved = 2100-01-01T00:00:00Z\n",
		"version 2, roots":   "version = 2\ndigests = ['" + Digest(nil) + "']\nroots = ['/r']\napproved = 2100-01-01T00:00:00Z\n",
		"version 3, empty":   "version = 3\n",
		"relative root":      "version = 3\nroots = ['r']\n",
		"unclean root":       "version = 3\nroots = ['/r/../s']\n",
		"too many roots":     "version = 3\nroots = [" + manyRoots + "]\n",
		"version 2, digest":  "version = 2\ndigest = '" + Digest(nil) + "'\napproved = 2100-01-01T00:00:00Z\n",
		"version 1, digests": "version = 1\ndigests = ['" + Digest(nil) + "']\napproved = 2100-01-01T00:00:00Z\n",
		"no digests":         "version = 2\ndigests = []\napproved = 2100-01-01T00:00:00Z\n",
		"too many digests":   "version = 2\ndigests = [" + many + "]\napproved = 2100-01-01T00:00:00Z\n",
		"bad digest":         "version = 1\ndigest = 'md5:00'\napproved = 2100-01-01T00:00:00Z\n",
		"bad digest in set":  "version = 2\ndigests = ['" + Digest(nil) + "', 'md5:00']\napproved = 2100-01-01T00:00:00Z\n",
		"unknown key":        "version = 1\ndigest = '" + Digest(nil) + "'\napproved = 2100-01-01T00:00:00Z\npolicy = 'x'\n",
		"no version key":     "digest = '" + Digest(nil) + "'\n",
	} {
		t.Run(name, func(t *testing.T) {
			r, policyPath, s := setup(t)
			approve(t, s, r, policyPath)
			key, _ := Key(r.GitDir)
			if err := os.WriteFile(filepath.Join(s.Dir, "approved", key), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Status(r, policyPath); !errors.Is(err, ErrBadRecord) {
				t.Errorf("Status error = %v, want ErrBadRecord", err)
			}
		})
	}
}

func TestApproveRejectsABadDigest(t *testing.T) {
	r, _, s := setup(t)
	if err := s.Approve(r, "sha256:"+sentinel, time.Now()); err == nil || strings.Contains(err.Error(), sentinel) {
		t.Errorf("Approve = %v; want an error that does not echo the digest", err)
	}
}

func TestConfigDir(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "config")
	t.Setenv(EnvConfigDir, abs+string(filepath.Separator))
	if got, err := ConfigDir(); err != nil || got != abs {
		t.Errorf("ConfigDir = %q, %v; want the cleaned variable", got, err)
	}
	t.Setenv(EnvConfigDir, "relative/config")
	if _, err := ConfigDir(); !errors.Is(err, ErrNoConfigDir) {
		t.Errorf("a relative %s: %v, want ErrNoConfigDir", EnvConfigDir, err)
	}
	t.Setenv(EnvConfigDir, "")
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user configuration directory")
	}
	if got, err := ConfigDir(); err != nil || got != filepath.Join(base, "pbipx") {
		t.Errorf("an empty %s: %q, %v; want the user configuration directory", EnvConfigDir, got, err)
	}
}

func TestStatusString(t *testing.T) {
	for st, want := range map[Status]string{Approved: "approved", Unapproved: "unapproved", Changed: "changed", Missing: "missing"} {
		if st.String() != want {
			t.Errorf("%d.String() = %q, want %q", int(st), st, want)
		}
	}
}

func TestInRepository(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "config")
	type inCase struct {
		name string
		dir  string
		in   bool
	}
	tests := []inCase{
		{"the root", root, true},
		{"below the root", filepath.Join(root, "sub", "pbipx"), true},
		{"climbing back in", filepath.Join(base, "x", "..", "repo", "cfg"), true},
		{"beside the root", outside, false},
		{"a sibling with the root as a prefix", root + "-config", false},
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err == nil {
		tests = append(tests, inCase{"through a link, not yet there", filepath.Join(link, "new", "pbipx"), true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (&Store{Dir: tt.dir}).InRepository(root); got != tt.in {
				t.Errorf("InRepository = %v, want %v", got, tt.in)
			}
		})
	}

	t.Setenv(EnvConfigDir, filepath.Join(root, "cfg"))
	if _, err := OpenFor(root); !errors.Is(err, ErrInRepository) {
		t.Errorf("OpenFor with the store in the repository: %v, want ErrInRepository", err)
	}
	t.Setenv(EnvConfigDir, outside)
	if s, err := OpenFor(root); err != nil || s.Dir != outside {
		t.Errorf("OpenFor = %v, %v; want the store outside", s, err)
	}
}

func TestCheckDigest(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	if st, err := s.Check(r, Digest([]byte(testPolicy))); err != nil || st != Approved {
		t.Errorf("Check of the approved bytes = %s, %v", st, err)
	}
	if st, err := s.Check(r, Digest([]byte(testPolicy+"# edited\n"))); err != nil || st != Changed {
		t.Errorf("Check of other bytes = %s, %v; want changed", st, err)
	}
}

// TestAdopt checks that Adopt marks the clone adopted, approves nothing,
// keeps the digests already approved, and puts the work tree in the index.
func TestAdopt(t *testing.T) {
	r, policyPath, s := setup(t)
	if ok, err := s.Adopted(r); ok || err != nil {
		t.Fatalf("Adopted before anything = %v, %v", ok, err)
	}
	if err := s.Adopt(r); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Adopted(r); !ok || err != nil {
		t.Errorf("Adopted after Adopt = %v, %v", ok, err)
	}
	if got := status(t, s, r, policyPath); got != Unapproved {
		t.Errorf("after Adopt alone the policy is %s, want unapproved", got)
	}
	roots, anyRecord, err := s.Roots()
	if err != nil || !anyRecord || !slices.Equal(roots, []string{r.Root}) {
		t.Errorf("Roots = %v, %v, %v; want the work tree", roots, anyRecord, err)
	}
	approve(t, s, r, policyPath)
	if err := s.Adopt(r); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, r, policyPath); got != Approved {
		t.Errorf("Adopt after an approval left the policy %s", got)
	}
	// A second work tree of the clone joins the index.
	linked := Repository{GitDir: r.GitDir, Root: t.TempDir()}
	if err := s.Adopt(linked); err != nil {
		t.Fatal(err)
	}
	roots, _, err = s.Roots()
	slices.Sort(roots)
	want := []string{r.Root, linked.Root}
	slices.Sort(want)
	if err != nil || !slices.Equal(roots, want) {
		t.Errorf("Roots = %v, %v; want both work trees", roots, err)
	}
}

// TestAdoptKeepsABadRecord checks that Adopt neither replaces nor reads
// past a record that does not parse, and that Adopted counts it.
func TestAdoptKeepsABadRecord(t *testing.T) {
	r, _, s := setup(t)
	key, err := Key(r.GitDir)
	if err != nil {
		t.Fatal(err)
	}
	writeRecord(t, s, key, "not a record")
	if err := s.Adopt(r); !errors.Is(err, ErrBadRecord) {
		t.Errorf("Adopt = %v, want ErrBadRecord", err)
	}
	if ok, err := s.Adopted(r); !ok || !errors.Is(err, ErrBadRecord) {
		t.Errorf("Adopted = %v, %v; want true and ErrBadRecord", ok, err)
	}
	roots, anyRecord, err := s.Roots()
	if err != nil || !anyRecord || len(roots) != 0 {
		t.Errorf("Roots = %v, %v, %v; want a record with no work tree", roots, anyRecord, err)
	}
}

// TestRootsOfAnEmptyStore checks a store with no records directory, and one
// with files that are not records.
func TestRootsOfAnEmptyStore(t *testing.T) {
	_, _, s := setup(t)
	if roots, anyRecord, err := s.Roots(); len(roots) != 0 || anyRecord || err != nil {
		t.Errorf("Roots of an empty store = %v, %v, %v", roots, anyRecord, err)
	}
	writeRecord(t, s, ".approval-123", "version = 3\nroots = ['/r']\n")
	if roots, anyRecord, err := s.Roots(); len(roots) != 0 || anyRecord || err != nil {
		t.Errorf("Roots with a temporary file = %v, %v, %v", roots, anyRecord, err)
	}
}

// TestRootsKeepsTheNewest checks that a record keeps the MaxRoots most
// recent work trees.
func TestRootsKeepsTheNewest(t *testing.T) {
	r, _, s := setup(t)
	var last string
	for i := range MaxRoots + 2 {
		last = filepath.Join(r.Root, fmt.Sprint("w", i))
		if err := os.Mkdir(last, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := s.Adopt(Repository{GitDir: r.GitDir, Root: last}); err != nil {
			t.Fatal(err)
		}
	}
	roots, _, err := s.Roots()
	if err != nil || len(roots) != MaxRoots || !slices.Contains(roots, last) || slices.Contains(roots, filepath.Join(r.Root, "w0")) {
		t.Errorf("Roots = %d work trees, %v; want the %d newest", len(roots), err, MaxRoots)
	}
}

// TestVersion2Record checks that a record from before the index is still
// read, and that the next approval adds the work tree.
func TestVersion2Record(t *testing.T) {
	r, policyPath, s := setup(t)
	key, err := Key(r.GitDir)
	if err != nil {
		t.Fatal(err)
	}
	digest := Digest([]byte(testPolicy))
	writeRecord(t, s, key, "version = 2\ndigests = ['"+digest+"']\napproved = 2100-01-01T00:00:00Z\n")
	if got := status(t, s, r, policyPath); got != Approved {
		t.Errorf("a version 2 record: %s", got)
	}
	if roots, anyRecord, err := s.Roots(); len(roots) != 0 || !anyRecord || err != nil {
		t.Errorf("Roots = %v, %v, %v; want a record with no work tree", roots, anyRecord, err)
	}
	approve(t, s, r, policyPath)
	if roots, _, err := s.Roots(); err != nil || !slices.Equal(roots, []string{r.Root}) {
		t.Errorf("Roots after an approval = %v, %v", roots, err)
	}
}

// readAsWindows makes the store read a path through a file as Windows
// does: as missing, where Unix reports ENOTDIR.
func readAsWindows(t *testing.T) {
	t.Helper()
	oldFile, oldDir := readFile, readDir
	t.Cleanup(func() { readFile, readDir = oldFile, oldDir })
	asMissing := func(path string, err error) error {
		if errors.Is(err, syscall.ENOTDIR) {
			return &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
		}
		return err
	}
	readFile = func(path string) ([]byte, error) {
		data, err := oldFile(path)
		return data, asMissing(path, err)
	}
	readDir = func(path string) ([]os.DirEntry, error) {
		entries, err := oldDir(path)
		return entries, asMissing(path, err)
	}
}

// TestStoreUnderAFile checks that a store whose directory, or one above
// it, is a file is an error, not an empty store, however the platform
// reports a path through a file.
func TestStoreUnderAFile(t *testing.T) {
	for _, windows := range []bool{false, true} {
		for name, below := range map[string]string{
			"records directory is a file": approvedDir,
			"store is a file":             "",
			"store is under a file":       "pbipx",
		} {
			t.Run(fmt.Sprintf("%s, as Windows %v", name, windows), func(t *testing.T) {
				if windows {
					readAsWindows(t)
				}
				r, policyPath, _ := setup(t)
				file := filepath.Join(t.TempDir(), "file")
				s := &Store{Dir: file}
				if below == approvedDir {
					s.Dir = t.TempDir()
					file = filepath.Join(s.Dir, approvedDir)
				} else if below != "" {
					s.Dir = filepath.Join(file, below)
				}
				if err := os.WriteFile(file, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Status(r, policyPath); !errors.Is(err, ErrStoreNotDir) {
					t.Errorf("Status error = %v, want ErrStoreNotDir", err)
				}
				if _, err := s.Adopted(r); !errors.Is(err, ErrStoreNotDir) {
					t.Errorf("Adopted error = %v, want ErrStoreNotDir", err)
				}
				if _, _, err := s.Roots(); !errors.Is(err, ErrStoreNotDir) {
					t.Errorf("Roots error = %v, want ErrStoreNotDir", err)
				}
			})
		}
	}
}

// recordFor writes content as the record keyed by dir, which must exist, and
// returns the record's path.
func recordFor(t *testing.T, s *Store, dir, content string) string {
	t.Helper()
	key, err := Key(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeRecord(t, s, key, content)
	return filepath.Join(s.Dir, approvedDir, key)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func record3(digests []string, roots ...string) string {
	var b strings.Builder
	b.WriteString("version = 3\n")
	if len(digests) > 0 {
		b.WriteString("digests = ['" + strings.Join(digests, "', '") + "']\n")
	}
	if len(roots) > 0 {
		b.WriteString("roots = ['" + strings.Join(roots, "', '") + "']\n")
	}
	return b.String()
}

func mustAdopted(t *testing.T, s *Store, r Repository, want bool) {
	t.Helper()
	got, err := s.Adopted(r)
	if err != nil || got != want {
		t.Errorf("Adopted = %v, %v; want %v", got, err, want)
	}
}

func TestUnenrollRemovesTheCloneRecord(t *testing.T) {
	v1 := "version = 1\ndigest = '" + Digest(nil) + "'\napproved = 2100-01-01T00:00:00Z\n"
	v2 := "version = 2\ndigests = ['" + Digest(nil) + "']\napproved = 2100-01-01T00:00:00Z\n"
	tests := []struct {
		name    string
		content string
		legacy  bool // keyed by the work tree, as a version 1 record is
	}{
		{"v3", record3([]string{Digest(nil)}, "/some/root"), false},
		{"v2", v2, false},
		{"v1", v1, true},
		{"bad", "not a record = = =\n", false},
		{"bad, keyed by the work tree", "not a record = = =\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, s := setup(t)
			dir := r.GitDir
			if tt.legacy {
				dir = r.Root
			}
			path := recordFor(t, s, dir, tt.content)
			if got, _ := s.Adopted(r); !got {
				t.Fatal("the clone is not adopted to begin with")
			}
			if err := s.Unenroll(r, nil); err != nil {
				t.Fatal(err)
			}
			if exists(path) {
				t.Error("the record is still there")
			}
			mustAdopted(t, s, r, false)
			if roots, any, err := s.Roots(); err != nil || len(roots) != 0 || any {
				t.Errorf("Roots = %v, %v, %v; want none", roots, any, err)
			}
		})
	}
}

func TestUnenrollRemovesBothRecordsOfTheClone(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	legacy := recordFor(t, s, r.Root, "version = 1\ndigest = '"+Digest(nil)+"'\napproved = 2100-01-01T00:00:00Z\n")
	shared := recordFor(t, s, r.GitDir, record3([]string{Digest(nil)}, r.Root))
	if err := s.Unenroll(r, nil); err != nil {
		t.Fatal(err)
	}
	if exists(legacy) || exists(shared) {
		t.Error("a record of the clone is still there")
	}
	mustAdopted(t, s, r, false)
}

func TestUnenrollRemovesALegacyRecordOfAnotherWorkTree(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	other := t.TempDir()
	unrelated := t.TempDir()
	v1 := "version = 1\ndigest = '" + Digest(nil) + "'\napproved = 2100-01-01T00:00:00Z\n"
	legacy := recordFor(t, s, other, v1)
	keep := recordFor(t, s, unrelated, v1)
	if err := s.Unenroll(r, []string{other}); err != nil {
		t.Fatal(err)
	}
	if exists(legacy) {
		t.Error("the legacy record of the clone's other work tree is still there")
	}
	if !exists(keep) {
		t.Error("the legacy record of an unrelated work tree was removed")
	}
}

func TestUnenrollKeepsTheDigestsOfAnotherRecordsRoot(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	d := Digest([]byte("x"))
	// Another clone's record that names this work tree among its own.
	other := recordFor(t, s, t.TempDir(), record3([]string{d}, r.Root, elsewhere))
	if err := s.Unenroll(r, nil); err != nil {
		t.Fatal(err)
	}
	rec, found, err := readRecord(other)
	if err != nil || !found {
		t.Fatalf("the other record: %v, %v", found, err)
	}
	if !slices.Equal(rec.Roots, []string{elsewhere}) || !slices.Equal(rec.Digests, []string{d}) {
		t.Errorf("roots %v, digests %v; want only this work tree dropped", rec.Roots, rec.Digests)
	}
	roots, _, err := s.Roots()
	if err != nil || slices.Contains(roots, r.Root) {
		t.Errorf("Roots = %v, %v; the work tree is still indexed", roots, err)
	}
}

func TestUnenrollDeletesARecordStrippedToNothing(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	other := recordFor(t, s, t.TempDir(), record3(nil, r.Root))
	if err := s.Unenroll(r, nil); err != nil {
		t.Fatal(err)
	}
	// A version 3 record with no digest and no root is invalid, and an
	// invalid record counts as adopted: it must be gone, not left empty.
	if exists(other) {
		t.Error("a record with neither roots nor digests was left behind")
	}
	if _, _, err := s.Roots(); err != nil {
		t.Errorf("Roots: %v", err)
	}
}

// TestUnenrollKeepsADigestsOnlyRecord: a record left with digests and no
// roots is valid, and adopts only its own clone by its key, not a work tree
// it no longer names.
func TestUnenrollKeepsADigestsOnlyRecord(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	d := Digest([]byte("x"))
	other := recordFor(t, s, t.TempDir(), record3([]string{d}, r.Root))
	if err := s.Unenroll(r, nil); err != nil {
		t.Fatal(err)
	}
	rec, found, err := readRecord(other)
	if err != nil || !found || len(rec.Roots) != 0 || !slices.Equal(rec.Digests, []string{d}) {
		t.Fatalf("the other record: %+v, %v, %v", rec, found, err)
	}
	mustAdopted(t, s, r, false)
	if roots, _, _ := s.Roots(); slices.Contains(roots, r.Root) {
		t.Error("the work tree is still indexed")
	}
}

func TestUnenrollLeavesAnotherCloneAndAnUnreadableForeignRecord(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	o := newRepository(t)
	if err := s.Adopt(o); err != nil {
		t.Fatal(err)
	}
	foreign := recordFor(t, s, t.TempDir(), "garbage = = =\n")
	if err := s.Unenroll(r, nil); err != nil {
		t.Fatal(err)
	}
	mustAdopted(t, s, o, true)
	if !exists(foreign) {
		t.Error("an unreadable record that is not this clone's was removed")
	}
}

func TestUnenrollTreatsSpellingsOfARootAlike(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(r.Root, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	d := Digest([]byte("x"))
	viaLink := recordFor(t, s, t.TempDir(), record3(nil, link))
	upper := recordFor(t, s, t.TempDir(), record3([]string{d}, strings.ToUpper(r.Root), "/elsewhere"))
	old := foldCase
	foldCase = true
	t.Cleanup(func() { foldCase = old })
	if err := s.Unenroll(r, nil); err != nil {
		t.Fatal(err)
	}
	if exists(viaLink) {
		t.Error("a root spelled through a symlink was kept")
	}
	rec, _, err := readRecord(upper)
	if err != nil || slices.Contains(rec.Roots, strings.ToUpper(r.Root)) {
		t.Errorf("a root spelled in another case was kept: %v, %v", rec.Roots, err)
	}
}

func TestUnenrollDoesNotFoldCaseWhereCaseCounts(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	d := Digest([]byte("x"))
	root := filepath.Join(filepath.Dir(r.Root), strings.ToUpper(filepath.Base(r.Root)))
	if root == r.Root {
		t.Skip("no letters to change")
	}
	other := recordFor(t, s, t.TempDir(), record3([]string{d}, root))
	old := foldCase
	foldCase = false
	t.Cleanup(func() { foldCase = old })
	if err := s.Unenroll(r, nil); err != nil {
		t.Fatal(err)
	}
	if rec, _, _ := readRecord(other); !slices.Contains(rec.Roots, root) {
		t.Error("a different directory was stripped")
	}
}

func TestUnenrollStripsAMissingWorkTree(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	gone := filepath.Join(t.TempDir(), "gone")
	other := recordFor(t, s, t.TempDir(), record3(nil, gone))
	if err := s.Unenroll(r, []string{gone}); err != nil {
		t.Fatal(err)
	}
	if exists(other) {
		t.Error("a root whose directory is gone was kept")
	}
}

func TestUnenrollErrorLeavesTheCloneAdopted(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	// A directory where a record belongs cannot be read as one.
	bad := filepath.Join(s.Dir, approvedDir, strings.Repeat("a", 64))
	if err := os.Mkdir(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Unenroll(r, nil); err == nil {
		t.Fatal("no error")
	}
	mustAdopted(t, s, r, true)
}

func TestUnenrollNothingToRemove(t *testing.T) {
	r, _, s := setup(t)
	for _, store := range []*Store{s, {Dir: filepath.Join(t.TempDir(), "never made")}} {
		if held, err := store.Holds(r, nil); err != nil || held {
			t.Errorf("Holds = %v, %v; want false", held, err)
		}
		if err := store.Unenroll(r, nil); err != nil {
			t.Errorf("Unenroll: %v", err)
		}
	}
	if err := s.Adopt(r); err != nil {
		t.Fatal(err)
	}
	if held, err := s.Holds(r, nil); err != nil || !held {
		t.Errorf("Holds = %v, %v; want true", held, err)
	}
}

func TestUnenrollStoreUnderAFile(t *testing.T) {
	r, _, _ := setup(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Store{Dir: filepath.Join(file, "pbipx")}
	if err := s.Unenroll(r, nil); !errors.Is(err, ErrStoreNotDir) {
		t.Errorf("Unenroll error = %v, want ErrStoreNotDir", err)
	}
	if _, err := s.Holds(r, nil); !errors.Is(err, ErrStoreNotDir) {
		t.Errorf("Holds error = %v, want ErrStoreNotDir", err)
	}
}
