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

// wcStore is an empty store and a directory standing for a working copy.
func wcStore(t *testing.T) (s *Store, work string) {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "pbipx")}, t.TempDir()
}

func TestWorkingCopyRoundTrip(t *testing.T) {
	s, work := wcStore(t)
	got, err := s.WorkingCopies()
	if err != nil || len(got) != 0 {
		t.Fatalf("an empty store: %v, %v", got, err)
	}
	when := time.Date(2100, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := s.AddWorkingCopy(WorkingCopy{Path: work, GitDir: filepath.Join(work, "g"), Model: "pbip/Example.SemanticModel", Staged: when}); err != nil {
		t.Fatal(err)
	}
	// Adding again replaces: one entry per working copy.
	if err := s.AddWorkingCopy(WorkingCopy{Path: work}); err != nil {
		t.Fatal(err)
	}
	got, err = s.WorkingCopies()
	if err != nil || len(got) != 1 || got[0].Path != ResolveExisting(work) {
		t.Fatalf("WorkingCopies = %+v, %v; want the one working copy", got, err)
	}
	if err := s.RemoveWorkingCopy(work); err != nil {
		t.Fatal(err)
	}
	if got, err = s.WorkingCopies(); err != nil || len(got) != 0 {
		t.Fatalf("after removal: %v, %v", got, err)
	}
	// Removing what is not there is not an error.
	if err := s.RemoveWorkingCopy(work); err != nil {
		t.Errorf("second removal: %v", err)
	}
}

func TestWorkingCopyKeepsFields(t *testing.T) {
	s, work := wcStore(t)
	when := time.Date(2100, 1, 2, 3, 4, 5, 0, time.UTC)
	in := WorkingCopy{Path: ResolveExisting(work), GitDir: filepath.Join(work, "g"), Model: "pbip/Example.SemanticModel", Staged: when}
	if err := s.AddWorkingCopy(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.WorkingCopies()
	if err != nil || len(got) != 1 || !got[0].Staged.Equal(in.Staged) {
		t.Fatalf("WorkingCopies = %+v, %v; want %+v", got, err, in)
	}
	got[0].Staged = in.Staged
	if got[0] != in {
		t.Errorf("WorkingCopies = %+v, want %+v", got[0], in)
	}
}

func TestWorkingCopyRejectsRelativePaths(t *testing.T) {
	s, _ := wcStore(t)
	for _, w := range []WorkingCopy{{Path: ""}, {Path: "copy"}, {Path: "../copy"}, {Path: t.TempDir(), GitDir: "rel/.git"}} {
		if err := s.AddWorkingCopy(w); err == nil {
			t.Errorf("AddWorkingCopy(%+v) succeeded", w)
		}
	}
	if got, err := s.WorkingCopies(); err != nil || len(got) != 0 {
		t.Errorf("a refused add left an entry: %v, %v", got, err)
	}
}

func TestWorkingCopyPermissionsAndAtomicity(t *testing.T) {
	s, work := wcStore(t)
	if err := s.AddWorkingCopy(WorkingCopy{Path: work}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Dir, "stages")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("the registry holds %d files, %v; want 1 and no temporary file", len(entries), err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for path, want := range map[string]os.FileMode{
		s.Dir: 0o700, dir: 0o700, filepath.Join(dir, entries[0].Name()): 0o600,
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

// TestWorkingCopyHoldsPathsOnly checks the entry holds no content: the
// path, and nothing a file under it could add.
func TestWorkingCopyHoldsPathsOnly(t *testing.T) {
	s, work := wcStore(t)
	if err := os.WriteFile(filepath.Join(work, "cache.abf"), []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddWorkingCopy(WorkingCopy{Path: work}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "stages"))
	data, err := os.ReadFile(filepath.Join(s.Dir, "stages", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), sentinel) || strings.Contains(string(data), "cache.abf") {
		t.Errorf("the entry holds file content or a file name")
	}
}

func TestWorkingCopyBrokenRegistry(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"not json", "version = 1"},
		{"unknown key", `{"version":1,"path":"/x","extra":1}`},
		{"unknown version", `{"version":2,"path":"/x"}`},
		{"no path", `{"version":1}`},
		{"relative path", `{"version":1,"path":"x"}`},
		{"unclean path", `{"version":1,"path":"/x/../y"}`},
		{"empty", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, work := wcStore(t)
			if err := s.AddWorkingCopy(WorkingCopy{Path: work}); err != nil {
				t.Fatal(err)
			}
			name := strings.Repeat("a", 64) + ".json"
			if err := os.WriteFile(filepath.Join(s.Dir, "stages", name), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.WorkingCopies(); !errors.Is(err, ErrBadRecord) {
				t.Errorf("error = %v, want ErrBadRecord", err)
			}
		})
	}
}

func TestWorkingCopyRegistryNotADirectory(t *testing.T) {
	s, _ := wcStore(t)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "stages"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkingCopies(); !errors.Is(err, ErrStoreNotDir) {
		t.Errorf("error = %v, want ErrStoreNotDir", err)
	}
}

func TestWorkingCopyUnreadableEntry(t *testing.T) {
	s, work := wcStore(t)
	if err := s.AddWorkingCopy(WorkingCopy{Path: work}); err != nil {
		t.Fatal(err)
	}
	// A directory where an entry's file should be cannot be read.
	name := strings.Repeat("b", 64) + ".json"
	if err := os.Mkdir(filepath.Join(s.Dir, "stages", name), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkingCopies(); err == nil {
		t.Error("an unreadable entry was skipped")
	}
}

// TestWorkingCopyIgnoresStrays checks that a file whose name is not an
// entry's (a temporary file an interrupted write left) is not an entry.
func TestWorkingCopyIgnoresStrays(t *testing.T) {
	s, work := wcStore(t)
	if err := s.AddWorkingCopy(WorkingCopy{Path: work}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "stages", ".approval-123"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.WorkingCopies()
	if err != nil || len(got) != 1 {
		t.Errorf("WorkingCopies = %v, %v", got, err)
	}
}

// TestWorkingCopyDoesNotDisturbApprovals checks the two stay apart: the
// index of adopted work trees and an approval are not touched.
func TestWorkingCopyDoesNotDisturbApprovals(t *testing.T) {
	r, policyPath, s := setup(t)
	approve(t, s, r, policyPath)
	if err := s.AddWorkingCopy(WorkingCopy{Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	roots, anyRecord, err := s.Roots()
	if err != nil || !anyRecord || len(roots) != 1 {
		t.Errorf("Roots = %v, %v, %v", roots, anyRecord, err)
	}
	if got := status(t, s, r, policyPath); got != Approved {
		t.Errorf("status = %s", got)
	}
	if got, err := s.WorkingCopies(); err != nil || len(got) != 1 {
		t.Errorf("WorkingCopies = %v, %v", got, err)
	}
}

// TestRemoveWorkingCopyByAnySpelling checks that an entry added by a path
// through a symlink is removed by the resolved path.
func TestRemoveWorkingCopyByAnySpelling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges")
	}
	s, work := wcStore(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(work, link); err != nil {
		t.Fatal(err)
	}
	if err := s.AddWorkingCopy(WorkingCopy{Path: link}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveWorkingCopy(work); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.WorkingCopies(); len(got) != 0 {
		t.Errorf("the entry added by a link survived removal by the real path: %v", got)
	}
}
