// Package sqlfile scans SQL files, templated with Jinja (dbt) or not
// (docs/design/behavior.md, "SQL files").
//
// A template is not rendered. Its delimiters are blanked, keeping offsets and
// line breaks, and the inside is lexed as SQL, so a value cannot hide in a
// template block. A template comment is a comment. Literals are associated
// with columns as in model code (exprlex); a number is judged by its column
// only.
package sqlfile

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/craigalodon/pbipx/internal/exprlex"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/shape"
)

var _ judge.Scanner = Scan

// Scan judges every literal and comment of one SQL file. A file that does
// not lex returns a *judge.ParseError (unlexable-sql) with its line, and no
// findings.
func Scan(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	vals, err := Values(string(data))
	if err != nil {
		return nil, ParseError(err)
	}
	var out []finding.Finding
	for _, v := range vals {
		ctx := finding.SQL
		if v.Comment {
			ctx = finding.SQLComment
		}
		if verdict, ok := v.Judge(p); ok {
			out = append(out, verdict.Finding(path, ctx, v.Locator, v.Text))
		}
	}
	finding.Sort(out)
	return out, nil
}

// ParseError turns an error from Values into the unlexable-sql parse error,
// keeping its line and structure only.
func ParseError(err error) *judge.ParseError {
	pe := &judge.ParseError{Reason: finding.UnlexableSQL, Msg: "the SQL does not lex"}
	var le *exprlex.LexError
	if errors.As(err, &le) {
		pe.Line, pe.Msg = le.Line, le.Msg
	}
	return pe
}

// Value is one value read out of SQL: a literal, a comment judged whole, or
// a reading of a comment (a quoted run in it, or a literal its SQL ties to a
// column).
type Value struct {
	Text    string
	Kind    judge.Kind
	Columns []finding.Column
	// Comment is set for a comment and its readings.
	Comment bool
	// NameOnly is set for a quoted run inside free text, which is judged for
	// the person-name shape only.
	NameOnly bool
	// Locator is structure only: "lit3", "comment0", "comment0.quoted1".
	Locator string
}

// Judge decides v's verdict under p. Columns are judged by their names as
// written; the verdict's column is then printable (see Printable).
func (v Value) Judge(p *policy.Policy) (judge.Verdict, bool) {
	if v.NameOnly {
		return NameShape(v.Text, p)
	}
	verdict, ok := judge.Literal(v.Text, v.Kind, v.Columns, p)
	if ok && verdict.Column != nil {
		c := Printable(*verdict.Column)
		verdict.Column = &c
	}
	return verdict, ok
}

// Printable is c as a finding may print it: a table or column name that
// shape.PrintableKey refuses is written {?}. A name read from a quoted
// identifier or a YAML key is text from the file, and could hold a value.
func Printable(c finding.Column) finding.Column {
	if c.Table != "" && !shape.PrintableKey(c.Table) {
		c.Table = "{?}"
	}
	if !shape.PrintableKey(c.Name) {
		c.Name = "{?}"
	}
	return c
}

// NameShape judges a quoted run inside free text for the person-name shape
// only (behavior.md, "Free text inside code"). The run is expected to have
// the shape already; the policy can still switch the shape off or allow the
// literal.
func NameShape(run string, p *policy.Policy) (judge.Verdict, bool) {
	if run == "" || p.AllowsValue(run) || !p.Shapes().PersonName {
		return judge.Verdict{}, false
	}
	return judge.Verdict{Tier: finding.Block, Reasons: []finding.Reason{finding.PersonNameShape}}, true
}

// Values reads every value of src, a SQL text that may hold Jinja: each
// literal with the columns it is associated with, and each comment, judged
// whole and read again for quoted runs and as SQL. Its error is a
// *exprlex.LexError, which carries a line and a structure only.
func Values(src string) ([]Value, error) {
	blanked, tcoms, err := blankTemplate(src)
	if err != nil {
		return nil, err
	}
	sites, coms, err := exprlex.SQLSites(blanked)
	if err != nil {
		return nil, err
	}
	var out []Value
	for _, s := range sites {
		kind := judge.String
		if s.Number {
			kind = judge.Number
		}
		out = append(out, Value{Text: s.Value, Kind: kind, Columns: s.Columns, Locator: "lit" + strconv.Itoa(s.Index)})
	}
	coms = append(tcoms, coms...)
	slices.SortStableFunc(coms, func(a, b exprlex.Comment) int { return a.Line - b.Line })
	for i, c := range coms {
		loc := "comment" + strconv.Itoa(i)
		out = append(out, Value{Text: c.Text, Kind: judge.String, Comment: true, Locator: loc})
		for _, r := range Readings(c.Text, true, loc) {
			r.Comment = true
			out = append(out, r)
		}
	}
	return out, nil
}

// Readings is the extra readings of free text (a comment, a description, a
// YAML value), whose whole is judged by the caller: every quoted run with
// the name shape, judged for that shape only, and, when sql is set, every
// literal the text ties to a column when lexed as SQL, best effort. Each
// locator is base plus the reading's suffix.
func Readings(text string, sql bool, base string) []Value {
	var out []Value
	for _, fv := range exprlex.FreeText(text, sql) {
		v := Value{Text: fv.Value, Kind: judge.String, Columns: fv.Columns, Locator: base + fv.Suffix}
		switch {
		case fv.Number:
			v.Kind = judge.Number
		case len(fv.Columns) == 0:
			v.NameOnly = true
		}
		out = append(out, v)
	}
	return out
}

// Template comment error message.
const msgTemplateComment = "unterminated template comment"

// blankTemplate blanks Jinja's delimiters ({{ }}, {% %} and their - and +
// trim markers) with spaces, so the inside is lexed as SQL at the same
// offsets and lines. A {# #} comment is taken out as a comment and blanked
// whole, its line breaks kept; an unterminated one is a lex error.
func blankTemplate(src string) (string, []exprlex.Comment, error) {
	if !strings.Contains(src, "{") {
		return src, nil, nil
	}
	b := []byte(src)
	var coms []exprlex.Comment
	line := 1
	closer := "" // "}}" or "%}" while a block is open
	for i := 0; i < len(b); {
		switch {
		case b[i] == '\n':
			line++
			i++
		case closer == "" && strings.HasPrefix(src[i:], "{#"):
			end := strings.Index(src[i+2:], "#}")
			if end < 0 {
				return "", nil, &exprlex.LexError{Line: line, Msg: msgTemplateComment}
			}
			inner := src[i+2 : i+2+end]
			text := strings.TrimSpace(trimMarkers(inner))
			coms = append(coms, exprlex.Comment{Text: text, Line: line})
			stop := i + 2 + end + 2
			for ; i < stop; i++ {
				if b[i] == '\n' {
					line++
				} else {
					b[i] = ' '
				}
			}
		case closer == "" && (strings.HasPrefix(src[i:], "{{") || strings.HasPrefix(src[i:], "{%")):
			if src[i+1] == '{' {
				closer = "}}"
			} else {
				closer = "%}"
			}
			b[i], b[i+1] = ' ', ' '
			i += 2
			if i < len(b) && (b[i] == '-' || b[i] == '+') {
				b[i] = ' '
				i++
			}
		case closer != "" && strings.HasPrefix(src[i:], closer):
			if i > 0 && (src[i-1] == '-' || src[i-1] == '+') {
				b[i-1] = ' '
			}
			b[i], b[i+1] = ' ', ' '
			i += 2
			closer = ""
		default:
			i++
		}
	}
	return string(b), coms, nil
}

// trimMarkers removes a template comment's trim markers ({#- -#}).
func trimMarkers(s string) string {
	s = strings.TrimLeft(s, "-+")
	return strings.TrimRight(s, "-+")
}
