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
)

// lexer is the state the three languages share.
type lexer struct {
	src     string
	pos     int
	line    int
	toks    []Token
	dq      []bool // parallel to toks: a Str read from a double-quoted run (SQL)
	coms    []Comment
	lastEnd int // byte offset just after the last token
}

func newLexer(src string) *lexer { return &lexer{src: src, line: 1, lastEnd: -1} }

func (l *lexer) emit(k Kind, text, table string, line int) {
	l.toks = append(l.toks, Token{Kind: k, Text: text, Table: table, Line: line})
	l.dq = append(l.dq, false)
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
	l.coms = append(l.coms, Comment{Text: strings.TrimSpace(rest[:end]), Line: line})
	l.advance(n + end)
}

// blockComment reads a /* */ comment.
func (l *lexer) blockComment() error {
	line := l.line
	end := strings.Index(l.src[l.pos+2:], "*/")
	if end < 0 {
		return &LexError{Line: line, Msg: msgBlockComment}
	}
	l.coms = append(l.coms, Comment{Text: strings.TrimSpace(l.src[l.pos+2 : l.pos+2+end]), Line: line})
	l.advance(2 + end + 2)
	return nil
}

// delimited reads a run from the opening byte at pos to the closing byte, a
// doubled closing byte standing for itself. It returns the content,
// unescaped.
func (l *lexer) delimited(closer byte, msg string) (string, error) {
	line := l.line
	var b strings.Builder
	i := l.pos + 1
	for {
		j := strings.IndexByte(l.src[i:], closer)
		if j < 0 {
			return "", &LexError{Line: line, Msg: msg}
		}
		b.WriteString(l.src[i : i+j])
		i += j
		if i+1 < len(l.src) && l.src[i+1] == closer {
			b.WriteByte(closer)
			i += 2
			continue
		}
		l.advance(i + 1 - l.pos)
		return b.String(), nil
	}
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

// SQL lexes SQL, reading "..." as dq says.
func SQL(src string, dq DoubleQuote) ([]Token, []Comment, error) {
	l, err := lexSQL(src, dq)
	if err != nil {
		return nil, nil, err
	}
	return l.toks, l.coms, nil
}

func lexSQL(src string, dq DoubleQuote) (*lexer, error) {
	l := newLexer(src)
	for l.pos < len(src) {
		r, _ := l.rune()
		var err error
		switch {
		case isSpace(r):
			l.skip()
		case l.has("--"):
			l.lineComment(2)
		case l.has("/*"):
			err = l.blockComment()
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
