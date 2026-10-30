// Package approval records which policy a person approved for a repository
// on this machine, and checks the policy against it (ADR-0006; threat model,
// layer 6).
//
// An approval is the SHA-256 digest of the policy file's exact bytes, kept
// outside the repository in
// ${PBIPX_CONFIG_DIR:-<user config dir>/pbipx}/approved/<key>, where the key
// is a hash of the absolute, cleaned, symlink-resolved path of the clone's
// shared git directory, so every work tree of one clone shares it. A record
// holds a format version, the approved digests (at most MaxDigests, the
// most recent last) and the time of the last approval: never policy
// content, never the path in clear.
package approval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// EnvConfigDir names the configuration directory, overriding the user
// configuration directory's pbipx/. It must be absolute; empty is unset.
const EnvConfigDir = "PBIPX_CONFIG_DIR"

// Version is the record format version this package writes. It also reads
// legacyVersion.
const Version = 2

// legacyVersion is the format of a record that holds one digest, keyed by
// the work tree's path.
const legacyVersion = 1

// MaxDigests is how many approved digests a record keeps.
const MaxDigests = 16

// approvedDir is the directory of records inside the configuration
// directory.
const approvedDir = "approved"

// digestPrefix starts every digest: the algorithm, then the hex digest.
const digestPrefix = "sha256:"

var digestShape = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Errors. None names a path or holds file content.
var (
	ErrNoConfigDir = errors.New("approval: no configuration directory: " + EnvConfigDir + " is not absolute, or the user configuration directory is unknown")
	ErrBadRecord   = errors.New("approval: the approval record is not valid")
	// ErrInRepository is a configuration directory inside the repository
	// whose policy it approves, where an agent could write an approval.
	ErrInRepository = errors.New("approval: the configuration directory is inside the repository")
)

// Status is how a repository's policy stands against its approval.
type Status int

// Statuses.
const (
	// Unapproved: no record exists for the repository.
	Unapproved Status = iota
	// Approved: the policy's digest is one of the recorded ones.
	Approved
	// Changed: a record exists, and the policy's digest is not in it.
	Changed
	// Missing: there is no policy file.
	Missing
)

func (s Status) String() string {
	switch s {
	case Approved:
		return "approved"
	case Changed:
		return "changed"
	case Missing:
		return "missing"
	}
	return "unapproved"
}

// record is the file format.
type record struct {
	Version int `toml:"version"`
	// Digest is a legacyVersion record's one digest.
	Digest string `toml:"digest,omitempty"`
	// Digests are a Version record's digests, oldest first.
	Digests  []string  `toml:"digests,omitempty"`
	Approved time.Time `toml:"approved"`
}

// Store is a configuration directory's approval records.
type Store struct {
	// Dir is the configuration directory; records are in Dir/approved/.
	Dir string
}

// ConfigDir is $PBIPX_CONFIG_DIR when it is set and not empty, else
// <user config dir>/pbipx. A relative PBIPX_CONFIG_DIR is ErrNoConfigDir:
// resolved against the working directory, it could point into the
// repository the approval guards.
func ConfigDir() (string, error) {
	if d := os.Getenv(EnvConfigDir); d != "" {
		if !filepath.IsAbs(d) {
			return "", ErrNoConfigDir
		}
		return filepath.Clean(d), nil
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return "", ErrNoConfigDir
	}
	return filepath.Join(base, "pbipx"), nil
}

// Open is the store in ConfigDir.
func Open() (*Store, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

// OpenFor is the store in ConfigDir for the repository at root, refused
// with ErrInRepository when the directory is inside that repository.
func OpenFor(root string) (*Store, error) {
	s, err := Open()
	if err != nil {
		return nil, err
	}
	if s.InRepository(root) {
		return nil, ErrInRepository
	}
	return s, nil
}

// InRepository reports whether the store's directory is the repository
// root at root or below it, under any spelling: as given, and with the
// symlinks of root and of the directory's deepest existing ancestor
// resolved. A record there could be written by anything that can write to
// the repository, so it approves nothing.
func (s *Store) InRepository(root string) bool {
	roots := spellings(root)
	for _, d := range spellings(s.Dir) {
		for _, r := range roots {
			if Within(r, d) {
				return true
			}
		}
	}
	return false
}

// spellings are p made absolute and clean, and that with the symlinks of
// its deepest existing ancestor resolved.
func spellings(p string) []string {
	a, err := filepath.Abs(p)
	if err != nil {
		return []string{filepath.Clean(p)}
	}
	return []string{a, ResolveExisting(a)}
}

// ResolveExisting is the absolute path a with the symlinks of its deepest
// existing ancestor resolved, and the rest joined on unchanged: a itself
// resolved when it exists, and for a path not yet there, where it would be
// created. When no ancestor resolves, it is a, clean.
func ResolveExisting(a string) string {
	a = filepath.Clean(a)
	rest := ""
	for dir := a; ; dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		if filepath.Dir(dir) == dir {
			return a
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// Within reports whether path is dir or below it, both absolute and clean.
// On Windows and macOS, whose file systems are usually case-insensitive,
// case is ignored.
func Within(dir, path string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		dir, path = strings.ToLower(dir), strings.ToLower(path)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Digest is the digest of a policy's exact bytes, as "sha256:<hex>".
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return digestPrefix + hex.EncodeToString(sum[:])
}

// DigestFile is the digest of the file at path. A missing file is an error
// that wraps fs.ErrNotExist.
func DigestFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the policy path is the repository's own
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

// Repository is what an approval is recorded for (ADR-0006, amended
// 2026-10-29).
type Repository struct {
	// GitDir is the clone's shared git directory (git rev-parse
	// --git-common-dir, source.Repo.CommonDir): every work tree of one
	// clone, linked ones included, has the same one, so all of them share
	// one record.
	GitDir string
	// Root is the work tree. A record written before the key was the git
	// directory is keyed by it, and is still read until an approval merges
	// it into the shared record.
	Root string
}

// InClone reports whether the store's directory is in r's work tree, in
// its git directory, or in the main work tree that git directory belongs
// to (its parent, when it is named .git), under any spelling
// (InRepository). A linked work tree lies inside another work tree of its
// clone, as Claude Code's .claude/worktrees do, and anything that can write
// to one can write there.
func (s *Store) InClone(r Repository) bool {
	dirs := []string{r.Root, r.GitDir}
	if strings.EqualFold(filepath.Base(r.GitDir), ".git") {
		dirs = append(dirs, filepath.Dir(r.GitDir))
	}
	for _, d := range dirs {
		if d != "" && s.InRepository(d) {
			return true
		}
	}
	return false
}

// Key is a record's file name for a directory: the hex SHA-256 of its
// absolute, cleaned, symlink-resolved path. The directory must exist.
func Key(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(filepath.Clean(resolved)))
	return hex.EncodeToString(sum[:]), nil
}

// recordPath is the path of the record keyed by dir.
func (s *Store) recordPath(dir string) (string, error) {
	key, err := Key(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.Dir, approvedDir, key), nil
}

// paths are the records r's approvals are read from: one keyed by its work
// tree (legacyVersion), when that differs, then the shared one.
func (s *Store) paths(r Repository) (legacy, shared string, err error) {
	if shared, err = s.recordPath(r.GitDir); err != nil {
		return "", "", err
	}
	if r.Root != "" {
		if legacy, err = s.recordPath(r.Root); err != nil {
			return "", "", err
		}
		if legacy == shared {
			legacy = ""
		}
	}
	return legacy, shared, nil
}

// Approve adds digest to the approved digests of r, approved at now, and
// keeps the most recent MaxDigests: approving one already there makes it
// the most recent. A record keyed by the work tree is merged in and then
// removed. The record is written through a temporary file in the same
// directory and renamed into place, so a reader sees the old record or the
// new one, never part of one. Directories are created 0700 and the record
// 0600. A record that does not parse is replaced: approving is a person's
// act at a terminal.
func (s *Store) Approve(r Repository, digest string, now time.Time) error {
	if !digestShape.MatchString(digest) {
		return errors.New("approval: the digest is not a sha256 digest")
	}
	legacy, shared, err := s.paths(r)
	if err != nil {
		return err
	}
	var digests []string
	for _, p := range []string{legacy, shared} {
		if p == "" {
			continue
		}
		if d, ok, err := readRecord(p); err == nil && ok {
			digests = append(digests, d...)
		}
	}
	digests = dedupe(append(digests, digest))
	if len(digests) > MaxDigests {
		digests = digests[len(digests)-MaxDigests:]
	}
	data, err := toml.Marshal(record{Version: Version, Digests: digests, Approved: now.UTC().Truncate(time.Second)})
	if err != nil {
		return err
	}
	if err := writeAtomic(shared, data); err != nil {
		return err
	}
	if legacy != "" {
		_ = os.Remove(legacy)
	}
	return nil
}

// dedupe is digests without repeats, each where it last appears.
func dedupe(digests []string) []string {
	var out []string
	for i, d := range digests {
		if !slices.Contains(digests[i+1:], d) {
			out = append(out, d)
		}
	}
	return out
}

// writeAtomic writes data to path through a temporary file renamed into
// place, creating the directory 0700 and the file 0600.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".approval-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	werr := tmp.Chmod(0o600)
	if werr == nil {
		_, werr = tmp.Write(data)
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	cerr := tmp.Close()
	if werr == nil && cerr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		return errors.Join(werr, cerr)
	}
	return nil
}

// Recorded is the digests approved for r, oldest first: those in its
// shared record and in a record keyed by its work tree. ok is false when
// there is neither. A record that does not parse, or is of an unknown
// version, is ErrBadRecord.
func (s *Store) Recorded(r Repository) (digests []string, ok bool, err error) {
	legacy, shared, err := s.paths(r)
	if err != nil {
		return nil, false, err
	}
	for _, p := range []string{legacy, shared} {
		if p == "" {
			continue
		}
		d, found, err := readRecord(p)
		if err != nil {
			return nil, false, err
		}
		if found {
			ok = true
			digests = append(digests, d...)
		}
	}
	return dedupe(digests), ok, nil
}

// readRecord reads one record's digests; found is false when there is no
// record at path.
func readRecord(path string) (digests []string, found bool, err error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is a hash under the store's own directory
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var r record
	dec := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if dec.Decode(&r) != nil {
		return nil, false, ErrBadRecord
	}
	switch r.Version {
	case legacyVersion:
		if r.Digests != nil || !digestShape.MatchString(r.Digest) {
			return nil, false, ErrBadRecord
		}
		return []string{r.Digest}, true, nil
	case Version:
		if r.Digest != "" || len(r.Digests) == 0 || len(r.Digests) > MaxDigests {
			return nil, false, ErrBadRecord
		}
		for _, d := range r.Digests {
			if !digestShape.MatchString(d) {
				return nil, false, ErrBadRecord
			}
		}
		return r.Digests, true, nil
	}
	return nil, false, ErrBadRecord
}

// Status checks the policy file at policyPath against the approvals of r.
func (s *Store) Status(r Repository, policyPath string) (Status, error) {
	digest, err := DigestFile(policyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return Unapproved, err
	}
	return s.Check(r, digest)
}

// Check checks a policy's digest against the approvals of r: Approved when
// it is one of the approved digests. It is never Missing.
func (s *Store) Check(r Repository, digest string) (Status, error) {
	digests, ok, err := s.Recorded(r)
	switch {
	case err != nil:
		return Unapproved, err
	case !ok:
		return Unapproved, nil
	case !slices.Contains(digests, digest):
		return Changed, nil
	}
	return Approved, nil
}
