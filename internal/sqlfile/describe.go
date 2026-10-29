package sqlfile

import (
	"regexp"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/exprlex"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/shape"
)

// Description is the structure of one SQL file, as explain prints it
// (docs/design/explain.md): the relations it creates, writes and reads, and
// a count of its literals by context. It holds names and counts only, never
// a value.
type Description struct {
	Creates []Created `json:"creates"`
	Writes  []string  `json:"writes"`
	// Reads are qualified relations and template ref() and source() calls.
	Reads []string `json:"reads"`
	// ReadsUnqualified are one-part relations: a CTE, or a table named
	// without its schema.
	ReadsUnqualified []string       `json:"reads_unqualified"`
	Literals         map[string]int `json:"literals"`
}

// Created is a relation a statement creates.
type Created struct {
	Kind string `json:"kind"` // view or table
	Name string `json:"name"`
}

// Describe describes one SQL file. A file that does not lex returns the
// unlexable-sql *judge.ParseError, as Scan does.
func Describe(data []byte) (*Description, error) {
	src := string(data)
	vals, err := Values(src)
	if err != nil {
		return nil, ParseError(err)
	}
	blanked, _, err := blankTemplate(src)
	if err != nil {
		return nil, ParseError(err)
	}
	rels, err := relationsOf(blanked)
	if err != nil {
		return nil, ParseError(err)
	}
	calls, err := templateReads(src, blanked)
	if err != nil {
		return nil, ParseError(err)
	}
	d := &Description{
		Creates:          append([]Created{}, rels.creates...),
		Writes:           sortedSet(rels.writes),
		Reads:            sortedSet(append(rels.qualified, calls...)),
		ReadsUnqualified: sortedSet(append(rels.unqualified, rels.cteReads...)),
		Literals:         map[string]int{},
	}
	slices.SortFunc(d.Creates, func(a, b Created) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Kind, b.Kind)
	})
	d.Creates = slices.Compact(d.Creates)
	for _, v := range vals {
		if !v.Comment {
			d.Literals[string(finding.SQL)]++
		}
	}
	return d, nil
}

// NativeReads is the relations a query reads, CTEs left out: the
// relations SQL embedded in a model's M string reads. Its error is a
// *exprlex.LexError.
func NativeReads(src string) ([]string, error) {
	rels, err := relationsOf(src)
	if err != nil {
		return nil, err
	}
	return sortedSet(append(rels.qualified, rels.unqualified...)), nil
}

// relations is what one SQL text names, each name printable.
type relations struct {
	creates     []Created
	writes      []string
	qualified   []string // reads of a name with two parts or more
	unqualified []string // reads of a one-part name that is not a CTE
	cteReads    []string // reads of a CTE
}

// fromFunctions are the functions whose FROM is not a relation:
// EXTRACT(year FROM d), TRIM(x FROM s), SUBSTRING(s FROM 1 FOR 2), ...
var fromFunctions = map[string]bool{"extract": true, "trim": true, "substring": true, "substr": true, "overlay": true}

// createModifiers are the words between CREATE and VIEW or TABLE.
var createModifiers = map[string]bool{
	"or": true, "replace": true, "temp": true, "temporary": true, "materialized": true,
	"global": true, "local": true, "transient": true, "external": true, "volatile": true,
	"secure": true, "recursive": true, "unlogged": true, "streaming": true, "live": true,
}

// statementWords start a statement, and end what USING means after MERGE.
var statementWords = map[string]bool{
	"select": true, "insert": true, "update": true, "delete": true, "merge": true,
	"create": true, "with": true, "alter": true, "drop": true,
}

// relationsOf reads the relations of SQL text whose template delimiters are
// already blanked, under the first reading that lexes. "..." is a quoted
// identifier.
func relationsOf(src string) (*relations, error) {
	toks, _, err := exprlex.SQLFirstReading(src, exprlex.QuoteIdent)
	if err != nil {
		return nil, err
	}
	r := &relScan{toks: toks, ctes: cteNames(toks), used: map[int]bool{}, out: &relations{}}
	r.run()
	return r.out, nil
}

type relScan struct {
	toks []exprlex.Token
	ctes map[string]bool
	used map[int]bool // name tokens already taken as a created or written relation
	out  *relations
}

// word is a token's lowercased text when it is one bare word: a keyword or
// a one-part name, followed by a parenthesis or not. Anything else is "".
func (r *relScan) word(i int) string {
	if i < 0 || i >= len(r.toks) {
		return ""
	}
	t := r.toks[i]
	if (t.Kind == exprlex.Ident && len(t.Parts) <= 1) || (t.Kind == exprlex.Col && len(t.Parts) == 1) {
		return strings.ToLower(t.Text)
	}
	return ""
}

// isName reports whether token i is a relation name. A name followed by a
// parenthesis reads as a call; it is a name only where a column list may
// follow one (CREATE TABLE t (...), INSERT INTO t (...), a CTE's columns).
func (r *relScan) isName(i int, listMayFollow bool) bool {
	if i < 0 || i >= len(r.toks) || len(r.toks[i].Parts) == 0 {
		return false
	}
	switch r.toks[i].Kind {
	case exprlex.Col:
		return true
	case exprlex.Ident:
		return listMayFollow
	}
	return false
}

func (r *relScan) isOpen(i int) bool {
	return i < len(r.toks) && r.toks[i].Kind == exprlex.Open && r.toks[i].Text == "("
}

// name is token i's relation name, each part printable.
func (r *relScan) name(i int) string {
	parts := r.toks[i].Parts
	out := make([]string, len(parts))
	for k, p := range parts {
		out[k] = shape.IdentifierPart(p)
	}
	return strings.Join(out, ".")
}

func (r *relScan) run() {
	var parens []string // the function each open parenthesis belongs to; "" for none
	stmt := ""
	for i, t := range r.toks {
		switch {
		case t.Kind == exprlex.Open && t.Text == "(":
			fn := ""
			if i > 0 && r.toks[i-1].Kind == exprlex.Ident && len(r.toks[i-1].Parts) > 0 {
				parts := r.toks[i-1].Parts
				fn = strings.ToLower(parts[len(parts)-1])
			}
			parens = append(parens, fn)
			continue
		case t.Kind == exprlex.Close && t.Text == ")":
			if len(parens) > 0 {
				parens = parens[:len(parens)-1]
			}
			continue
		}
		if t.Kind != exprlex.Ident || len(t.Parts) > 1 {
			continue // only a keyword starts a clause
		}
		w := r.word(i)
		if statementWords[w] {
			stmt = w
		}
		switch w {
		case "create":
			r.create(i + 1)
		case "insert":
			j := i + 1
			for r.word(j) == "into" || r.word(j) == "overwrite" || r.word(j) == "table" {
				j++
			}
			r.write(j)
		case "merge":
			j := i + 1
			if r.word(j) == "into" {
				j++
			}
			r.write(j)
		case "using":
			if stmt == "merge" && r.isName(i+1, false) && !r.used[i+1] {
				r.read(i + 1)
			}
		case "from":
			switch {
			case len(parens) > 0 && fromFunctions[parens[len(parens)-1]]:
			case r.word(i-1) == "distinct" && (r.word(i-2) == "is" || (r.word(i-2) == "not" && r.word(i-3) == "is")):
			case stmt == "delete" && r.word(i-1) == "delete":
				r.write(i + 1)
			default:
				r.list(i+1, true)
			}
		case "join":
			r.list(i+1, false)
		}
	}
}

// create takes CREATE [modifiers] VIEW|TABLE [IF NOT EXISTS] name, from the
// token after CREATE.
func (r *relScan) create(j int) {
	for createModifiers[r.word(j)] {
		j++
	}
	kind := r.word(j)
	if kind != "view" && kind != "table" {
		return // CREATE SCHEMA, FUNCTION, INDEX, ...: not a relation
	}
	j++
	if r.word(j) == "if" {
		j++
		for r.word(j) == "not" || r.word(j) == "exists" {
			j++
		}
	}
	if r.isName(j, true) {
		r.used[j] = true
		r.out.creates = append(r.out.creates, Created{Kind: kind, Name: r.name(j)})
	}
}

func (r *relScan) write(j int) {
	if r.isName(j, true) {
		r.used[j] = true
		r.out.writes = append(r.out.writes, r.name(j))
	}
}

func (r *relScan) read(j int) {
	t := r.toks[j]
	n := r.name(j)
	switch {
	case len(t.Parts) > 1:
		r.out.qualified = append(r.out.qualified, n)
	case r.ctes[strings.ToLower(t.Parts[0])]:
		r.out.cteReads = append(r.out.cteReads, n)
	default:
		r.out.unqualified = append(r.out.unqualified, n)
	}
}

// list reads the relations after FROM or JOIN: a name or a subquery, an
// optional alias, and, after FROM, more of them after commas.
func (r *relScan) list(j int, commas bool) {
	for j < len(r.toks) {
		switch {
		case r.isName(j, false):
			if !r.used[j] {
				r.read(j)
			}
			j++
		case r.isOpen(j), r.isOpen(j+1) && r.toks[j].Kind == exprlex.Ident && len(r.toks[j].Parts) > 0:
			// A subquery or a table function: its own FROMs are read where
			// they stand.
			if !r.isOpen(j) {
				j++
			}
			j = r.skipParens(j)
		default:
			return
		}
		if r.word(j) == "as" {
			j++
		}
		if r.isName(j, false) && len(r.toks[j].Parts) == 1 {
			j++ // an alias
		}
		if !commas || j >= len(r.toks) || r.toks[j].Kind != exprlex.Comma {
			return
		}
		j++
	}
}

// skipParens returns the index after the parenthesis that closes the one
// at open.
func (r *relScan) skipParens(open int) int {
	depth := 0
	for j := open; j < len(r.toks); j++ {
		switch t := r.toks[j]; {
		case t.Kind == exprlex.Open && t.Text == "(":
			depth++
		case t.Kind == exprlex.Close && t.Text == ")":
			depth--
			if depth == 0 {
				return j + 1
			}
		}
	}
	return len(r.toks)
}

// cteNames are the names WITH defines: name [(columns)] AS ( ... ), each
// lowercased.
func cteNames(toks []exprlex.Token) map[string]bool {
	r := &relScan{toks: toks}
	out := map[string]bool{}
	for i := range toks {
		if toks[i].Kind != exprlex.Ident || r.word(i) != "with" {
			continue
		}
		j := i + 1
		if r.word(j) == "recursive" {
			j++
		}
		for r.isName(j, true) && len(toks[j].Parts) == 1 {
			name := strings.ToLower(toks[j].Parts[0])
			j++
			if r.isOpen(j) {
				j = r.skipParens(j) // a column list
			}
			if r.word(j) != "as" {
				break
			}
			j++
			for r.word(j) == "not" || r.word(j) == "materialized" {
				j++
			}
			if !r.isOpen(j) {
				break
			}
			out[name] = true
			j = r.skipParens(j)
			if j >= len(toks) || toks[j].Kind != exprlex.Comma {
				break
			}
			j++
		}
	}
	return out
}

// templateCall finds a template's ref( or source(; templateReads decides
// whether it stands in code.
var templateCall = regexp.MustCompile(`\b(?:ref|source)\(`)

// Where a byte of a templated SQL text stands (templateWhere).
const (
	inSQL      byte = iota // outside every template construct
	inTemplate             // code inside a {{ }} or {% %} block
	inOther                // a template delimiter, comment or string
)

// templateWhere tells, for each byte of src, whether it is outside every
// template construct, code inside a template block, or neither. A block's
// strings run to their closing quote or the block's end, as blankTemplate
// closes a block at its first closer.
func templateWhere(src string) []byte {
	where := make([]byte, len(src))
	other := func(from, to int) {
		for k := from; k < to && k < len(src); k++ {
			where[k] = inOther
		}
	}
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "{#"):
			end := strings.Index(src[i+2:], "#}")
			if end < 0 {
				other(i, len(src))
				return where
			}
			other(i, i+2+end+2)
			i += 2 + end + 2
		case strings.HasPrefix(src[i:], "{{"), strings.HasPrefix(src[i:], "{%"):
			closer := "}}"
			if src[i+1] == '%' {
				closer = "%}"
			}
			other(i, i+2)
			i += 2
			var quote byte
			for i < len(src) && !strings.HasPrefix(src[i:], closer) {
				c := src[i]
				switch {
				case quote != 0:
					where[i] = inOther
					if c == '\\' && i+1 < len(src) {
						i++
						where[i] = inOther
					} else if c == quote {
						quote = 0
					}
				case c == '\'' || c == '"':
					quote = c
					where[i] = inOther
				default:
					where[i] = inTemplate
				}
				i++
			}
			other(i, i+2)
			i += 2
		default:
			i++
		}
	}
	return where
}

// templateReads is the relations the text's ref() and source() calls read:
// the quoted names joined with dots, each part printable, or Dynamic for a
// call whose arguments are not all quoted names. A call counts where it is
// code: inside a template block, wherever the block stands (dbt reads
// -- depends_on: {{ ref('x') }}), or in the SQL outside every comment and
// string. A call in comment prose or in a string is not a read. blanked is
// src with its template delimiters blanked.
func templateReads(src, blanked string) ([]string, error) {
	where := templateWhere(src)
	code, err := exprlex.SQLCode(blanked)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, loc := range templateCall.FindAllStringIndex(src, -1) {
		switch i := loc[0]; where[i] {
		case inTemplate:
		case inSQL:
			if !code[i] {
				continue
			}
		default:
			continue
		}
		parts, ok := quotedArgs(src[loc[1]:])
		if !ok || len(parts) == 0 {
			out = append(out, shape.Dynamic)
			continue
		}
		for k, p := range parts {
			parts[k] = shape.IdentifierPart(p)
		}
		out = append(out, strings.Join(parts, "."))
	}
	return out, nil
}

// quotedArgs reads a call's arguments, from just after its parenthesis,
// when every one is a quoted name: 'a' or "a", separated by commas.
func quotedArgs(s string) ([]string, bool) {
	var args []string
	i := 0
	skip := func() {
		for i < len(s) && strings.IndexByte(" \t\r\n", s[i]) >= 0 {
			i++
		}
	}
	for {
		skip()
		if i < len(s) && s[i] == ')' && len(args) == 0 {
			return nil, true
		}
		if i >= len(s) || (s[i] != '\'' && s[i] != '"') {
			return nil, false
		}
		end := strings.IndexByte(s[i+1:], s[i])
		if end < 0 {
			return nil, false
		}
		args = append(args, s[i+1:i+1+end])
		i += end + 2
		skip()
		switch {
		case i < len(s) && s[i] == ')':
			return args, true
		case i < len(s) && s[i] == ',':
			i++
		default:
			return nil, false
		}
	}
}

// sortedSet is names sorted, each once, and never nil.
func sortedSet(names []string) []string {
	out := slices.Clone(names)
	if out == nil {
		out = []string{}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
