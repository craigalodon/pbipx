// Package exprlex lexes the code a model or a SQL file holds (DAX, M and
// SQL) into tokens, and decides which columns each literal in it is
// associated with (docs/design/behavior.md, "Association in code" and "Free
// text inside code").
//
// It works on plain strings and never prints one. A LexError carries a line
// and a structure, never source text (principle 2).
package exprlex

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind is a token's kind.
type Kind int

// Token kinds.
const (
	Str   Kind = iota // a string literal; Text is its value, unescaped
	Num               // a number literal, without any sign
	Col               // a column reference; Text is the column, Table its table when the syntax gives one
	Ident             // a name: a function, table, keyword, step or type name
	Op                // an operator such as =, <>, & or ||
	Open              // ( { or, in M, the [ of a record
	Close             // ) } or, in M, the ] of a record
	Comma             // ,
)

var kindNames = [...]string{"Str", "Num", "Col", "Ident", "Op", "Open", "Close", "Comma"}

func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Token is one lexed token.
type Token struct {
	Kind  Kind
	Text  string
	Table string // for Col, when the syntax gives one
	Line  int    // 1-based within src
	// Parts is every part of a SQL name as written, unquoted (a.b.c), for a
	// Col or a function's Ident; nil for any other token.
	Parts []string
}

// Comment is one comment, with its markers and the whitespace around its
// text removed, so the name shape can anchor.
type Comment struct {
	Text string
	Line int // 1-based line of the comment's start
	// Extra is set on a comment of SQL found only under another reading of
	// the source than the first one that lexes (SQLSites): a # comment, or
	// one that a backslash escape moves out of a string.
	Extra bool
	off   int // byte offset of the comment's marker
}

// LexError is code that could not be lexed. Its message is structure only.
type LexError struct {
	Line int
	Msg  string
}

func (e *LexError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// Lex error messages.
const (
	msgString       = "unterminated string"
	msgName         = "unterminated quoted name"
	msgColumn       = "unterminated column reference"
	msgBlockComment = "unterminated block comment"
	msgDollar       = "unterminated dollar-quoted string"
)

// lexer is the state the three languages share.
type lexer struct {
	src     string
	pos     int
	line    int
	start   int // byte offset where the token being read starts
	toks    []Token
	dq      []bool // parallel to toks: a Str read from a double-quoted run (SQL)
	offs    []int  // parallel to toks: the byte offset where each starts
	coms    []Comment
	lastEnd int // byte offset just after the last token
	// spans are the byte ranges [start, end) of every comment and every
	// quoted run (a string or a name), for SQLCode.
	spans [][2]int
	// SQL readings (SQLSites): bs reads a backslash in a quoted run as an
	// escape; hash reads # as the start of a line comment.
	bs, hash bool
	// bodies are the tags of the dollar-quoted bodies open under the body
	// reading, innermost last.
	bodies []string
}

func newLexer(src string) *lexer { return &lexer{src: src, line: 1, lastEnd: -1} }

func (l *lexer) emit(k Kind, text, table string, line int) {
	if len(l.toks) == cap(l.toks) {
		// Double, where append grows a large slice by a quarter: a long
		// expression would otherwise copy its tokens several times over.
		l.toks = slices.Grow(l.toks, len(l.toks)+16)
	}
	l.toks = append(l.toks, Token{Kind: k, Text: text, Table: table, Line: line})
	l.dq = append(l.dq, false)
	l.offs = append(l.offs, l.start)
	l.lastEnd = l.pos
}

func (l *lexer) at(off int) byte {
	if i := l.pos + off; i < len(l.src) {
		return l.src[i]
	}
	return 0
}

func (l *lexer) has(prefix string) bool { return strings.HasPrefix(l.src[l.pos:], prefix) }

// advance moves n bytes, counting lines.
func (l *lexer) advance(n int) {
	end := min(l.pos+n, len(l.src))
	l.line += strings.Count(l.src[l.pos:end], "\n")
	l.pos = end
}

func (l *lexer) rune() (rune, int) { return utf8.DecodeRuneInString(l.src[l.pos:]) }

func isSpace(r rune) bool { return unicode.IsSpace(r) }

func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// lineComment reads a comment from a marker of n bytes to the end of the
// line.
func (l *lexer) lineComment(n int) {
	line := l.line
	rest := l.src[l.pos+n:]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	l.coms = append(l.coms, Comment{Text: strings.TrimSpace(rest[:end]), Line: line, off: l.pos})
	l.spans = append(l.spans, [2]int{l.pos, l.pos + n + end})
	l.advance(n + end)
}

// blockComment reads a /* */ comment.
func (l *lexer) blockComment() error {
	line := l.line
	end := strings.Index(l.src[l.pos+2:], "*/")
	if end < 0 {
		return &LexError{Line: line, Msg: msgBlockComment}
	}
	l.coms = append(l.coms, Comment{Text: strings.TrimSpace(l.src[l.pos+2 : l.pos+2+end]), Line: line, off: l.pos})
	l.spans = append(l.spans, [2]int{l.pos, l.pos + 2 + end + 2})
	l.advance(2 + end + 2)
	return nil
}

// delimited reads a run from the opening byte at pos to the closing byte, a
// doubled closing byte standing for itself. It returns the content,
// unescaped.
//
// Under the backslash reading (l.bs), a backslash in a '...' or "..." run
// escapes the byte after it: \', \" and \\ stand for the quote or the
// backslash, and any other pair is kept as written, so a pattern such as
// '\d+' has the same value under both readings. A backticked run has no
// escapes.
func (l *lexer) delimited(closer byte, msg string) (string, error) {
	line := l.line
	start := l.pos
	stops := string(closer)
	if l.bs && closer != '`' {
		stops += `\`
	}
	var b strings.Builder
	i := l.pos + 1
	for {
		j := strings.IndexAny(l.src[i:], stops)
		if j < 0 {
			return "", &LexError{Line: line, Msg: msg}
		}
		b.WriteString(l.src[i : i+j])
		i += j
		if l.src[i] == '\\' {
			if i+1 >= len(l.src) {
				return "", &LexError{Line: line, Msg: msg}
			}
			switch c := l.src[i+1]; c {
			case '\'', '"', '\\':
				b.WriteByte(c)
			default:
				b.WriteString(l.src[i : i+2])
			}
			i += 2
			continue
		}
		if i+1 < len(l.src) && l.src[i+1] == closer {
			b.WriteByte(closer)
			i += 2
			continue
		}
		l.advance(i + 1 - l.pos)
		l.spans = append(l.spans, [2]int{start, l.pos})
		return b.String(), nil
	}
}

func isASCIILetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// dollarTag returns the length of a dollar quote's tag ($$ or $name$) at
// pos, or 0. A $ right after a name character belongs to that name, as in
// PostgreSQL, so a$b$ opens nothing; nor does $1, a parameter.
func (l *lexer) dollarTag() int {
	if l.at(0) != '$' {
		return 0
	}
	if l.pos > 0 {
		if c := l.src[l.pos-1]; c == '$' || c == '_' || c >= 0x80 || isDigit(c) || isASCIILetter(c) {
			return 0
		}
	}
	for i := l.pos + 1; i < len(l.src); i++ {
		c := l.src[i]
		switch {
		case c == '$':
			return i + 1 - l.pos
		case c == '_' || isASCIILetter(c) || (isDigit(c) && i > l.pos+1):
		default:
			return 0
		}
	}
	return 0
}

// dollarString reads a dollar-quoted string ($$...$$ or $tag$...$tag$)
// whose tag of n bytes is at pos. Its content is taken as written.
func (l *lexer) dollarString(n int) (string, error) {
	line := l.line
	start := l.pos
	tag := l.src[l.pos : l.pos+n]
	end := strings.Index(l.src[l.pos+n:], tag)
	if end < 0 {
		return "", &LexError{Line: line, Msg: msgDollar}
	}
	s := l.src[l.pos+n : l.pos+n+end]
	l.advance(n + end + n)
	l.spans = append(l.spans, [2]int{start, l.pos})
	return s, nil
}

// number reads an integer, a decimal or a number with an exponent, if one
// starts at pos.
func (l *lexer) number() bool {
	c := l.at(0)
	if !isDigit(c) && (c != '.' || !isDigit(l.at(1))) {
		return false
	}
	line := l.line
	i := l.pos
	digits := func() {
		for i < len(l.src) && isDigit(l.src[i]) {
			i++
		}
	}
	digits()
	if i+1 < len(l.src) && l.src[i] == '.' && isDigit(l.src[i+1]) {
		i++
		digits()
	} else if i < len(l.src) && l.src[i] == '.' && i > l.pos {
		i++ // a trailing point: 1.
	}
	if i < len(l.src) && (l.src[i] == 'e' || l.src[i] == 'E') {
		k := i + 1
		if k < len(l.src) && (l.src[k] == '+' || l.src[k] == '-') {
			k++
		}
		if k < len(l.src) && isDigit(l.src[k]) {
			i = k
			digits()
		}
	}
	text := l.src[l.pos:i]
	l.advance(i - l.pos)
	l.emit(Num, text, "", line)
	return true
}

// word reads an identifier at pos; dots join parts when dotted is set (DAX
// and M names such as Text.Contains).
func (l *lexer) word(dotted bool) string {
	start := l.pos
	for l.pos < len(l.src) {
		r, n := l.rune()
		if isIdentPart(r) {
			l.pos += n
			continue
		}
		if dotted && r == '.' {
			if next, _ := utf8.DecodeRuneInString(l.src[l.pos+1:]); isIdentStart(next) {
				l.pos++
				continue
			}
		}
		break
	}
	return l.src[start:l.pos]
}

var ops = []string{"==", "<>", "!=", "<=", ">=", "&&", "||", "=>", "=", "<", ">", "&", "+", "-", "*", "/", "^", "%"}

// op reads an operator, if one starts at pos.
func (l *lexer) op() bool {
	for _, o := range ops {
		if l.has(o) {
			line := l.line
			l.advance(len(o))
			l.emit(Op, o, "", line)
			return true
		}
	}
	return false
}

// punct reads a comma or a parenthesis or brace.
func (l *lexer) punct() bool {
	var k Kind
	switch l.at(0) {
	case ',':
		k = Comma
	case '(', '{':
		k = Open
	case ')', '}':
		k = Close
	default:
		return false
	}
	line := l.line
	text := l.src[l.pos : l.pos+1]
	l.advance(1)
	l.emit(k, text, "", line)
	return true
}

// skip passes over whitespace or one unknown character.
func (l *lexer) skip() {
	_, n := l.rune()
	l.advance(n)
}

// DAX lexes a DAX expression.
func DAX(src string) ([]Token, []Comment, error) {
	l := newLexer(src)
	for l.pos < len(src) {
		l.start = l.pos
		r, _ := l.rune()
		var err error
		switch {
		case isSpace(r):
			l.skip()
		case l.has("//"), l.has("--"):
			l.lineComment(2)
		case l.has("/*"):
			err = l.blockComment()
		case r == '"':
			line := l.line
			var s string
			if s, err = l.delimited('"', msgString); err == nil {
				l.emit(Str, s, "", line)
			}
		case r == '\'':
			line := l.line
			var s string
			if s, err = l.delimited('\'', msgName); err == nil {
				l.emit(Ident, s, "", line)
			}
		case r == '[':
			err = l.daxColumn()
		case l.number():
		case isIdentStart(r):
			line := l.line
			w := l.word(true)
			l.emit(Ident, w, "", line)
		case l.punct(), l.op():
		default:
			l.skip()
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return l.toks, l.coms, nil
}

// daxColumn reads [col], taking its table from a name written right before
// it.
func (l *lexer) daxColumn() error {
	line := l.line
	start := l.pos
	name, err := l.delimited(']', msgColumn)
	if err != nil {
		return err
	}
	if n := len(l.toks); n > 0 && l.toks[n-1].Kind == Ident && l.lastEnd == start {
		prev := l.toks[n-1]
		l.toks[n-1] = Token{Kind: Col, Text: name, Table: prev.Text, Line: prev.Line}
		l.lastEnd = l.pos
		return nil
	}
	l.emit(Col, name, "", line)
	return nil
}

// M lexes a Power Query M expression.
func M(src string) ([]Token, []Comment, error) {
	l := newLexer(src)
	for l.pos < len(src) {
		l.start = l.pos
		r, _ := l.rune()
		var err error
		switch {
		case isSpace(r):
			l.skip()
		case l.has("//"):
			l.lineComment(2)
		case l.has("/*"):
			err = l.blockComment()
		case r == '"':
			line := l.line
			var s string
			if s, err = l.delimited('"', msgString); err == nil {
				l.emit(Str, s, "", line)
			}
		case l.has(`#"`):
			line := l.line
			l.advance(1)
			var s string
			if s, err = l.delimited('"', msgName); err == nil {
				l.emit(Ident, s, "", line)
			}
		case r == '#' && l.pos+1 < len(src) && isIdentStart(firstRune(src[l.pos+1:])):
			line := l.line
			l.advance(1)
			w := l.word(true)
			l.emit(Ident, "#"+w, "", line)
		case r == '[':
			err = l.mBracket()
		case r == ']':
			line := l.line
			l.advance(1)
			l.emit(Close, "]", "", line)
		case l.number():
		case isIdentStart(r):
			line := l.line
			w := l.word(true)
			l.emit(Ident, w, "", line)
		case l.punct(), l.op():
		default:
			l.skip()
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return l.toks, l.coms, nil
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// mBracket reads [field], [#"a b"] or [field]? as a column access, and
// otherwise opens a record (or a field projection) whose contents are lexed
// as code.
func (l *lexer) mBracket() error {
	line := l.line
	i := l.pos + 1
	for i < len(l.src) && (l.src[i] == ' ' || l.src[i] == '\t') {
		i++
	}
	if strings.HasPrefix(l.src[i:], `#"`) {
		save, saveLine := l.pos, l.line
		l.advance(i + 1 - l.pos)
		name, err := l.delimited('"', msgName)
		if err != nil {
			return err
		}
		j := l.pos
		for j < len(l.src) && (l.src[j] == ' ' || l.src[j] == '\t') {
			j++
		}
		if j < len(l.src) && l.src[j] == ']' {
			l.advance(j + 1 - l.pos)
			l.optional()
			l.emit(Col, name, "", line)
			return nil
		}
		l.pos, l.line = save, saveLine
		l.advance(1)
		l.emit(Open, "[", "", line)
		return nil
	}
	for j := i; j < len(l.src); j++ {
		switch l.src[j] {
		case ']':
			name := strings.TrimSpace(l.src[l.pos+1 : j])
			if name == "" {
				l.advance(1)
				l.emit(Open, "[", "", line)
				return nil
			}
			l.advance(j + 1 - l.pos)
			l.optional()
			l.emit(Col, name, "", line)
			return nil
		case '=', ',', '[', '(', '{', '"', '\'', '\n':
			l.advance(1)
			l.emit(Open, "[", "", line)
			return nil
		}
	}
	return &LexError{Line: line, Msg: msgColumn}
}

// optional passes the ? of an optional field access.
func (l *lexer) optional() {
	if l.at(0) == '?' {
		l.advance(1)
	}
}

// DoubleQuote is how SQL reads a "..." run.
type DoubleQuote int

const (
	// QuoteSkip skips the quotes and lexes the words inside as code: SQL
	// embedded in an M string, where the quotes are M's.
	QuoteSkip DoubleQuote = iota
	// QuoteString reads "..." as a string.
	QuoteString
	// QuoteIdent reads "..." as a quoted identifier.
	QuoteIdent
)

// sqlKeywords are words that are never columns.
var sqlKeywords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		select from where and or not in is null like ilike rlike regexp similar
		between as on join inner left right full outer cross natural using
		group by order having limit offset union all distinct case when then
		else end exists insert into values update set delete create table view
		replace with recursive asc desc true false top fetch only over partition
		window qualify lateral intersect except minus cast interval escape
		collate any some returning if begin commit rollback transaction declare
		primary references default unique check constraint alter drop add
		materialized temporary temp nulls filter within go use exec execute
		merge matched truncate grant revoke`) {
		sqlKeywords[w] = true
	}
}

// IsSQLKeyword reports whether a bare word is an SQL keyword, which is never
// a column.
func IsSQLKeyword(w string) bool { return sqlKeywords[strings.ToLower(w)] }

// SQL lexes SQL, reading "..." as dq says, under the first reading: a quote
// in a string is escaped only by doubling it, and # is not a comment.
// Dollar-quoted strings ($$...$$, $tag$...$tag$) are strings.
func SQL(src string, dq DoubleQuote) ([]Token, []Comment, error) {
	l, err := lexSQL(src, dq, reading{})
	if err != nil {
		return nil, nil, err
	}
	return l.toks, l.coms, nil
}

// SQLFirstReading lexes SQL, reading "..." as dq says, under the first of
// SQLSites's readings that lexes. Its error is the first reading's, when
// none does.
func SQLFirstReading(src string, dq DoubleQuote) ([]Token, []Comment, error) {
	var first error
	for _, rd := range readingsOf(src) {
		l, err := lexSQL(src, dq, rd)
		if err == nil {
			return l.toks, l.coms, nil
		}
		if first == nil {
			first = err
		}
	}
	return nil, nil, first
}

// reading is one way of lexing SQL that dialects disagree on (behavior.md,
// "SQL files"). The zero reading is standard SQL.
type reading struct {
	bs   bool // a backslash escapes the next byte in '...' and "..." (MySQL, BigQuery, Spark)
	hash bool // # starts a line comment (MySQL, BigQuery)
	// body lexes a dollar-quoted string's content as SQL, its tags passed
	// over, as a function body is: a string in it is a literal of its own.
	// An unterminated dollar quote is still an error.
	body bool
}

// readingsOf lists the readings src is lexed under, the zero reading first.
// A reading that cannot differ from one before it, because src has no
// backslash, no # or no $, is left out.
func readingsOf(src string) []reading {
	out := []reading{{}}
	if strings.Contains(src, `\`) {
		out = append(out, reading{bs: true})
	}
	if strings.Contains(src, "#") {
		for _, rd := range out {
			rd.hash = true
			out = append(out, rd)
		}
	}
	if strings.Contains(src, "$") {
		for _, rd := range out {
			rd.body = true
			out = append(out, rd)
		}
	}
	return out
}

func lexSQL(src string, dq DoubleQuote, rd reading) (*lexer, error) {
	l := newLexer(src)
	l.bs, l.hash = rd.bs, rd.hash
	for l.pos < len(src) {
		l.start = l.pos
		r, _ := l.rune()
		var err error
		switch {
		case isSpace(r):
			l.skip()
		case l.has("--"):
			l.lineComment(2)
		case l.hash && r == '#':
			l.lineComment(1)
		case l.has("/*"):
			err = l.blockComment()
		case len(l.bodies) > 0 && l.has(l.bodies[len(l.bodies)-1]):
			l.advance(len(l.bodies[len(l.bodies)-1]))
			l.bodies = l.bodies[:len(l.bodies)-1]
		case r == '$' && l.dollarTag() > 0 && rd.body:
			tag := l.src[l.pos : l.pos+l.dollarTag()]
			if !strings.Contains(l.src[l.pos+len(tag):], tag) {
				err = &LexError{Line: l.line, Msg: msgDollar}
				break
			}
			l.bodies = append(l.bodies, tag)
			l.advance(len(tag))
		case r == '$' && l.dollarTag() > 0:
			line := l.line
			var s string
			if s, err = l.dollarString(l.dollarTag()); err == nil {
				l.emit(Str, s, "", line)
			}
		case r == '\'':
			line := l.line
			var s string
			if s, err = l.delimited('\'', msgString); err == nil {
				l.emit(Str, s, "", line)
			}
		case r == '"' && dq == QuoteSkip:
			l.advance(1)
		case r == '"' && dq == QuoteString:
			line := l.line
			var s string
			if s, err = l.delimited('"', msgString); err == nil {
				l.emit(Str, s, "", line)
				l.dq[len(l.dq)-1] = true
			}
		case r == '`', r == '"', isIdentStart(r):
			err = l.sqlName(dq)
		case l.number():
		case l.punct(), l.op():
		default:
			l.skip()
		}
		if err != nil {
			return nil, err
		}
	}
	if len(l.bodies) > 0 {
		return nil, &LexError{Line: l.line, Msg: msgDollar}
	}
	return l, nil
}

// sqlName reads a name, possibly qualified (a.b.c). A run followed by ( is a
// function; a single bare keyword is a keyword; anything else is a column,
// its last part the column and the part before it the table.
func (l *lexer) sqlName(dq DoubleQuote) error {
	line := l.line
	var parts []string
	bare := true
	for {
		var part string
		switch c := l.at(0); {
		case c == '`':
			s, err := l.delimited('`', msgName)
			if err != nil {
				return err
			}
			part, bare = s, false
		case c == '"' && dq == QuoteIdent:
			s, err := l.delimited('"', msgName)
			if err != nil {
				return err
			}
			part, bare = s, false
		default:
			part = l.word(false)
		}
		parts = append(parts, part)
		if l.at(0) != '.' {
			break
		}
		next := l.at(1)
		if next == '`' || (next == '"' && dq == QuoteIdent) || isIdentStart(firstRune(l.src[l.pos+1:])) {
			l.advance(1)
			continue
		}
		break
	}
	if len(parts) > 1 {
		bare = false
	}
	j := l.pos
	for j < len(l.src) && strings.IndexByte(" \t\r\n", l.src[j]) >= 0 {
		j++
	}
	last := parts[len(parts)-1]
	switch {
	case j < len(l.src) && l.src[j] == '(':
		l.emit(Ident, strings.Join(parts, "."), "", line)
	case bare && IsSQLKeyword(last):
		l.emit(Ident, last, "", line)
		return nil
	default:
		table := ""
		if len(parts) > 1 {
			table = parts[len(parts)-2]
		}
		l.emit(Col, last, table, line)
	}
	l.toks[len(l.toks)-1].Parts = parts
	return nil
}
