package approval

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// sentinel is planted in the policy. No record may hold it.
const sentinel = "Doe, Jane"

const testPolicy = "version = 1\nprotected_roots = [\"pbip\"]\n\n[values]\nallow = [\"" + sentinel + "\"]\n"

// setup is a repository root with a policy, and an empty store.
func setup(t *testing.T) (root, policyPath string, s *Store) {
	t.Helper()
	root = t.TempDir()
	policyPath = filepath.Join(root, ".pbipx.toml")
	if err := os.WriteFile(policyPath, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, policyPath, &Store{Dir: filepath.Join(t.TempDir(), "pbipx")}
}

func approve(t *testing.T, s *Store, root, policyPath string) {
	t.Helper()
	digest, err := DigestFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(root, digest, time.Date(2100, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func status(t *testing.T, s *Store, root, policyPath string) Status {
	t.Helper()
	st, err := s.Status(root, policyPath)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestRoundTrip(t *testing.T) {
	root, policyPath, s := setup(t)
	if got := status(t, s, root, policyPath); got != Unapproved {
		t.Fatalf("before approval: %s", got)
	}
	approve(t, s, root, policyPath)
	if got := status(t, s, root, policyPath); got != Approved {
		t.Fatalf("after approval: %s", got)
	}
	recorded, ok, err := s.Recorded(root)
	if err != nil || !ok {
		t.Fatalf("Recorded = %v, %v", ok, err)
	}
	if want := Digest([]byte(testPolicy)); recorded != want {
		t.Errorf("recorded digest = %s, want %s", recorded, want)
	}

	// A one-byte edit, even in a comment, changes the digest.
	if err := os.WriteFile(policyPath, []byte(testPolicy+"# a comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, root, policyPath); got != Changed {
		t.Errorf("after an edit: %s", got)
	}
	approve(t, s, root, policyPath)
	if got := status(t, s, root, policyPath); got != Approved {
		t.Errorf("after re-approval: %s", got)
	}

	if err := os.Remove(policyPath); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, root, policyPath); got != Missing {
		t.Errorf("without a policy: %s", got)
	}
}

func TestRecordFormat(t *testing.T) {
	root, policyPath, s := setup(t)
	approve(t, s, root, policyPath)
	key, err := Key(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "approved", key)) //nolint:gosec // G304: the test's own store
	if err != nil {
		t.Fatal(err)
	}
	want := "version = 1\ndigest = '" + Digest([]byte(testPolicy)) + "'\napproved = 2100-01-02T03:04:05Z\n"
	if string(data) != want {
		t.Errorf("record = %q, want %q", data, want)
	}
	for _, leak := range []string{sentinel, "Doe", "allow", "protected_roots", filepath.Base(root)} {
		if strings.Contains(string(data), leak) {
			t.Errorf("the record holds %q", leak)
		}
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	root, policyPath, s := setup(t)
	approve(t, s, root, policyPath)
	key, _ := Key(root)
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
	root, policyPath, s := setup(t)
	approve(t, s, root, policyPath)
	approve(t, s, root, policyPath)
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
	if err := s.Approve(root, Digest([]byte("other")), time.Now()); err == nil {
		t.Fatal("Approve wrote into a read-only directory")
	}
	if got := status(t, s, root, policyPath); got != Approved {
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

func TestApprovalIsPerRoot(t *testing.T) {
	root, policyPath, s := setup(t)
	approve(t, s, root, policyPath)
	clone := t.TempDir()
	clonePolicy := filepath.Join(clone, ".pbipx.toml")
	if err := os.WriteFile(clonePolicy, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := status(t, s, clone, clonePolicy); got != Unapproved {
		t.Errorf("a second clone with the same policy is %s", got)
	}
}

func TestBadRecord(t *testing.T) {
	for name, content := range map[string]string{
		"not toml":       "version = \n",
		"other version":  "version = 2\ndigest = '" + Digest(nil) + "'\napproved = 2100-01-01T00:00:00Z\n",
		"bad digest":     "version = 1\ndigest = 'md5:00'\napproved = 2100-01-01T00:00:00Z\n",
		"unknown key":    "version = 1\ndigest = '" + Digest(nil) + "'\napproved = 2100-01-01T00:00:00Z\npolicy = 'x'\n",
		"no version key": "digest = '" + Digest(nil) + "'\n",
	} {
		t.Run(name, func(t *testing.T) {
			root, policyPath, s := setup(t)
			approve(t, s, root, policyPath)
			key, _ := Key(root)
			if err := os.WriteFile(filepath.Join(s.Dir, "approved", key), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Status(root, policyPath); !errors.Is(err, ErrBadRecord) {
				t.Errorf("Status error = %v, want ErrBadRecord", err)
			}
		})
	}
}

func TestApproveRejectsABadDigest(t *testing.T) {
	root, _, s := setup(t)
	if err := s.Approve(root, "sha256:"+sentinel, time.Now()); err == nil || strings.Contains(err.Error(), sentinel) {
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
	root, policyPath, s := setup(t)
	approve(t, s, root, policyPath)
	if st, err := s.Check(root, Digest([]byte(testPolicy))); err != nil || st != Approved {
		t.Errorf("Check of the approved bytes = %s, %v", st, err)
	}
	if st, err := s.Check(root, Digest([]byte(testPolicy+"# edited\n"))); err != nil || st != Changed {
		t.Errorf("Check of other bytes = %s, %v; want changed", st, err)
	}
}
