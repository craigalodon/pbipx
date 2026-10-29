// Package judge decides one literal's verdict, and defines what a file
// scanner is.
//
// Every scanner reduces a file to literals, each with the columns it is
// associated with, and calls Literal (docs/design/behavior.md, "Judging one
// literal"). Scanners work on the plain string; a finding carries it as a
// secret.Value.
package judge

import (
	"fmt"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/secret"
	"github.com/craigalodon/pbipx/internal/shape"
)

// Kind is how a literal is judged.
type Kind int

const (
	// String literals get the column verdict and the shapes.
	String Kind = iota
	// Number literals get the column verdict only: a number has no name or
	// email shape, and unassociated numbers are thresholds and flags.
	Number
	// TypedDate literals (a report's datetime'...') get the column verdict
	// only: every typed date has the date shape, so judging it would block
	// every date filter.
	TypedDate
)

// Verdict is the outcome for one literal that fires.
type Verdict struct {
	Tier    finding.Tier
	Reasons []finding.Reason
	Column  *finding.Column // the column that decided it; nil for shapes alone
}

// Literal judges value, associated with columns, under p. It returns false
// when the literal does not fire.
func Literal(value string, kind Kind, columns []finding.Column, p *policy.Policy) (Verdict, bool) {
	if value == "" || p.AllowsValue(value) {
		return Verdict{}, false
	}
	var blockCol, warnCol *finding.Column
	for i := range columns {
		c := &columns[i]
		if p.BlocksColumn(c.Name) {
			blockCol = c
			break
		}
		if warnCol == nil && p.WarnsColumn(c.Name) {
			warnCol = c
		}
	}
	var shapes []finding.Reason
	if kind == String {
		shapes = shape.Literal(value, switches(p.Shapes()))
	}
	switch {
	case blockCol != nil:
		col := *blockCol
		return Verdict{Tier: finding.Block, Reasons: append([]finding.Reason{finding.BlockColumn}, shapes...), Column: &col}, true
	case len(shapes) > 0:
		return Verdict{Tier: finding.Block, Reasons: shapes}, true
	case warnCol != nil:
		col := *warnCol
		return Verdict{Tier: finding.Warn, Reasons: []finding.Reason{finding.WarnColumn}, Column: &col}, true
	}
	return Verdict{}, false
}

// Finding builds the finding for a verdict.
func (v Verdict) Finding(path string, ctx finding.Context, locator, value string) finding.Finding {
	return finding.Finding{
		Path:    path,
		Context: ctx,
		Locator: locator,
		Column:  v.Column,
		Tier:    v.Tier,
		Reasons: v.Reasons,
		Value:   secret.New(value),
	}
}

func switches(s policy.Shapes) shape.Switches {
	return shape.Switches{
		PersonNameOff: !s.PersonName,
		EmailOff:      !s.Email,
		SSNOff:        !s.SSN,
		DateOff:       !s.Date,
		PhoneOff:      !s.Phone,
	}
}

// Scanner reads one file of its kind and returns its findings. It returns a
// *ParseError when it cannot vet the file; it never returns findings with
// one, because a file that does not parse produces only its error.
type Scanner func(path string, data []byte, p *policy.Policy) ([]finding.Finding, error)

// ParseError is a file a scanner could not vet. It carries a reason, a line
// and structure, never source text (principle 2).
type ParseError struct {
	Reason finding.Reason // an error reason, such as finding.UnparseableJSON
	Line   int            // 1-based; 0 when unknown
	Msg    string         // structure only: "unterminated string", "space indentation"
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s: line %d: %s", e.Reason, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Reason, e.Msg)
}
