package approval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// The registry of working copies (ADR-0007; docs/design/stage-collect.md).
// A working copy is a folder `pbipx stage` made outside every repository for
// Power BI Desktop to open. Desktop writes real data there (a data cache),
// so the gate denies every path under a registered one, with no scanning.
//
// The registry is in the store, beside the approval records, where the
// gate already denies an agent's write: one file per working copy,
// <store>/stages/<key>.json, with key the SHA-256 of the working copy's
// absolute, symlink-resolved path. An entry holds the working copy's path,
// and optionally the clone's git directory, the model's repository path
// and the date it was staged: paths and a date, never content. Directories
// are 0700, files 0600, and an entry is written through a temporary file
// renamed into place.

// stagesDir is the directory of registry entries inside the configuration
// directory.
const stagesDir = "stages"

// workingCopyVersion is the entry format version.
const workingCopyVersion = 1

// Limits on the registry. Every gated call reads it, so it is small:
// AddWorkingCopy refuses past MaxWorkingCopies entries, and a registry read
// with more than MaxWorkingCopiesRead is an error (the gate denies).
const (
	MaxWorkingCopies     = 64
	MaxWorkingCopiesRead = 256
)

// Errors of the registry. None names a path or holds file content.
var (
	// ErrTooManyWorkingCopies is a registry at its cap on add, or past the
	// read cap on read.
	ErrTooManyWorkingCopies = errors.New("approval: too many working copies are registered")
	// ErrNotACopyPlace is a path that cannot be a working copy: a root, the
	// home folder or a parent of it, or a folder in or holding a git work
	// tree.
	ErrNotACopyPlace = errors.New("approval: the path cannot be a working copy")
)

// EntryError is an entry that could not be read or is not valid. Name is
// the entry's file name in the registry, a hash of the working copy's path,
// which names nothing; Err is ErrBadRecord or the read error.
type EntryError struct {
	Name string
	Err  error
}

func (e *EntryError) Error() string {
	return "approval: the registry entry " + e.Name + " cannot be used"
}

func (e *EntryError) Unwrap() error { return e.Err }

// WorkingCopy is one registry entry.
type WorkingCopy struct {
	// Path is the working copy's folder: absolute and clean. AddWorkingCopy
	// stores it with the symlinks of its existing part resolved.
	Path string
	// GitDir is the git directory of the clone it was staged from, and
	// Model the model's repository path; both optional.
	GitDir string
	Model  string
	// Staged is when it was staged; zero when not given.
	Staged time.Time
}

// entry is the file format.
type entry struct {
	Version int       `json:"version"`
	Path    string    `json:"path"`
	GitDir  string    `json:"git-dir,omitempty"`
	Model   string    `json:"model,omitempty"`
	Staged  time.Time `json:"staged,omitzero"`
}

// entryName is an entry's file name: a hex SHA-256 and .json.
var entryName = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)

// entryKey is the key of the path a, made absolute and clean.
func entryKey(a string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(a)))
	return hex.EncodeToString(sum[:])
}

// AddWorkingCopy registers w. An entry for the same path is replaced. The
// path, and a git directory when there is one, must be absolute; the path
// is stored with the symlinks of its existing part resolved, and need not
// exist yet.
func (s *Store) AddWorkingCopy(w WorkingCopy) error {
	if _, ok := cleanRoot(w.Path); !ok {
		return errors.New("approval: the working copy's path is not absolute")
	}
	if w.GitDir != "" {
		if _, ok := cleanRoot(w.GitDir); !ok {
			return errors.New("approval: the git directory is not absolute")
		}
	}
	path := ResolveExisting(w.Path)
	if err := checkCopyPlace(path); err != nil {
		return err
	}
	key := entryKey(path) + ".json"
	dir := filepath.Join(s.Dir, stagesDir)
	if entries, err := readDir(dir); err == nil {
		n := 0
		for _, e := range entries {
			if !entryName.MatchString(e.Name()) {
				continue
			}
			if e.Name() == key {
				n = -1 << 30 // a replacement, not an addition
			}
			n++
		}
		if n >= MaxWorkingCopies {
			return ErrTooManyWorkingCopies
		}
	}
	data, err := json.Marshal(entry{Version: workingCopyVersion, Path: path, GitDir: w.GitDir, Model: w.Model,
		Staged: w.Staged.UTC().Truncate(time.Second)})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, key), append(data, '\n'))
}

// checkCopyPlace is ErrNotACopyPlace for a resolved absolute path that is a
// file system root, the home folder or a parent of it, or a folder that
// holds a git entry or has one in a folder above it (it is, or would be,
// inside a git work tree).
func checkCopyPlace(path string) error {
	if filepath.Dir(path) == path {
		return ErrNotACopyPlace
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, h := range []string{filepath.Clean(home), ResolveExisting(home)} {
			if Within(path, h) {
				return ErrNotACopyPlace
			}
		}
	}
	for d := path; ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return ErrNotACopyPlace
		}
		if filepath.Dir(d) == d {
			return nil
		}
	}
}

// RemoveWorkingCopy removes the entry for the working copy at path, found
// under any spelling of it: as given, with its symlinks resolved, or as the
// path an entry holds. A path with no entry is not an error.
func (s *Store) RemoveWorkingCopy(path string) error {
	a, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.Dir, stagesDir)
	names := map[string]bool{entryKey(a) + ".json": true, entryKey(ResolveExisting(a)) + ".json": true}
	if entries, err := readDir(dir); err == nil {
		for _, e := range entries {
			if !entryName.MatchString(e.Name()) {
				continue
			}
			if w, found, err := readEntry(filepath.Join(dir, e.Name())); err == nil && found && sameTree(w.Path, a) {
				names[e.Name()] = true
			}
		}
	}
	var errs []error
	for name := range names {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !missing(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// WorkingCopies are the registered working copies, in no set order. A store
// with no registry has none. An entry that does not parse is ErrBadRecord,
// and one that cannot be read, or a registry directory that is not a
// directory (ErrStoreNotDir), is an error too: the gate denies every call
// then, since a working copy it cannot see is one it cannot protect.
func (s *Store) WorkingCopies() ([]WorkingCopy, error) {
	dir := filepath.Join(s.Dir, stagesDir)
	if notDirAt(dir) {
		return nil, ErrStoreNotDir
	}
	entries, err := readDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []WorkingCopy
	for _, e := range entries {
		if !entryName.MatchString(e.Name()) {
			continue
		}
		if len(out) >= MaxWorkingCopiesRead {
			return nil, ErrTooManyWorkingCopies
		}
		w, found, err := readEntry(filepath.Join(dir, e.Name()))
		if err != nil {
			if errors.Is(err, ErrStoreNotDir) {
				return nil, err
			}
			return nil, &EntryError{Name: e.Name(), Err: err}
		}
		if found {
			out = append(out, w)
		}
	}
	return out, nil
}

// readEntry reads and validates one entry; found is false when the file is
// not there. An entry that does not parse, has an unknown key or version,
// or holds a path that is not absolute and clean is ErrBadRecord.
func readEntry(path string) (w WorkingCopy, found bool, err error) {
	data, err := readFile(path)
	if missing(err) {
		if notDirAt(path) {
			return WorkingCopy{}, false, ErrStoreNotDir
		}
		if errors.Is(err, fs.ErrNotExist) {
			return WorkingCopy{}, false, nil
		}
	}
	if err != nil {
		return WorkingCopy{}, false, err
	}
	var e entry
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&e) != nil || dec.Decode(&struct{}{}) != io.EOF || e.Version != workingCopyVersion {
		return WorkingCopy{}, false, ErrBadRecord
	}
	if c, ok := cleanRoot(e.Path); !ok || c != e.Path {
		return WorkingCopy{}, false, ErrBadRecord
	}
	if e.GitDir != "" {
		if c, ok := cleanRoot(e.GitDir); !ok || c != e.GitDir {
			return WorkingCopy{}, false, ErrBadRecord
		}
	}
	return WorkingCopy{Path: e.Path, GitDir: e.GitDir, Model: e.Model, Staged: e.Staged}, true, nil
}
