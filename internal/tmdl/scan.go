package tmdl

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/craigalodon/pbipx/internal/exprlex"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/shape"
)

var _ judge.Scanner = Scan

// Scan judges every value one TMDL file persists (docs/design/behavior.md,
// "Models (TMDL)"): the literals of its DAX, M and native SQL, the cells of
// its Enter data tables, its property values, descriptions, comments and
// other text, and its role members. A file that does not parse, an
// expression the lexer rejects, or an Enter data blob that cannot be decoded
// returns a *judge.ParseError and no findings.
func Scan(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	root, err := Parse(data)
	if err != nil {
		return nil, err
	}
	s := &scanner{path: path, p: p}
	if err := s.children(root, "", scope{}); err != nil {
		return nil, err
	}
	finding.Sort(s.out)
	return s.out, nil
}

type scanner struct {
	path string
	p    *policy.Policy
	out  []finding.Finding
	// counts, when set, makes the scanner count each value it would judge,
	// by context, and judge nothing (Describe).
	counts map[finding.Context]int
}

// scope is the table being declared, and its declared columns.
type scope struct {
	table   string
	columns []finding.Column
}

func tableScope(n *Node) scope {
	sc := scope{table: n.Name}
	for _, c := range n.Children {
		if strings.EqualFold(c.Kind, "column") && c.Name != "" {
			sc.columns = append(sc.columns, finding.Column{Table: n.Name, Name: c.Name})
		}
	}
	return sc
}

// lang is how an `=` body is read.
type lang int

const (
	langText lang = iota // free text: an annotation, extended property, ...
	langNone             // not a value: a partition's type
	langDAX
	langCalculated // DAX that declares a calculated table's rows
	langM
)

// daxKinds are the declarations whose body is DAX, lowercased.
var daxKinds = map[string]bool{
	"measure": true, "column": true, "calculationitem": true, "tablepermission": true,
	"formatstringdefinition": true, "detailrowsdefinition": true, "defaultdetailrowsdefinition": true,
	"targetexpression": true, "statusexpression": true, "trendexpression": true,
	"multipleoremptyselectionexpression": true, "noselectionexpression": true,
}

// language decides how the body of n, a child of parent, is read.
func language(n, parent *Node) lang {
	kind := strings.ToLower(n.Kind)
	switch {
	case kind == "partition":
		return langNone
	case kind == "source" && strings.EqualFold(parent.Kind, "partition"):
		switch strings.ToLower(strings.TrimSpace(parent.Value)) {
		case "m", "policyrange":
			return langM
		case "calculated":
			return langCalculated
		}
		return langText
	case kind == "expression" && n.Name != "":
		return langM
	case kind == "expression":
		return langDAX
	case kind == "sourceexpression", kind == "pollingexpression":
		return langM
	case daxKinds[kind]:
		return langDAX
	}
	return langText
}

// children visits the declarations under parent, whose locator is at.
func (s *scanner) children(parent *Node, at string, sc scope) error {
	if strings.EqualFold(parent.Kind, "table") {
		sc = tableScope(parent)
	}
	role := strings.EqualFold(parent.Kind, "role")
	members := 0
	for _, n := range parent.Children {
		var loc string
		switch {
		case n.Kind == DescriptionKind:
			loc = join(at, "description")
		case role && strings.EqualFold(n.Kind, "member"):
			loc = join(at, "member#"+strconv.Itoa(members))
			members++
		default:
			loc = join(at, segment(n))
		}
		if err := s.node(n, parent, loc, sc); err != nil {
			return err
		}
	}
	return nil
}

// node judges one declaration and everything under it.
func (s *scanner) node(n, parent *Node, at string, sc scope) error {
	if n.Description != "" {
		s.freeText(n.Description, finding.TMDLDescription, at+"#description")
	}
	switch {
	case n.Kind == DescriptionKind:
		return nil
	case strings.EqualFold(n.Kind, "member") && strings.EqualFold(parent.Kind, "role"):
		// Every member blocks, whatever its identity looks like, and the
		// identity is never judged any other way (principle 9).
		if n.Name != "" {
			v := judge.Verdict{Tier: finding.Block, Reasons: []finding.Reason{finding.RoleMember}}
			s.out = append(s.out, v.Finding(s.path, finding.TMDLRoleMember, at, n.Name))
		}
	case n.Sep == Colon:
		s.literal(unquote(n.Value), judge.String, nil, finding.TMDLProperty, at)
	case n.Sep == Equals:
		var err error
		switch language(n, parent) {
		case langDAX:
			err = s.dax(n.Value, n.ValueLine, at, sc, false)
		case langCalculated:
			err = s.dax(n.Value, n.ValueLine, at, sc, true)
		case langM:
			err = s.m(n.Value, n.ValueLine, at, sc)
		case langText:
			s.text(n.Value, at)
		}
		if err != nil {
			return err
		}
	}
	return s.children(n, at, sc)
}

// dax judges a DAX body that starts on line. In a calculated table's
// source, the cells of a {...} constructor take the table's columns.
func (s *scanner) dax(src string, line int, at string, sc scope, calculated bool) error {
	toks, coms, err := exprlex.DAX(src)
	if err != nil {
		return lexError(err, line)
	}
	headers := exprlex.DatatableHeaders(toks)
	var braces map[int]bool
	if calculated {
		braces = exprlex.InBraces(toks)
	}
	sites, err := exprlex.AssociateChecked(toks, "&")
	if err != nil {
		return lexError(err, line)
	}
	for _, site := range sites {
		cols := site.Columns
		if h, ok := headers[site.Index]; ok {
			for _, name := range h {
				cols = addColumn(cols, finding.Column{Table: sc.table, Name: name})
			}
		} else if braces[site.Index] {
			for _, c := range sc.columns {
				cols = addColumn(cols, c)
			}
		}
		s.literal(site.Value, kindOf(site.Number), cols, finding.TMDLDAX, lit(at, site.Index))
	}
	s.comments(coms, at, false)
	return nil
}

// m judges an M body that starts on line: its literals, the SQL in its
// strings, its Enter data blobs and its comments.
func (s *scanner) m(src string, line int, at string, sc scope) error {
	toks, coms, err := exprlex.M(src)
	if err != nil {
		return lexError(err, line)
	}
	blobs := blobLiterals(toks)
	blobN := 0
	sites, err := exprlex.AssociateChecked(toks, "&")
	if err != nil {
		return lexError(err, line)
	}
	for _, site := range sites {
		siteLine := line + site.Line - 1
		if tok, ok := blobs[site.Index]; ok {
			// The blob is judged as the table it encodes, not as a string.
			fields := typeTableFields(toks, tok)
			loc := at + "#blob" + strconv.Itoa(blobN)
			blobN++
			if err := s.enterData(site.Value, siteLine, loc, fields, sc); err != nil {
				return err
			}
			continue
		}
		cols := make([]finding.Column, 0, len(site.Columns))
		for _, c := range site.Columns {
			if c.Table == "" {
				c.Table = sc.table // an M column takes the table being declared
			}
			cols = addColumn(cols, c)
		}
		loc := lit(at, site.Index)
		s.literal(site.Value, kindOf(site.Number), cols, finding.TMDLM, loc)
		if !site.Number && exprlex.LooksLikeSQL(site.Value) {
			if err := s.nativeSQL(site.Value, siteLine, loc); err != nil {
				return err
			}
		}
	}
	s.comments(coms, at, false)
	return nil
}

// nativeSQL judges SQL held in an M string that starts on line. The
// string's double quotes were M's, so SQL reads them as neither strings
// nor names. A bare column stays bare: it does not take the M table.
func (s *scanner) nativeSQL(src string, line int, at string) error {
	toks, coms, err := exprlex.SQL(src, exprlex.QuoteSkip)
	if err != nil {
		return lexError(err, line)
	}
	at += "/sql"
	sites, err := exprlex.AssociateChecked(toks, "||")
	if err != nil {
		return lexError(err, line)
	}
	for _, site := range sites {
		s.literal(site.Value, kindOf(site.Number), site.Columns, finding.TMDLNativeSQL, lit(at, site.Index))
	}
	s.comments(coms, at, true)
	return nil
}

// comments judges each comment whole, then its free-text readings; a
// comment in SQL is also lexed as SQL.
func (s *scanner) comments(coms []exprlex.Comment, at string, sql bool) {
	for i, c := range coms {
		loc := at + "#comment" + strconv.Itoa(i)
		s.literal(c.Text, judge.String, nil, finding.TMDLComment, loc)
		s.readings(c.Text, sql, finding.TMDLComment, loc)
	}
}

// freeText judges text whole, then its free-text readings.
func (s *scanner) freeText(text string, ctx finding.Context, at string) {
	s.literal(text, judge.String, nil, ctx, at)
	s.readings(text, false, ctx, at)
}

// readings judges the extra readings of free text (behavior.md, "Free text
// inside code"). A quoted run is judged for the name shape only; a literal
// the text ties to a column, as a literal.
func (s *scanner) readings(text string, sql bool, ctx finding.Context, at string) {
	if s.counts != nil {
		return // a reading is not a literal of its own
	}
	for _, fv := range exprlex.FreeText(text, sql) {
		loc := at + fv.Suffix
		if len(fv.Columns) > 0 {
			s.literal(fv.Value, kindOf(fv.Number), fv.Columns, ctx, loc)
			continue
		}
		if fv.Value == "" || s.p.AllowsValue(fv.Value) || !s.p.Shapes().PersonName {
			continue
		}
		v := judge.Verdict{Tier: finding.Block, Reasons: []finding.Reason{finding.PersonNameShape}}
		s.out = append(s.out, v.Finding(s.path, ctx, loc, fv.Value))
	}
}

// text judges any other `=` body: as JSON, each string by itself; otherwise
// the whole text.
func (s *scanner) text(body, at string) {
	strs, ok := jsonStrings([]byte(body))
	if !ok {
		s.freeText(body, finding.TMDLText, at)
		return
	}
	for i, str := range strs {
		s.freeText(str, finding.TMDLText, at+"#json"+strconv.Itoa(i))
	}
}

// literal judges one value and keeps its finding, if it fires.
func (s *scanner) literal(value string, kind judge.Kind, cols []finding.Column, ctx finding.Context, loc string) {
	if s.counts != nil {
		s.counts[ctx]++
		return
	}
	if v, ok := judge.Literal(value, kind, cols, s.p); ok {
		s.out = append(s.out, v.Finding(s.path, ctx, loc, value))
	}
}

// maxBlob bounds a decompressed Enter data blob.
const maxBlob = 16 << 20

// enterData decodes an Enter data blob, base64 around raw deflate or plain
// bytes, and judges each cell: each string and number of its JSON, or the
// whole text when it is not JSON. Cells take the fields of the expression's
// type table, or else the table's columns.
func (s *scanner) enterData(b64 string, line int, at string, fields []string, sc scope) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return &judge.ParseError{Reason: finding.UnparseableTMDL, Line: line, Msg: "Enter data blob is not base64"}
	}
	candidates := [][]byte{raw}
	inflated, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(raw)), maxBlob+1))
	if err == nil && len(inflated) <= maxBlob {
		candidates = [][]byte{inflated, raw}
	}
	cols := sc.columns
	if fields != nil {
		cols = make([]finding.Column, 0, len(fields))
		for _, f := range fields {
			cols = addColumn(cols, finding.Column{Table: sc.table, Name: f})
		}
	}
	for _, data := range candidates {
		if cells, ok := jsonCells(data); ok {
			for i, c := range cells {
				s.literal(c.value, kindOf(c.number), cols, finding.TMDLEnterData, at+"/cell"+strconv.Itoa(i))
			}
			return nil
		}
	}
	for _, data := range candidates {
		if utf8.Valid(data) {
			s.literal(string(data), judge.String, cols, finding.TMDLEnterData, at+"/cell0")
			return nil
		}
	}
	return &judge.ParseError{Reason: finding.UnparseableTMDL, Line: line, Msg: "Enter data blob decodes to neither JSON nor text"}
}

type cell struct {
	value  string
	number bool
}

// jsonCells lists the strings and numbers of a JSON document, in order;
// booleans and nulls are not cells. It reports false when data is not JSON.
func jsonCells(data []byte) ([]cell, bool) {
	if !json.Valid(data) {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out []cell
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out, true
		}
		if err != nil {
			return nil, false
		}
		switch v := tok.(type) {
		case string:
			out = append(out, cell{value: v})
		case json.Number:
			out = append(out, cell{value: v.String(), number: true})
		}
	}
}

// jsonStrings lists the strings of a JSON document, keys included, in
// order. It reports false when data is not JSON.
func jsonStrings(data []byte) ([]string, bool) {
	cells, ok := jsonCells(data)
	if !ok {
		return nil, false
	}
	var out []string
	for _, c := range cells {
		if !c.number {
			out = append(out, c.value)
		}
	}
	return out, true
}

// blobLiterals maps the literal index of each Binary.FromText string
// argument to its token index.
func blobLiterals(toks []exprlex.Token) map[int]int {
	out := map[int]int{}
	n := 0
	for i, t := range toks {
		if t.Kind != exprlex.Str && t.Kind != exprlex.Num {
			continue
		}
		if t.Kind == exprlex.Str && i >= 2 && toks[i-1].Kind == exprlex.Open && toks[i-1].Text == "(" &&
			toks[i-2].Kind == exprlex.Ident && strings.EqualFold(toks[i-2].Text, "Binary.FromText") {
			out[n] = i
		}
		n++
	}
	return out
}

// typeTableFields returns the field names of the first `type table [...]`
// after token from, or nil when there is none.
func typeTableFields(toks []exprlex.Token, from int) []string {
	word := func(i int, w string) bool {
		return i < len(toks) && toks[i].Kind == exprlex.Ident && strings.EqualFold(toks[i].Text, w)
	}
	for i := from; i+2 < len(toks); i++ {
		if !word(i, "type") || !word(i+1, "table") {
			continue
		}
		open := i + 2
		switch t := toks[open]; {
		case t.Kind == exprlex.Col:
			return []string{t.Text} // type table [name]
		case t.Kind == exprlex.Open && t.Text == "[":
			return recordFields(toks, open)
		}
	}
	return nil
}

// recordFields lists the field names of the record type opened at open: a
// name right after the [ or a comma, at the record's own depth.
func recordFields(toks []exprlex.Token, open int) []string {
	fields := []string{}
	depth := 0
	for j := open; j < len(toks); j++ {
		t := toks[j]
		switch t.Kind {
		case exprlex.Open:
			depth++
		case exprlex.Close:
			depth--
			if depth == 0 {
				return fields
			}
		case exprlex.Ident, exprlex.Col:
			prev := toks[j-1]
			if depth == 1 && (j-1 == open || prev.Kind == exprlex.Comma) {
				fields = append(fields, t.Text)
			}
		}
	}
	return fields
}

// lexError turns a lexer error in a body that starts on line into a parse
// error on the file's line.
func lexError(err error, line int) error {
	var le *exprlex.LexError
	if errors.As(err, &le) {
		return &judge.ParseError{Reason: finding.UnparseableTMDL, Line: line + le.Line - 1, Msg: le.Msg}
	}
	return &judge.ParseError{Reason: finding.UnparseableTMDL, Line: line, Msg: "expression could not be lexed"}
}

func kindOf(number bool) judge.Kind {
	if number {
		return judge.Number
	}
	return judge.String
}

func addColumn(cols []finding.Column, c finding.Column) []finding.Column {
	if slices.Contains(cols, c) {
		return cols
	}
	return append(cols, c)
}

// unquote removes the quotes around a quoted property value.
func unquote(v string) string {
	for _, q := range []string{"'", `"`} {
		if len(v) >= 2 && strings.HasPrefix(v, q) && strings.HasSuffix(v, q) {
			return strings.ReplaceAll(v[1:len(v)-1], q+q, q)
		}
	}
	return v
}

// segment is a declaration's part of a locator: its kind, and its name in
// brackets. A kind or name that shape.PrintableKey refuses is written as
// {?}, so a locator, and the fingerprint that hashes it, never carries text
// that is not schema.
func segment(n *Node) string {
	seg := printable(n.Kind)
	if n.Name != "" {
		seg += "[" + printable(n.Name) + "]"
	}
	return seg
}

func printable(key string) string {
	if !shape.PrintableKey(key) {
		return "{?}"
	}
	return key
}

func join(at, seg string) string {
	if at == "" {
		return seg
	}
	return at + "/" + seg
}

func lit(at string, i int) string { return at + "#lit" + strconv.Itoa(i) }
