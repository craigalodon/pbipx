// Package approval records which policy a person approved for a repository
// on this machine, and checks the policy against it (ADR-0006; threat model,
// layer 6).
//
// An approval is the SHA-256 digest of the policy file's exact bytes, kept
// outside the repository in
// ${PBIPX_CONFIG_DIR:-<user config dir>/pbipx}/approved/<key>, where the key
// is a hash of the repository root's absolute, cleaned, symlink-resolved
// path. A record holds a format version, the digest and the approval time:
// never policy content, never the path in clear.
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
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// EnvConfigDir names the configuration directory, overriding the user
// configuration directory's pbipx/. It must be absolute; empty is unset.
const EnvConfigDir = "PBIPX_CONFIG_DIR"

// Version is the only record format version this package reads and writes.
const Version = 1

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
	// Approved: the policy's digest is the recorded one.
	Approved
	// Changed: a record exists, and the policy's digest differs from it.
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
	Version  int       `toml:"version"`
	Digest   string    `toml:"digest"`
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
	out := []string{a}
	rest := ""
	for dir := a; ; dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			out = append(out, filepath.Join(r, rest))
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
	return out
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

// Key is the record's file name for a repository root: the hex SHA-256 of
// its absolute, cleaned, symlink-resolved path. The root must exist.
func Key(root string) (string, error) {
	abs, err := filepath.Abs(root)
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

// path is the record's path for a repository root.
func (s *Store) path(root string) (string, error) {
	key, err := Key(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.Dir, approvedDir, key), nil
}

// Approve records digest as the approved policy of the repository at root,
// approved at now. The record is written through a temporary file in the
// same directory and renamed into place, so a reader sees the old record or
// the new one, never part of one. Directories are created 0700 and the
// record 0600.
func (s *Store) Approve(root, digest string, now time.Time) error {
	if !digestShape.MatchString(digest) {
		return errors.New("approval: the digest is not a sha256 digest")
	}
	path, err := s.path(root)
	if err != nil {
		return err
	}
	data, err := toml.Marshal(record{Version: Version, Digest: digest, Approved: now.UTC().Truncate(time.Second)})
	if err != nil {
		return err
	}
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

// Recorded is the digest recorded for the repository at root, and false
// when there is no record. A record that does not parse, or is of another
// version, is ErrBadRecord.
func (s *Store) Recorded(root string) (string, bool, error) {
	path, err := s.path(root)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is a hash under the store's own directory
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var r record
	dec := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if dec.Decode(&r) != nil || r.Version != Version || !digestShape.MatchString(r.Digest) {
		return "", false, ErrBadRecord
	}
	return r.Digest, true, nil
}

// Status checks the policy file at policyPath against the approval of the
// repository at root.
func (s *Store) Status(root, policyPath string) (Status, error) {
	digest, err := DigestFile(policyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return Unapproved, err
	}
	return s.Check(root, digest)
}

// Check checks a policy's digest against the approval of the repository at
// root. It is never Missing.
func (s *Store) Check(root, digest string) (Status, error) {
	recorded, ok, err := s.Recorded(root)
	switch {
	case err != nil:
		return Unapproved, err
	case !ok:
		return Unapproved, nil
	case recorded != digest:
		return Changed, nil
	}
	return Approved, nil
}
