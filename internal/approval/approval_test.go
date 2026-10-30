package approval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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
	want := "version = 2\ndigests = ['" + Digest([]byte(testPolicy)) + "']\napproved = 2100-01-02T03:04:05Z\n"
	if string(data) != want {
		t.Errorf("record = %q, want %q", data, want)
	}
	for _, leak := range []string{sentinel, "Doe", "allow", "protected_roots", filepath.Base(r.Root)} {
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
	for name, content := range map[string]string{
		"not toml":           "version = \n",
		"other version":      "version = 3\ndigests = ['" + Digest(nil) + "']\napproved = 2100-01-01T00:00:00Z\n",
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
