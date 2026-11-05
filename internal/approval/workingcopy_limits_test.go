package approval

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// useHome makes dir the home directory os.UserHomeDir reports.
func useHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestAddWorkingCopyRefusesPlacesThatAreNotACopy(t *testing.T) {
	s, _ := wcStore(t)
	home := filepath.Join(t.TempDir(), "home", "user")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	useHome(t, home)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	inRepo := filepath.Join(repo, "deep", "copy")
	if err := os.MkdirAll(inRepo, 0o700); err != nil {
		t.Fatal(err)
	}
	// A file named .git (a linked work tree or a submodule) counts too.
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	vol := filepath.VolumeName(home)
	tests := map[string]string{
		"the root":                         vol + string(filepath.Separator),
		"the home folder":                  home,
		"the home folder, unclean":         home + string(filepath.Separator) + ".",
		"a parent of the home folder":      filepath.Dir(home),
		"the parent of the parent":         filepath.Dir(filepath.Dir(home)),
		"a folder holding .git":            repo,
		"a folder inside a work tree":      inRepo,
		"a folder not there yet in a tree": filepath.Join(inRepo, "new", "copy"),
		"a folder holding a .git file":     linked,
	}
	for name, path := range tests {
		t.Run(name, func(t *testing.T) {
			if err := s.AddWorkingCopy(WorkingCopy{Path: path}); !errors.Is(err, ErrNotACopyPlace) {
				t.Errorf("error = %v, want ErrNotACopyPlace", err)
			}
		})
	}
	if got, err := s.WorkingCopies(); err != nil || len(got) != 0 {
		t.Errorf("a refused add left an entry: %v, %v", got, err)
	}
	// Inside the home folder, outside a work tree, is where a copy goes.
	ok := filepath.Join(home, "work", "copy")
	if err := s.AddWorkingCopy(WorkingCopy{Path: ok}); err != nil {
		t.Errorf("a folder in the home folder: %v", err)
	}
}

func TestAddWorkingCopyResolvesBeforeJudging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges")
	}
	s, _ := wcStore(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	if err := s.AddWorkingCopy(WorkingCopy{Path: filepath.Join(link, "copy")}); !errors.Is(err, ErrNotACopyPlace) {
		t.Errorf("a folder in a work tree through a link: %v", err)
	}
}

func TestWorkingCopyCap(t *testing.T) {
	s, _ := wcStore(t)
	base := t.TempDir()
	add := func(i int) error {
		return s.AddWorkingCopy(WorkingCopy{Path: filepath.Join(base, "c"+strconv.Itoa(i))})
	}
	for i := range MaxWorkingCopies {
		if err := add(i); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if err := add(MaxWorkingCopies); !errors.Is(err, ErrTooManyWorkingCopies) {
		t.Errorf("the entry past the cap: %v, want ErrTooManyWorkingCopies", err)
	}
	// Replacing an entry that is there is not adding one.
	if err := add(0); err != nil {
		t.Errorf("replacing at the cap: %v", err)
	}
	// Removing one makes room.
	if err := s.RemoveWorkingCopy(filepath.Join(base, "c1")); err != nil {
		t.Fatal(err)
	}
	if err := add(MaxWorkingCopies); err != nil {
		t.Errorf("after a removal: %v", err)
	}
}

func TestWorkingCopiesReadCap(t *testing.T) {
	s, _ := wcStore(t)
	dir := filepath.Join(s.Dir, "stages")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// An absolute path on every platform: a drive letter on Windows.
	abs, err := json.Marshal(filepath.Join(t.TempDir(), "x"))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"version":1,"path":` + string(abs) + `}`)
	write := func(i int) {
		name := strings.Repeat("0", 64-len(strconv.Itoa(i))) + strconv.Itoa(i) + ".json"
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := range MaxWorkingCopiesRead {
		write(i)
	}
	if got, err := s.WorkingCopies(); err != nil || len(got) != MaxWorkingCopiesRead {
		t.Fatalf("at the read cap: %d, %v", len(got), err)
	}
	write(MaxWorkingCopiesRead)
	if _, err := s.WorkingCopies(); !errors.Is(err, ErrTooManyWorkingCopies) {
		t.Errorf("past the read cap: %v, want ErrTooManyWorkingCopies", err)
	}
}

func TestWorkingCopyErrorNamesTheEntry(t *testing.T) {
	s, work := wcStore(t)
	if err := s.AddWorkingCopy(WorkingCopy{Path: work}); err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("a", 64) + ".json"
	if err := os.WriteFile(filepath.Join(s.Dir, "stages", name), []byte(`{"path": "`+sentinel+`"`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.WorkingCopies()
	var ee *EntryError
	if !errors.As(err, &ee) || ee.Name != name || !errors.Is(err, ErrBadRecord) {
		t.Fatalf("error = %#v, want an EntryError for %s that is ErrBadRecord", err, name)
	}
	if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), s.Dir) {
		t.Errorf("the error prints content or a path: %v", err)
	}
}
