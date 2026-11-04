// Package baseline reads and writes .pbipx-baseline.toml (ADR-0004): reviewed
// warnings accepted until a date (docs/design/behavior.md, "Accepting a
// warning"; docs/design/corpus.md, "Baseline cases").
//
// An entry is keyed by a finding's fingerprint, which hashes location, column
// and reasons and never the value (principle 3). The other fields are for the
// reviewer. A block is never accepted, and an entry whose severity is not
// warn makes the whole file a baseline error.
//
// Errors carry the file, an entry's fingerprint (only when it has the shape
// of one), a key and a line, never text from the file.
package baseline

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/shape"
)

// FileName is the baseline's name at the repository root.
const FileName = ".pbipx-baseline.toml"

// Version is the only baseline version this package reads.
const Version = 1

// DefaultExpiryDays is how long a recorded acceptance lasts by default.
const DefaultExpiryDays = 180

// dateLayout is an expiry date: YYYY-MM-DD.
const dateLayout = "2006-01-02"

// severityWarn is the only severity an entry may have.
const severityWarn = "warn"

// Entry is one accepted warning.
type Entry struct {
	Severity string   `toml:"severity"`
	Expires  string   `toml:"expires"` // YYYY-MM-DD; unparseable counts as expired
	Path     string   `toml:"path"`
	Context  string   `toml:"context"`
	Column   *string  `toml:"column,omitempty"`
	Reasons  []string `toml:"reasons"`
	// Note is the reviewer's reason for the acceptance, required by Record.
	// It is written by a person, not read from a scanned file.
	Note string `toml:"note,omitempty"`
}

// Baseline is a loaded baseline, keyed by fingerprint. The zero Baseline is
// empty and valid.
type Baseline struct {
	Entries map[string]Entry
}

type file struct {
	Version int              `toml:"version"`
	Entries map[string]Entry `toml:"entries"`
}

// Error is a baseline error. It never holds text from the file beyond a
// well-formed fingerprint.
type Error struct {
	File  string // the path given to Load
	Entry string // the entry's fingerprint, or "" when not about one entry
	Key   string // a key such as "severity"; "" when not about one key
	Line  int    // 1-based; 0 when unknown
	Msg   string // what is wrong, in structural terms

	err error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("baseline ")
	b.WriteString(e.File)
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	b.WriteString(": ")
	if e.Entry != "" {
		fmt.Fprintf(&b, "entry %s ", e.Entry)
	}
	if e.Key != "" {
		b.WriteString(e.Key)
		b.WriteString(" ")
	}
	b.WriteString(e.Msg)
	return b.String()
}

// Unwrap returns the I/O error that kept the file from being read, if any.
func (e *Error) Unwrap() error { return e.err }

// fingerprintShape is what a fingerprint looks like. A key without it is
// not printed, since it is free text.
var fingerprintShape = regexp.MustCompile(`^fp[0-9]+:[0-9a-f]+$`)

// PrintableKey is an entry's key as it may be printed: the key when it has
// the shape of a fingerprint and no personal-data shape, and a placeholder
// otherwise.
func PrintableKey(key string) string {
	if isFingerprint(key) {
		return key
	}
	return `"…"`
}

// Load reads the baseline at path. A missing file is an empty baseline.
// Every other failure is an *Error.
func Load(path string) (*Baseline, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the baseline path is the caller's to choose
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Baseline{Entries: map[string]Entry{}}, nil
		}
		return nil, &Error{File: path, Msg: "cannot be read", err: err}
	}
	b, perr := parse(data)
	if perr != nil {
		perr.File = path
		return nil, perr
	}
	return b, nil
}

func parse(data []byte) (*Baseline, *Error) {
	var f file
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, decodeError(err)
	}
	if f.Version != Version {
		return nil, &Error{Key: "version", Msg: fmt.Sprintf("must be %d", Version)}
	}
	if f.Entries == nil {
		f.Entries = map[string]Entry{}
	}
	for _, fp := range slices.Sorted(maps.Keys(f.Entries)) {
		if f.Entries[fp].Severity != severityWarn {
			return nil, &Error{Entry: PrintableKey(fp), Key: "severity", Msg: "is not warn; a block is never baselined"}
		}
	}
	return &Baseline{Entries: f.Entries}, nil
}

// decodeError keeps only a decoder error's position and key: its message can
// quote the document.
func decodeError(err error) *Error {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) && len(strict.Errors) > 0 {
		first := &strict.Errors[0]
		line, _ := first.Position()
		return &Error{Line: line, Msg: "has a key that is not known"} // the key is text from the file: not echoed
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		line, _ := de.Position()
		if key := de.Key(); len(key) > 0 {
			return &Error{Key: keyName(key), Line: line, Msg: "has the wrong type, or is not valid TOML"}
		}
		return &Error{Line: line, Msg: "is not valid TOML"}
	}
	return &Error{Msg: "is not valid TOML, or a key has the wrong type"}
}

func isFingerprint(key string) bool {
	return fingerprintShape.MatchString(key) && len(shape.Literal(key, shape.Switches{})) == 0
}

// keyName joins a key's parts. A part that is neither a fingerprint nor a
// key shape.PrintableKey allows is free text, and is replaced.
func keyName(key toml.Key) string {
	parts := make([]string, len(key))
	for i, k := range key {
		switch {
		case shape.PrintableKey(k), isFingerprint(k):
			parts[i] = k
		default:
			parts[i] = `"…"`
		}
	}
	return strings.Join(parts, ".")
}

// today is now's calendar date, in now's location.
func today(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Expired reports whether the entry's expiry date is before now's date. An
// expiry that does not parse counts as expired.
func (e Entry) Expired(now time.Time) bool {
	exp, err := time.Parse(dateLayout, e.Expires)
	if err != nil {
		return true
	}
	return exp.Before(today(now))
}

// Accepts reports whether the baseline accepts f at now: a warning whose
// fingerprint is listed and has not expired. A block or an error is never
// accepted.
func (b *Baseline) Accepts(f finding.Finding, now time.Time) bool {
	if b == nil || f.Tier != finding.Warn {
		return false
	}
	e, ok := b.Entries[f.Fingerprint()]
	return ok && e.Severity == severityWarn && !e.Expired(now)
}

// Expired returns the fingerprints of the entries that have expired at now,
// sorted.
func (b *Baseline) Expired(now time.Time) []string {
	var out []string
	for fp, e := range b.Entries {
		if e.Expired(now) {
			out = append(out, fp)
		}
	}
	slices.Sort(out)
	return out
}

// Refusal is why Record will not record an acceptance.
type Refusal int

// Refusals.
const (
	// NotAWarning is a fingerprint whose finding is a block or an error.
	NotAWarning Refusal = iota + 1
	// UnknownFinding is a fingerprint with no current finding.
	UnknownFinding
	// NoNote is an acceptance without a reviewer's note.
	NoNote
	// BadExpiry is an expiry of zero days or fewer.
	BadExpiry
)

func (r Refusal) Error() string {
	switch r {
	case NotAWarning:
		return "the finding is not a warning; a block is never baselined"
	case UnknownFinding:
		return "no current finding has that fingerprint"
	case NoNote:
		return "an acceptance needs a note"
	case BadExpiry:
		return "the expiry must be at least one day"
	}
	return "refused"
}

// Record adds an acceptance of the finding with fingerprint fp, one of
// findings, expiring days after now, with note. It refuses (with a Refusal)
// a fingerprint that no finding has, a finding that is not a warning, an
// empty note and a non-positive expiry. Recording a fingerprint already
// listed replaces its entry.
func (b *Baseline) Record(fp string, findings []finding.Finding, note string, days int, now time.Time) (Entry, error) {
	if strings.TrimSpace(note) == "" {
		return Entry{}, NoNote
	}
	if days <= 0 {
		return Entry{}, BadExpiry
	}
	idx := slices.IndexFunc(findings, func(f finding.Finding) bool { return f.Fingerprint() == fp })
	if idx < 0 {
		return Entry{}, UnknownFinding
	}
	f := findings[idx]
	if f.Tier != finding.Warn {
		return Entry{}, NotAWarning
	}
	reasons := make([]string, len(f.Reasons))
	for i, r := range f.Reasons {
		reasons[i] = string(r)
	}
	e := Entry{
		Severity: severityWarn,
		Expires:  today(now).AddDate(0, 0, days).Format(dateLayout),
		Path:     f.Path,
		Context:  string(f.Context),
		Reasons:  reasons,
		Note:     note,
	}
	if f.Column != nil {
		c := f.Column.String()
		e.Column = &c
	}
	if b.Entries == nil {
		b.Entries = map[string]Entry{}
	}
	b.Entries[fp] = e
	return e, nil
}

// Encode is the baseline as TOML, entries sorted by fingerprint.
func (b *Baseline) Encode() ([]byte, error) {
	entries := b.Entries
	if entries == nil {
		entries = map[string]Entry{}
	}
	return toml.Marshal(file{Version: Version, Entries: entries})
}

// Save writes the baseline to path, through a temporary file in the same
// directory, so a failed write leaves the old baseline whole.
func (b *Baseline) Save(path string) error {
	data, err := b.Encode()
	if err != nil {
		return &Error{File: path, Msg: "cannot be encoded"}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pbipx-baseline-*.toml")
	if err != nil {
		return &Error{File: path, Msg: "cannot be written", err: err}
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil && cerr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		return &Error{File: path, Msg: "cannot be written", err: errors.Join(werr, cerr)}
	}
	return nil
}
