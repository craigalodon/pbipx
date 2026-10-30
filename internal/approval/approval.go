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
// most recent last), the work trees of the clone it was written from (at
// most MaxRoots, the most recent last) and the time of the last approval:
// never policy content.
//
// A record also marks its clone as adopted (ADR-0006, amended 2026-10-30):
// `pbipx init` writes one with no digest, and the gate acts only for an
// adopted repository. The work trees of every record together are the
// index the gate finds adopted repositories by (Store.Roots).
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
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// EnvConfigDir names the configuration directory, overriding the user
// configuration directory's pbipx/. It must be absolute; empty is unset.
const EnvConfigDir = "PBIPX_CONFIG_DIR"

// Version is the record format version this package writes. It also reads
// setVersion and legacyVersion.
const Version = 3

// setVersion is the format of a record that holds a set of digests and no
// work trees.
const setVersion = 2

// legacyVersion is the format of a record that holds one digest, keyed by
// the work tree's path.
const legacyVersion = 1

// MaxDigests is how many approved digests a record keeps.
const MaxDigests = 16

// MaxRoots is how many work trees a record keeps.
const MaxRoots = 64

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
	// ErrStoreNotDir is a store whose records directory, or a directory
	// above it, is not a directory.
	ErrStoreNotDir = errors.New("approval: the approval records directory is not a directory")
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
	// Digests are a setVersion or Version record's digests, oldest first.
	// A Version record may have none: its clone is adopted, and no policy
	// is approved yet.
	Digests []string `toml:"digests,omitempty"`
	// Roots are a Version record's work trees, absolute and clean, oldest
	// first.
	Roots []string `toml:"roots,omitempty"`
	// Approved is the time of the last approval; zero when there is none.
	Approved time.Time `toml:"approved,omitempty"`
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
// the most recent. It records r's work tree as Adopt does. A record keyed
// by the work tree is merged in and then removed. The record is written
// through a temporary file in the same directory and renamed into place, so
// a reader sees the old record or the new one, never part of one.
// Directories are created 0700 and the record 0600. A record that does not
// parse is replaced: approving is a person's act at a terminal.
func (s *Store) Approve(r Repository, digest string, now time.Time) error {
	if !digestShape.MatchString(digest) {
		return errors.New("approval: the digest is not a sha256 digest")
	}
	at := now.UTC().Truncate(time.Second)
	return s.update(r, true, func(rec *record) {
		rec.Digests = dedupe(append(rec.Digests, digest))
		if len(rec.Digests) > MaxDigests {
			rec.Digests = rec.Digests[len(rec.Digests)-MaxDigests:]
		}
		rec.Approved = at
	})
}

// Adopt records r's clone as adopted, with r's work tree in the index
// (Roots), keeping every digest already approved for it: `pbipx init` runs
// it, so the gate acts for the repository before its policy is approved.
// It writes as Approve does. A record that does not parse is left as it
// is, and is ErrBadRecord: the gate already treats the clone as adopted.
func (s *Store) Adopt(r Repository) error {
	return s.update(r, false, func(*record) {})
}

// update reads r's records (its shared one, and one keyed by its work
// tree), applies change to them merged, adds r's work tree to the roots,
// and writes the result as r's shared record, removing the one keyed by
// the work tree. With replaceBad, a record that does not parse counts as
// none; without it, it is ErrBadRecord and nothing is written.
func (s *Store) update(r Repository, replaceBad bool, change func(*record)) error {
	legacy, shared, err := s.paths(r)
	if err != nil {
		return err
	}
	merged := record{Version: Version}
	for _, p := range []string{legacy, shared} {
		if p == "" {
			continue
		}
		rec, ok, err := readRecord(p)
		if err != nil && !replaceBad {
			return err
		}
		if err != nil || !ok {
			continue
		}
		merged.Digests = append(merged.Digests, rec.Digests...)
		merged.Roots = append(merged.Roots, rec.Roots...)
		if rec.Approved.After(merged.Approved) {
			merged.Approved = rec.Approved
		}
	}
	merged.Digests = dedupe(merged.Digests)
	change(&merged)
	if root, ok := cleanRoot(r.Root); ok {
		merged.Roots = dedupe(append(merged.Roots, root))
	}
	merged.Roots = dedupe(merged.Roots)
	if len(merged.Roots) > MaxRoots {
		merged.Roots = merged.Roots[len(merged.Roots)-MaxRoots:]
	}
	data, err := toml.Marshal(merged)
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

// cleanRoot is a work tree as the index keeps it: absolute and clean. ok
// is false for an empty or relative one, which is not kept.
func cleanRoot(root string) (string, bool) {
	if root == "" || !filepath.IsAbs(root) {
		return "", false
	}
	return filepath.Clean(root), true
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
// there are none: no record, or only a record that adopts the clone. A
// record that does not parse, or is of an unknown version, is ErrBadRecord.
func (s *Store) Recorded(r Repository) (digests []string, ok bool, err error) {
	legacy, shared, err := s.paths(r)
	if err != nil {
		return nil, false, err
	}
	for _, p := range []string{legacy, shared} {
		if p == "" {
			continue
		}
		rec, found, err := readRecord(p)
		if err != nil {
			return nil, false, err
		}
		if found {
			digests = append(digests, rec.Digests...)
		}
	}
	digests = dedupe(digests)
	return digests, len(digests) > 0, nil
}

// Adopted reports whether r's clone has a record: one with an approval, or
// one Adopt wrote. A record that does not parse counts, with ErrBadRecord,
// so a caller can fail closed on it.
func (s *Store) Adopted(r Repository) (bool, error) {
	legacy, shared, err := s.paths(r)
	if err != nil {
		return false, err
	}
	for _, p := range []string{legacy, shared} {
		if p == "" {
			continue
		}
		_, found, err := readRecord(p)
		if errors.Is(err, ErrBadRecord) {
			return true, err
		}
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// Roots is the index of adopted work trees: the work trees every record in
// the store was written from, in no set order, and whether the store holds
// any record at all. A record that does not parse adds no work tree, but
// counts as a record. A store with no records directory has none; one
// whose records directory, or a directory above it, is not a directory is
// ErrStoreNotDir, checked first since Windows may read a file as an empty
// or missing directory.
func (s *Store) Roots() (roots []string, anyRecord bool, err error) {
	dir := filepath.Join(s.Dir, approvedDir)
	if notDirAt(dir) {
		return nil, false, ErrStoreNotDir
	}
	entries, err := readDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	for _, e := range entries {
		if !recordName.MatchString(e.Name()) {
			continue
		}
		rec, found, err := readRecord(filepath.Join(dir, e.Name()))
		switch {
		case errors.Is(err, ErrBadRecord):
			anyRecord = true
		case err != nil:
			return nil, false, err
		case found:
			anyRecord = true
			roots = append(roots, rec.Roots...)
		}
	}
	return dedupe(roots), anyRecord, nil
}

// readFile and readDir read the store; a test replaces them to read as
// Windows does, where a path through a file is missing.
var (
	readFile = os.ReadFile
	readDir  = os.ReadDir
)

// missing reports whether err is a path that is not there: missing, or
// through a file (ENOTDIR, which Windows reports as missing).
func missing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// notDirAt reports whether the nearest of path and the directories above
// it that exists is not a directory: path is then under a file, and the
// store is broken rather than empty.
func notDirAt(path string) bool {
	for p := path; ; {
		info, err := os.Stat(p)
		if err == nil {
			return !info.IsDir()
		}
		if !missing(err) {
			return false
		}
		up := filepath.Dir(p)
		if up == p {
			return false
		}
		p = up
	}
}

// recordName is a record's file name: a hex SHA-256 (Key).
var recordName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// readRecord reads one record, validated, with a legacyVersion record's
// digest moved into Digests; found is false when there is no record at
// path. A record that does not parse, or is of an unknown version, is
// ErrBadRecord.
//
// A records directory that is not a directory, or one under a file (a
// configuration directory below a file), is ErrStoreNotDir, not an empty
// store: Windows reports a path through a file as missing, where Unix
// reports ENOTDIR.
func readRecord(path string) (rec record, found bool, err error) {
	data, err := readFile(path)
	if missing(err) {
		if notDirAt(path) {
			return record{}, false, ErrStoreNotDir
		}
		if errors.Is(err, fs.ErrNotExist) {
			return record{}, false, nil
		}
	}
	if err != nil {
		return record{}, false, err
	}
	bad := func() (record, bool, error) { return record{}, false, ErrBadRecord }
	dec := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if dec.Decode(&rec) != nil {
		return bad()
	}
	switch rec.Version {
	case legacyVersion:
		if rec.Digests != nil || rec.Roots != nil || !digestShape.MatchString(rec.Digest) {
			return bad()
		}
		rec.Digests, rec.Digest = []string{rec.Digest}, ""
	case setVersion:
		if rec.Digest != "" || rec.Roots != nil || len(rec.Digests) == 0 {
			return bad()
		}
	case Version:
		if rec.Digest != "" || len(rec.Digests)+len(rec.Roots) == 0 || len(rec.Roots) > MaxRoots {
			return bad()
		}
		for _, root := range rec.Roots {
			if c, ok := cleanRoot(root); !ok || c != root {
				return bad()
			}
		}
	default:
		return bad()
	}
	if len(rec.Digests) > MaxDigests {
		return bad()
	}
	for _, d := range rec.Digests {
		if !digestShape.MatchString(d) {
			return bad()
		}
	}
	return rec, true, nil
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
