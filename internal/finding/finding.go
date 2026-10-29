// Package finding holds what a scan reports: findings, their tiers, contexts
// and reasons, and the result of a scan.
//
// The contexts, reasons, failures and exit codes here are the corpus's closed
// vocabulary (testdata/corpus/vocabulary.toml); a test keeps them in step. A
// finding's value is a secret.Value, and no output path in this package
// renders it.
package finding

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/secret"
)

// Tier is a finding's severity.
type Tier string

// Tiers.
const (
	Block Tier = "block" // fails the scan; never accepted
	Warn  Tier = "warn"  // fails only under --strict; may be accepted
	Error Tier = "error" // the file was not vetted
)

// Context is where a value was persisted.
type Context string

// Report contexts.
const (
	Filter            Context = "filter"
	SlicerSelection   Context = "slicer-selection"
	ExpansionState    Context = "expansion-state"
	InsightSelector   Context = "insight-selector"
	ConditionalFormat Context = "conditional-format"
	TitleText         Context = "title-text"
	Bookmark          Context = "bookmark"
)

// Model contexts.
const (
	TMDLDAX         Context = "tmdl-dax"
	TMDLM           Context = "tmdl-m"
	TMDLNativeSQL   Context = "tmdl-native-sql"
	TMDLEnterData   Context = "tmdl-enter-data"
	TMDLProperty    Context = "tmdl-property"
	TMDLDescription Context = "tmdl-description"
	TMDLComment     Context = "tmdl-comment"
	TMDLText        Context = "tmdl-text"
	TMDLRoleMember  Context = "tmdl-role-member"
)

// SQL, YAML and prose contexts.
const (
	SQL         Context = "sql"
	SQLComment  Context = "sql-comment"
	YAML        Context = "yaml"
	YAMLKey     Context = "yaml-key"
	YAMLComment Context = "yaml-comment"
	YAMLSQL     Context = "yaml-sql"
	Text        Context = "text"
)

// Contexts lists every context.
var Contexts = []Context{
	Filter, SlicerSelection, ExpansionState, InsightSelector, ConditionalFormat, TitleText, Bookmark,
	TMDLDAX, TMDLM, TMDLNativeSQL, TMDLEnterData, TMDLProperty, TMDLDescription, TMDLComment, TMDLText, TMDLRoleMember,
	SQL, SQLComment, YAML, YAMLKey, YAMLComment, YAMLSQL, Text,
}

// Reason is why a finding fired.
type Reason string

// Finding reasons, in the order they are listed within a finding.
const (
	BlockColumn     Reason = "block-column"
	WarnColumn      Reason = "warn-column"
	RoleMember      Reason = "role-member"
	PersonNameShape Reason = "person-name-shape"
	EmailShape      Reason = "email-shape"
	SSNShape        Reason = "ssn-shape"
	DateShape       Reason = "date-shape"
	PhoneShape      Reason = "phone-shape"
)

// Error reasons: why a file was not vetted.
const (
	Unvettable      Reason = "unvettable"
	Unreadable      Reason = "unreadable"
	NotUTF8         Reason = "not-utf8"
	UnparseableJSON Reason = "unparseable-json"
	UnparseableTMDL Reason = "unparseable-tmdl"
	UnlexableSQL    Reason = "unlexable-sql"
	UnparseableYAML Reason = "unparseable-yaml"
)

// ReasonOrder is the order of finding reasons within one finding.
var ReasonOrder = []Reason{
	BlockColumn, WarnColumn, RoleMember,
	PersonNameShape, EmailShape, SSNShape, DateShape, PhoneShape,
}

// ErrorReasons lists every error reason.
var ErrorReasons = []Reason{
	Unvettable, Unreadable, NotUTF8, UnparseableJSON, UnparseableTMDL, UnlexableSQL, UnparseableYAML,
}

// SortReasons puts finding reasons in ReasonOrder. Reasons not in it, which
// only an error finding has, keep their order after the rest.
func SortReasons(reasons []Reason) {
	rank := func(r Reason) int {
		if i := slices.Index(ReasonOrder, r); i >= 0 {
			return i
		}
		return len(ReasonOrder)
	}
	slices.SortStableFunc(reasons, func(a, b Reason) int { return cmp.Compare(rank(a), rank(b)) })
}

// Column is a model column a value is associated with. Table is empty when
// the source does not say.
type Column struct {
	Table string
	Name  string
}

// String is table.column, or the bare name when the table is unknown.
func (c Column) String() string {
	if c.Table == "" {
		return c.Name
	}
	return c.Table + "." + c.Name
}

// Finding is one persisted value, or one file that could not be vetted.
type Finding struct {
	Path    string  // relative to the repository root, forward slashes
	Context Context // empty for an error
	Locator string  // where in the file; structure only, never source text
	Column  *Column // the column that decided the verdict, if any
	Tier    Tier
	Reasons []Reason
	Value   secret.Value // zero for an error
}

// fingerprintVersion is bumped whenever what a fingerprint hashes changes.
const fingerprintVersion = "fp1"

// Fingerprint identifies a finding for the baseline. It hashes the location,
// the column and the reasons, never the value (principle 3): a changed value
// of the same kind keeps its fingerprint, and a value that gains a reason
// does not.
func (f Finding) Fingerprint() string {
	reasons := make([]string, len(f.Reasons))
	for i, r := range f.Reasons {
		reasons[i] = string(r)
	}
	slices.Sort(reasons)
	column := ""
	if f.Column != nil {
		column = f.Column.String()
	}
	parts := []string{fingerprintVersion, f.Path, string(f.Context), f.Locator, column, strings.Join(reasons, "|")}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fingerprintVersion + ":" + hex.EncodeToString(sum[:12])
}

// Sort orders findings by path, context, column, then reasons, with an empty
// context or no column first (docs/design/corpus.md).
func Sort(findings []Finding) {
	slices.SortStableFunc(findings, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.Path, b.Path),
			cmp.Compare(a.Context, b.Context),
			compareColumns(a.Column, b.Column),
			slices.Compare(a.Reasons, b.Reasons),
		)
	})
}

func compareColumns(a, b *Column) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return cmp.Compare(a.String(), b.String())
}
