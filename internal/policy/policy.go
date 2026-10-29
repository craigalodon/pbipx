// Package policy loads and applies a repository's .pbipx.toml (ADR-0004):
// protected roots, scan scope per file kind, local state and allowlisted
// unscanned files, column lists, shape switches, allowlisted values and
// lasting acceptances.
//
// A policy that fails to load is an *Error, which the CLI maps to the
// policy-error failure. Errors carry key names, indexes and line numbers,
// never a value from the file: the allow list holds literals, and accept
// notes are free text.
package policy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// FileName is the policy's name at the repository root.
const FileName = ".pbipx.toml"

// Version is the only policy version this package reads.
const Version = 1

// Kind is a scanned file's kind, which picks its scanner.
type Kind string

// Kinds.
const (
	Report Kind = "report"
	Model  Kind = "model"
	SQL    Kind = "sql"
	YAML   Kind = "yaml"
	Prose  Kind = "prose"
)

// Kinds lists every kind, in the order Classify tries them.
var Kinds = []Kind{Report, Model, SQL, YAML, Prose}

// Shapes are the shape switches; each is on unless the policy turns it off.
type Shapes struct {
	PersonName bool
	Email      bool
	SSN        bool
	Date       bool
	Phone      bool
}

// Policy is a loaded, validated policy. The zero Policy is not valid; use
// Load.
type Policy struct {
	roots          []string
	scan           map[Kind]globs
	exclude        globs
	localState     globs
	unscannedAllow globs
	block          map[string]bool
	blockPattern   *regexp.Regexp // nil when absent
	warn           map[string]bool
	shapes         Shapes
	allow          map[string]bool
	accepts        []accept
}

// accept is one [[accept]] entry. Its note and reviewer are checked at load
// and not kept: nothing decides on them, and nothing should print them.
type accept struct {
	column string // table.column, lowercased
	path   string // a path prefix, at least three segments
}

// Error is a policy error. It names the policy file, the key it is about
// and, for TOML syntax, a line. It never holds a value from the file.
type Error struct {
	File string // the path given to Load
	Key  string // a key path such as "scan.report[1]"; empty for the whole file
	Line int    // 1-based; 0 when not known
	Msg  string // what is wrong, in structural terms

	err error // an underlying I/O error, for errors.Is
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("policy ")
	b.WriteString(e.File)
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	b.WriteString(": ")
	if e.Key != "" {
		b.WriteString(e.Key)
		b.WriteString(" ")
	}
	b.WriteString(e.Msg)
	return b.String()
}

// Unwrap returns the I/O error that kept the file from being read, if any.
func (e *Error) Unwrap() error { return e.err }

// The file's schema. Unknown keys at any level are rejected by the decoder.
type rawPolicy struct {
	Version        int         `toml:"version"`
	ProtectedRoots []string    `toml:"protected_roots"`
	Scan           rawScan     `toml:"scan"`
	Files          rawFiles    `toml:"files"`
	Columns        rawColumns  `toml:"columns"`
	Shapes         rawShapes   `toml:"shapes"`
	Values         rawValues   `toml:"values"`
	Accept         []rawAccept `toml:"accept"`
}

type rawScan struct {
	Report  []string `toml:"report"`
	Model   []string `toml:"model"`
	SQL     []string `toml:"sql"`
	YAML    []string `toml:"yaml"`
	Prose   []string `toml:"prose"`
	Exclude []string `toml:"exclude"`
}

type rawFiles struct {
	LocalState     []string `toml:"local_state"`
	UnscannedAllow []string `toml:"unscanned_allow"`
}

type rawColumns struct {
	Block        []string `toml:"block"`
	BlockPattern *string  `toml:"block_pattern"`
	Warn         []string `toml:"warn"`
}

type rawShapes struct {
	PersonName *bool `toml:"person_name"`
	Email      *bool `toml:"email"`
	SSN        *bool `toml:"ssn"`
	Date       *bool `toml:"date"`
	Phone      *bool `toml:"phone"`
}

type rawValues struct {
	Allow []string `toml:"allow"`
}

type rawAccept struct {
	Column     string `toml:"column"`
	Path       string `toml:"path"`
	Note       string `toml:"note"`
	ReviewedBy string `toml:"reviewed_by"`
}

// Load reads and validates the policy at path. Every failure is an *Error.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the policy path is the caller's to choose
	if err != nil {
		msg := "cannot be read"
		if errors.Is(err, fs.ErrNotExist) {
			msg = "does not exist"
		}
		return nil, &Error{File: path, Msg: msg, err: err}
	}
	p, perr := parse(data)
	if perr != nil {
		perr.File = path
		return nil, perr
	}
	return p, nil
}

// parse decodes and validates a policy. The returned error has no File.
func parse(data []byte) (*Policy, *Error) {
	var raw rawPolicy
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, decodeError(err)
	}
	return validate(&raw)
}

// decodeError turns a decoder error into a policy error. The decoder's own
// messages and its human-readable form can quote the document, so only the
// position and the key are kept.
func decodeError(err error) *Error {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) && len(strict.Errors) > 0 {
		first := &strict.Errors[0]
		line, _ := first.Position()
		return &Error{Key: keyName(first.Key()), Line: line, Msg: "is not a known key"}
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

var plainKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// keyName joins a key's parts. A part that is not a bare TOML key is free
// text, which may be a value written in the wrong place, so it is replaced.
func keyName(key toml.Key) string {
	parts := make([]string, len(key))
	for i, k := range key {
		if plainKey.MatchString(k) {
			parts[i] = k
		} else {
			parts[i] = `"…"`
		}
	}
	return strings.Join(parts, ".")
}

func validate(raw *rawPolicy) (*Policy, *Error) {
	if raw.Version != Version {
		return nil, &Error{Key: "version", Msg: fmt.Sprintf("must be %d", Version)}
	}
	p := &Policy{scan: map[Kind]globs{}}

	if len(raw.ProtectedRoots) == 0 {
		return nil, &Error{Key: "protected_roots", Msg: "is missing or empty"}
	}
	for i, r := range raw.ProtectedRoots {
		if problem := relPathProblem(r); problem != "" {
			return nil, &Error{Key: indexed("protected_roots", i), Msg: problem}
		}
		if strings.ContainsAny(r, "*?[") {
			return nil, &Error{Key: indexed("protected_roots", i), Msg: "is a glob, not a plain path"}
		}
	}
	p.roots = raw.ProtectedRoots

	kinds := map[Kind][]string{
		Report: raw.Scan.Report, Model: raw.Scan.Model, SQL: raw.Scan.SQL,
		YAML: raw.Scan.YAML, Prose: raw.Scan.Prose,
	}
	declared := false
	for _, k := range Kinds {
		gs, err := compileGlobs("scan."+string(k), kinds[k])
		if err != nil {
			return nil, err
		}
		p.scan[k] = gs
		declared = declared || len(gs) > 0
	}
	if !declared {
		return nil, &Error{Key: "scan", Msg: "declares no file kind"}
	}
	var err *Error
	if p.exclude, err = compileGlobs("scan.exclude", raw.Scan.Exclude); err != nil {
		return nil, err
	}
	if p.localState, err = compileGlobs("files.local_state", raw.Files.LocalState); err != nil {
		return nil, err
	}
	if p.unscannedAllow, err = compileGlobs("files.unscanned_allow", raw.Files.UnscannedAllow); err != nil {
		return nil, err
	}
	for i, r := range p.roots {
		if !p.covers(r) {
			return nil, &Error{Key: indexed("protected_roots", i), Msg: "is covered by no scan kind"}
		}
	}

	p.block = lowerSet(raw.Columns.Block)
	p.warn = lowerSet(raw.Columns.Warn)
	if bp := raw.Columns.BlockPattern; bp != nil {
		if *bp == "" {
			return nil, &Error{Key: "columns.block_pattern", Msg: "is empty, which would match every column"}
		}
		// The name is lowercased; (?i) keeps an uppercase letter in the
		// pattern from silently never matching, as the predecessor's
		// case-folded pattern did.
		re, cerr := regexp.Compile("(?i)" + *bp)
		if cerr != nil {
			return nil, &Error{Key: "columns.block_pattern", Msg: "does not compile"}
		}
		p.blockPattern = re
	}

	p.shapes = Shapes{
		PersonName: on(raw.Shapes.PersonName),
		Email:      on(raw.Shapes.Email),
		SSN:        on(raw.Shapes.SSN),
		Date:       on(raw.Shapes.Date),
		Phone:      on(raw.Shapes.Phone),
	}

	p.allow = make(map[string]bool, len(raw.Values.Allow))
	for _, v := range raw.Values.Allow {
		p.allow[v] = true
	}

	for i, a := range raw.Accept {
		entry, aerr := p.validateAccept(i, a)
		if aerr != nil {
			return nil, aerr
		}
		p.accepts = append(p.accepts, entry)
	}
	return p, nil
}

func (p *Policy) validateAccept(i int, a rawAccept) (accept, *Error) {
	key := func(field string) string { return indexed("accept", i) + "." + field }
	for _, f := range []struct{ name, value string }{
		{"column", a.Column}, {"path", a.Path}, {"note", a.Note}, {"reviewed_by", a.ReviewedBy},
	} {
		if strings.TrimSpace(f.value) == "" {
			return accept{}, &Error{Key: key(f.name), Msg: "is missing or empty"}
		}
	}
	column := strings.ToLower(a.Column)
	if !p.namesWarnColumn(column) {
		return accept{}, &Error{Key: key("column"), Msg: "is not table.column naming a warn column"}
	}
	if problem := relPathProblem(a.Path); problem != "" {
		return accept{}, &Error{Key: key("path"), Msg: problem}
	}
	if strings.Count(a.Path, "/") < 2 {
		return accept{}, &Error{Key: key("path"), Msg: "is shallower than three segments"}
	}
	return accept{column: column, path: a.Path}, nil
}

// namesWarnColumn reports whether column, lowercased, is table.column with a
// warn column's name. Table and column names may hold dots, so any split at
// a dot with both sides non-empty will do.
func (p *Policy) namesWarnColumn(column string) bool {
	for i := range len(column) {
		if column[i] == '.' && i > 0 && i < len(column)-1 && p.warn[column[i+1:]] {
			return true
		}
	}
	return false
}

// relPathProblem says what keeps s from being a plain repository-relative
// path, or "" if nothing does.
func relPathProblem(s string) string {
	switch {
	case s == "":
		return "is empty"
	case strings.Contains(s, `\`):
		return "uses a backslash"
	case strings.HasPrefix(s, "/") || isDrive(s):
		return "is absolute"
	}
	for seg := range strings.SplitSeq(s, "/") {
		switch seg {
		case "":
			return "has an empty segment"
		case ".", "..":
			return "uses . or .."
		}
	}
	return ""
}

// isDrive reports whether s starts with a Windows drive letter and colon.
func isDrive(s string) bool {
	return len(s) >= 2 && s[1] == ':' && ('a' <= s[0] && s[0] <= 'z' || 'A' <= s[0] && s[0] <= 'Z')
}

func compileGlobs(key string, patterns []string) (globs, *Error) {
	gs := make(globs, 0, len(patterns))
	for i, pat := range patterns {
		g, err := compileGlob(pat)
		if err != nil {
			return nil, &Error{Key: indexed(key, i), Msg: err.Error()}
		}
		gs = append(gs, g)
	}
	return gs, nil
}

// covers reports whether some scan kind's glob can match a file under root.
func (p *Policy) covers(root string) bool {
	for _, k := range Kinds {
		for _, g := range p.scan[k] {
			if g.coversUnder(root) {
				return true
			}
		}
	}
	return false
}

func indexed(key string, i int) string { return fmt.Sprintf("%s[%d]", key, i) }

func lowerSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[strings.ToLower(n)] = true
	}
	return set
}

func on(b *bool) bool { return b == nil || *b }
