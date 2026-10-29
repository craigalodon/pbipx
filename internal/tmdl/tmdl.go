// Package tmdl parses a model's TMDL files into a tree of declarations, and
// scans them for persisted values (Scan, in scan.go).
//
// The parser follows docs/design/behavior.md ("Models (TMDL)", "Parsing").
// It places every line by its tab indentation and fails closed: a line it
// cannot place is an error. Errors are *judge.ParseError values that name a
// line and a structure, never source text (principle 2). The parser judges
// nothing; it only gives Scan the declarations, their properties and their
// expression bodies.
package tmdl

import (
	"bytes"
	"strings"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
)

// Sep is what follows a declaration's kind and name.
type Sep int

const (
	// NoSep is a declaration (`kind name`) or a bare flag (`kind`).
	NoSep Sep = iota
	// Colon is a property: `kind: value`.
	Colon
	// Equals is an expression: `kind name = expr` or `kind = expr`.
	Equals
)

// DescriptionKind is the Kind of a trailing description: `///` lines with
// no declaration after them. Its text is in Description.
const DescriptionKind = "<description>"

// Node is one declaration. The root Node has Kind "" and holds the file's
// top-level declarations as Children.
type Node struct {
	Kind        string // the declaration keyword: table, column, measure, ...
	Name        string // "" when none; a quoted name is unquoted
	Sep         Sep
	Value       string // a property's value, or an expression's body
	Line        int    // 1-based line of the declaration
	ValueLine   int    // 1-based line where Value starts
	Description string // the `///` lines before the declaration, markers removed
	Children    []*Node
}

// fence opens and closes an expression written between fences.
const fence = "```"

// needsName lists the kinds that are always declared with a name. Any kind
// not listed may also stand alone, as a flag or an unnamed expression.
var needsName = map[string]bool{
	"table": true, "column": true, "measure": true, "partition": true,
	"role": true, "member": true, "hierarchy": true, "level": true,
	"relationship": true, "perspective": true, "perspectiveTable": true,
	"perspectiveColumn": true, "perspectiveMeasure": true,
	"perspectiveHierarchy": true, "calculationItem": true,
	"tablePermission": true, "columnPermission": true, "annotation": true,
	"extendedProperty": true, "cultureInfo": true, "dataSource": true,
	"queryGroup": true, "model": true, "ref": true,
	"function": true, "calendar": true, "variation": true,
}

// Parse parses one TMDL file. Line endings are normalized and a UTF-8 byte
// order mark is accepted.
func Parse(data []byte) (*Node, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	p := &parser{lines: strings.Split(text, "\n"), root: &Node{}}
	if err := p.parse(); err != nil {
		return nil, err
	}
	return p.root, nil
}

type parser struct {
	lines []string
	i     int // index of the next line
	root  *Node
	// stack is the open declarations: stack[d] takes the children at depth d.
	stack []*Node

	desc      []string // pending `///` lines
	descLine  int
	descDepth int
}

func fail(line int, msg string) error {
	return &judge.ParseError{Reason: finding.UnparseableTMDL, Line: line, Msg: msg}
}

// indent splits a line into its leading tab count and the rest. A space in
// the leading whitespace before any content is space indentation.
func indent(line string) (int, string, bool) {
	n := 0
	for n < len(line) && line[n] == '\t' {
		n++
	}
	rest := line[n:]
	if rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		return n, rest, false
	}
	return n, rest, true
}

func blank(line string) bool { return strings.TrimSpace(line) == "" }

func (p *parser) parse() error {
	p.stack = []*Node{p.root}
	for p.i < len(p.lines) {
		lineNo := p.i + 1
		line := p.lines[p.i]
		p.i++
		if blank(line) {
			continue
		}
		depth, rest, ok := indent(line)
		if !ok {
			return fail(lineNo, "space indentation")
		}
		if depth > len(p.stack)-1 {
			return fail(lineNo, "indentation jumps more than one level")
		}
		if strings.HasPrefix(rest, "///") {
			if p.desc == nil {
				p.descLine, p.descDepth = lineNo, depth
			}
			p.desc = append(p.desc, strings.TrimSpace(rest[3:]))
			continue
		}
		n, err := p.statement(rest, lineNo, depth)
		if err != nil {
			return err
		}
		if p.desc != nil {
			n.Description = strings.Join(p.desc, "\n")
			p.desc = nil
		}
		parent := p.stack[depth]
		parent.Children = append(parent.Children, n)
		p.stack = append(p.stack[:depth+1], n)
	}
	if p.desc != nil {
		// A trailing description is still text to judge.
		parent := p.stack[p.descDepth]
		parent.Children = append(parent.Children, &Node{
			Kind: DescriptionKind, Description: strings.Join(p.desc, "\n"),
			Line: p.descLine, ValueLine: p.descLine,
		})
	}
	return nil
}

func isIdentStart(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isIdent(c byte) bool { return isIdentStart(c) || ('0' <= c && c <= '9') }

// statement parses one declaration line, and the expression body after it.
func (p *parser) statement(s string, lineNo, depth int) (*Node, error) {
	if !isIdentStart(s[0]) {
		return nil, fail(lineNo, "statement does not start with an identifier")
	}
	k := 1
	for k < len(s) && isIdent(s[k]) {
		k++
	}
	n := &Node{Kind: s[:k], Line: lineNo}
	rest := strings.TrimLeft(s[k:], " \t")
	if k < len(s) && rest == s[k:] && rest[0] != ':' && rest[0] != '=' {
		return nil, fail(lineNo, "unexpected text after a kind")
	}

	switch {
	case rest == "":
		if needsName[n.Kind] {
			return nil, fail(lineNo, "missing name")
		}
		return n, nil
	case rest[0] == ':':
		n.Sep = Colon
		n.Value = strings.TrimSpace(rest[1:])
		n.ValueLine = lineNo
		return n, nil
	case rest[0] == '=':
		if needsName[n.Kind] {
			return nil, fail(lineNo, "missing name")
		}
		return n, p.expression(n, rest[1:], depth)
	case n.Kind == "ref":
		// `ref table Name`: the rest names what is referenced.
		n.Name = strings.TrimSpace(rest)
		return n, nil
	}

	name, after, err := parseName(rest, lineNo)
	if err != nil {
		return nil, err
	}
	n.Name = name
	after = strings.TrimLeft(after, " \t")
	switch {
	case after == "":
		return n, nil
	case after[0] == '=':
		return n, p.expression(n, after[1:], depth)
	}
	return nil, fail(lineNo, "unexpected text after a name")
}

// parseName reads a quoted or unquoted name and returns the text after it.
func parseName(s string, lineNo int) (string, string, error) {
	if s[0] != '\'' {
		end := strings.IndexAny(s, " \t=")
		if end < 0 {
			return s, "", nil
		}
		return s[:end], s[end:], nil
	}
	var b strings.Builder
	for k := 1; k < len(s); k++ {
		if s[k] != '\'' {
			b.WriteByte(s[k])
			continue
		}
		if k+1 < len(s) && s[k+1] == '\'' {
			b.WriteByte('\'')
			k++
			continue
		}
		return b.String(), s[k+1:], nil
	}
	return "", "", fail(lineNo, "unterminated quoted name")
}

// expression reads an expression's body: the text after `=` on the
// declaration line, then the lines at least two levels deeper, or the lines
// between fences.
func (p *parser) expression(n *Node, inline string, depth int) error {
	n.Sep = Equals
	inline = strings.TrimSpace(inline)
	if strings.HasPrefix(inline, fence) {
		return p.fenced(n, inline[len(fence):])
	}
	var body []string
	first := 0
	if inline != "" {
		body = append(body, inline)
		first = n.Line
	}
	var cont []string
	contLine := 0
	for j := p.i; j < len(p.lines); j++ {
		line := p.lines[j]
		if blank(line) {
			continue
		}
		d, _, _ := indent(line)
		if d < depth+2 {
			break
		}
		// Take this line and the blank lines before it.
		if contLine == 0 {
			contLine = p.i + 1
		}
		cont = append(cont, p.lines[p.i:j+1]...)
		p.i = j + 1
	}
	if first == 0 {
		first = contLine
	}
	body = append(body, dedent(cont)...)
	n.Value = strings.Join(body, "\n")
	n.ValueLine = first
	if n.ValueLine == 0 {
		n.ValueLine = n.Line
	}
	return nil
}

// fenced reads a body between fences. Inside them, indentation is not
// structure.
func (p *parser) fenced(n *Node, after string) error {
	if strings.HasSuffix(after, fence) {
		n.Value = strings.TrimSpace(strings.TrimSuffix(after, fence))
		n.ValueLine = n.Line
		return nil
	}
	var body []string
	first := n.Line + 1
	if strings.TrimSpace(after) != "" {
		body = append(body, strings.TrimSpace(after))
		first = n.Line
	}
	start := p.i
	for j := p.i; j < len(p.lines); j++ {
		if strings.TrimSpace(p.lines[j]) == fence {
			body = append(body, dedent(p.lines[start:j])...)
			p.i = j + 1
			n.Value = strings.Join(body, "\n")
			n.ValueLine = first
			return nil
		}
	}
	return fail(n.Line, "unterminated fenced expression")
}

// dedent removes the leading tabs common to the non-blank lines, so the
// body keeps its own indentation, and empties the blank lines.
func dedent(lines []string) []string {
	minTabs := -1
	for _, l := range lines {
		if blank(l) {
			continue
		}
		d := 0
		for d < len(l) && l[d] == '\t' {
			d++
		}
		if minTabs < 0 || d < minTabs {
			minTabs = d
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if blank(l) {
			continue
		}
		out[i] = l[minTabs:]
	}
	return out
}
