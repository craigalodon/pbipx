package policy

import (
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/finding"
)

// Class is how the policy treats a file (docs/design/behavior.md, "Files and
// the default deny"; principle 8).
type Class int

// Classes.
const (
	// Outside is a file under no protected root and in no scan scope: ignored.
	Outside Class = iota
	// LocalState is Power BI Desktop's UI state under a protected root: never
	// read, and skipped by tree scans.
	LocalState
	// Scanned is a file in a kind's scan scope; Classify says which kind.
	Scanned
	// UnscannedAllowed is an opaque file under a protected root that is
	// allowed without a scan.
	UnscannedAllowed
	// Unvettable is a file under a protected root that no class covers: an
	// error finding.
	Unvettable
)

func (c Class) String() string {
	switch c {
	case Outside:
		return "outside"
	case LocalState:
		return "local-state"
	case Scanned:
		return "scanned"
	case UnscannedAllowed:
		return "unscanned-allowed"
	case Unvettable:
		return "unvettable"
	}
	return "unknown"
}

// ProtectedRoots returns the protected roots, as written.
func (p *Policy) ProtectedRoots() []string { return slices.Clone(p.roots) }

// UnderRoot reports whether path is a protected root or below one. Paths are
// repository-relative with forward slashes, as Classify takes them.
func (p *Policy) UnderRoot(path string) bool {
	for _, r := range p.roots {
		if underPrefix(path, r) {
			return true
		}
	}
	return false
}

// Classify says how the policy treats the file at path, a clean
// repository-relative path with forward slashes, and for a scanned file its
// kind. Under a protected root the classes are tried in order: local state,
// scanned, unscanned-allowed, and otherwise unvettable. Outside every root a
// file is scanned if in scope and otherwise outside.
func (p *Policy) Classify(path string) (Class, Kind) {
	under := p.UnderRoot(path)
	if under && p.localState.match(path) {
		return LocalState, ""
	}
	if k := p.scanKind(path); k != "" {
		return Scanned, k
	}
	switch {
	case !under:
		return Outside, ""
	case p.unscannedAllow.match(path):
		return UnscannedAllowed, ""
	}
	return Unvettable, ""
}

// scanKind is the first kind, in Kinds order, whose globs match path, or ""
// when none does or path is excluded.
func (p *Policy) scanKind(path string) Kind {
	if p.exclude.match(path) {
		return ""
	}
	for _, k := range Kinds {
		if p.scan[k].match(path) {
			return k
		}
	}
	return ""
}

// BlocksColumn reports whether the bare column name, lowercased, is in the
// block list or matches the block pattern.
func (p *Policy) BlocksColumn(name string) bool {
	lower := strings.ToLower(name)
	return p.block[lower] || (p.blockPattern != nil && p.blockPattern.MatchString(lower))
}

// WarnsColumn reports whether the bare column name, lowercased, is in the
// warn list. A column that BlocksColumn reports is a block whatever this
// says.
func (p *Policy) WarnsColumn(name string) bool {
	return p.warn[strings.ToLower(name)]
}

// AllowsValue reports whether literal is in the allow list: an exact,
// case-sensitive match. An allowed literal is not judged.
func (p *Policy) AllowsValue(literal string) bool {
	return p.allow[literal]
}

// Shapes returns the shape switches.
func (p *Policy) Shapes() Shapes { return p.shapes }

// Accepts reports whether an [[accept]] entry accepts f: a warning whose
// reasons are exactly warn-column, whose column is the entry's table.column
// (compared lowercased), at or below the entry's path. A block is never
// accepted.
func (p *Policy) Accepts(f finding.Finding) bool {
	if f.Tier != finding.Warn || f.Column == nil || f.Column.Table == "" {
		return false
	}
	if !slices.Equal(f.Reasons, []finding.Reason{finding.WarnColumn}) {
		return false
	}
	column := strings.ToLower(f.Column.String())
	for _, a := range p.accepts {
		if a.column == column && underPrefix(f.Path, a.path) {
			return true
		}
	}
	return false
}

// underPrefix reports whether path is prefix or below it, segment by segment.
func underPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
