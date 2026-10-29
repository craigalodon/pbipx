package exprlex

import (
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/finding"
)

// Site is one literal of an expression, with the columns it is associated
// with.
type Site struct {
	Value   string
	Number  bool
	Columns []finding.Column // deduplicated, in order of appearance; nil when none
	Index   int              // ordinal among the expression's literals
	Line    int
	off     int // byte offset in the source (SQLSites)
}

// The association limits (behavior.md, "Association in code"). Association
// is near-linear in the size of the code, and these bound what is left: an
// expression past either one is an error, reported as the caller's parse
// error (unlexable-sql, unparseable-tmdl), never cut short and called clean.
const (
	// MaxColumnsPerLiteral is the most distinct columns one literal may be
	// associated with.
	MaxColumnsPerLiteral = 256
	// WorkPerToken and WorkFloor bound the work of associating n tokens to
	// WorkFloor + WorkPerToken*n units, a unit being a token visited, a
	// literal tied to a group of columns, or a column found for one.
	WorkPerToken = 64
	WorkFloor    = 1 << 16
)

// MsgTooLarge is the message of the *LexError for code past the
// association limits.
const MsgTooLarge = "too large to associate"

// matchingFuncs are the functions that tie a literal argument to the columns
// passed to the same call: DAX, then M. Compared case-insensitively.
var matchingFuncs = map[string]bool{
	"containsstring": true, "containsstringexact": true, "search": true, "find": true,
	"switch": true, "lookupvalue": true, "containsrow": true, "contains": true,
	"treatas": true, "exact": true, "pathcontains": true,
	"text.contains": true, "text.startswith": true, "text.endswith": true,
	"list.contains": true, "list.containsany": true, "list.containsall": true,
	"value.equals": true, "comparer.equals": true,
}

var comparisons = map[string]bool{"=": true, "==": true, "<>": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true}

var patternOps = map[string]bool{"like": true, "ilike": true, "rlike": true, "regexp": true}

// analysis is a token stream with its brackets matched and its literals
// numbered.
//
// Columns are numbered by position, in order of appearance. A group is the
// columns a literal is tied to by one comparison, IN list or matching call:
// a few ranges of positions, built once and shared by every literal it ties.
// A group's distinct columns are found once, through a tree over prev, at
// O(log n) each.
type analysis struct {
	t      []Token
	match  []int // partner of an Open or Close; -1 when unmatched or not a bracket
	lit    []int // literal ordinal of a Str or Num; -1 otherwise
	concat string

	colPre []int // colPre[i] is the number of Col tokens in t[:i]
	cmpPre []int // cmpPre[i] is the number of comparison operators in t[:i]
	colTok []int // the token of each column position
	key    []int // each column position's (table, name), interned
	prev   []int // the last earlier position with the same key, or -1
	seg    []int // a min tree over prev, built on first use
	size   int   // the number of leaves of seg

	groups   [][][2]int    // each group's ranges [lo, hi) of column positions
	resolved [][]int       // each group's distinct columns, as positions
	done     []bool        // whether resolved holds a group's columns
	ties     map[int][]int // literal token -> the groups it is tied to

	work, budget int
	line         int // the line being worked on, for the limit's error
	err          error
}

func analyze(tokens []Token, concat string) *analysis {
	n := len(tokens)
	a := &analysis{
		t: tokens, match: make([]int, n), lit: make([]int, n), concat: concat,
		colPre: make([]int, n+1), cmpPre: make([]int, n+1), ties: map[int][]int{},
		budget: WorkFloor + WorkPerToken*n,
	}
	var stack []int
	last := map[finding.Column]int{}
	keys := map[finding.Column]int{}
	lits := 0
	for i, tok := range tokens {
		a.match[i], a.lit[i] = -1, -1
		a.colPre[i+1], a.cmpPre[i+1] = a.colPre[i], a.cmpPre[i]
		switch tok.Kind {
		case Str, Num:
			a.lit[i] = lits
			lits++
		case Col:
			c := finding.Column{Table: tok.Table, Name: tok.Text}
			k, ok := keys[c]
			if !ok {
				k = len(keys)
				keys[c] = k
			}
			p := len(a.colTok)
			prev, seen := last[c]
			if !seen {
				prev = -1
			}
			last[c] = p
			a.colTok, a.key, a.prev = append(a.colTok, i), append(a.key, k), append(a.prev, prev)
			a.colPre[i+1]++
		case Op:
			if comparisons[tok.Text] {
				a.cmpPre[i+1]++
			}
		case Open:
			stack = append(stack, i)
		case Close:
			if len(stack) > 0 {
				o := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				a.match[i], a.match[o] = o, i
			}
		}
	}
	return a
}

// spend counts n units of work, and reports false once the work is past the
// budget, recording the error.
func (a *analysis) spend(n int) bool {
	if a.err != nil {
		return false
	}
	a.work += n
	if a.work > a.budget {
		a.err = &LexError{Line: a.line, Msg: MsgTooLarge}
		return false
	}
	return true
}

func (a *analysis) is(i int, k Kind) bool { return i >= 0 && i < len(a.t) && a.t[i].Kind == k }

func (a *analysis) word(i int, w string) bool {
	return a.is(i, Ident) && strings.EqualFold(a.t[i].Text, w)
}

func (a *analysis) isConcat(i int) bool { return a.is(i, Op) && a.t[i].Text == a.concat }

func (a *analysis) isCall(open int) bool {
	return a.is(open, Open) && a.t[open].Text == "(" && a.is(open-1, Ident)
}

// primaryBack returns where the primary ending at j starts, or -1.
func (a *analysis) primaryBack(j int) int {
	if j < 0 || j >= len(a.t) {
		return -1
	}
	switch a.t[j].Kind {
	case Str, Num, Col, Ident:
		return j
	case Close:
		o := a.match[j]
		if o < 0 {
			return -1
		}
		if a.isCall(o) {
			return o - 1
		}
		return o
	}
	return -1
}

// primaryFwd returns where the primary starting at j ends, or -1. A unary
// sign is part of the primary.
func (a *analysis) primaryFwd(j int) int {
	for a.is(j, Op) && (a.t[j].Text == "-" || a.t[j].Text == "+") {
		if !a.spend(1) {
			return -1
		}
		j++
	}
	if j < 0 || j >= len(a.t) {
		return -1
	}
	switch a.t[j].Kind {
	case Str, Num, Col:
		return j
	case Ident:
		if a.isCall(j + 1) {
			return a.match[j+1]
		}
		return j
	case Open:
		return a.match[j]
	}
	return -1
}

// operandBack returns the start of the operand (a chain of primaries joined
// by the concatenation operator) that ends at j, or -1.
func (a *analysis) operandBack(j int) int {
	s := a.primaryBack(j)
	for s >= 0 && a.isConcat(s-1) && a.spend(1) {
		p := a.primaryBack(s - 2)
		if p < 0 {
			break
		}
		s = p
	}
	return s
}

// operandFwd returns the end of the operand that starts at j, or -1.
func (a *analysis) operandFwd(j int) int {
	e := a.primaryFwd(j)
	for e >= 0 && a.isConcat(e+1) && a.spend(1) {
		p := a.primaryFwd(e + 2)
		if p < 0 {
			break
		}
		e = p
	}
	return e
}

// hasColumns reports whether [s, e] holds a column.
func (a *analysis) hasColumns(s, e int) bool { return a.colPre[e+1] > a.colPre[s] }

// span is the range of column positions of the tokens [s, e].
func (a *analysis) span(s, e int) [2]int { return [2]int{a.colPre[s], a.colPre[e+1]} }

// group records the columns in ranges as one group and returns its id, or
// -1 when the ranges hold no column.
func (a *analysis) group(ranges ...[2]int) int {
	ranges = slices.DeleteFunc(ranges, func(r [2]int) bool { return r[1] <= r[0] })
	if len(ranges) == 0 {
		return -1
	}
	a.groups = append(a.groups, ranges)
	a.resolved = append(a.resolved, nil)
	a.done = append(a.done, false)
	return len(a.groups) - 1
}

// literals lists the literals of an operand [s, e] that take its other
// side's columns: every literal in it when it holds no column (so
// lower('X') counts), otherwise only the literals of its top-level chain.
func (a *analysis) literals(s, e int) []int {
	if s < 0 || e < s {
		return nil
	}
	whole := !a.hasColumns(s, e)
	var out []int
	for i := s; i <= e && a.spend(1); i++ {
		if a.lit[i] >= 0 {
			out = append(out, i)
		}
		if !whole && a.t[i].Kind == Open && a.match[i] > i {
			i = a.match[i]
		}
	}
	return out
}

// tie ties each literal of lits to group g, if there is one.
func (a *analysis) tie(lits []int, g int) {
	if g < 0 || !a.spend(len(lits)) {
		return
	}
	for _, l := range lits {
		a.ties[l] = append(a.ties[l], g)
	}
}

// pair ties the literals of each operand to the columns of the other. An
// operand's literals are listed only when the other side has a column.
func (a *analysis) pair(ls, le, rs, re int) {
	if ls < 0 || rs < 0 || le < ls || re < rs {
		return
	}
	if g := a.group(a.span(rs, re)); g >= 0 {
		a.tie(a.literals(ls, le), g)
	}
	if g := a.group(a.span(ls, le)); g >= 0 {
		a.tie(a.literals(rs, re), g)
	}
}

// Associate is AssociateChecked without its error.
//
// Deprecated: past the association limits it returns every literal with no
// columns, judged on shape only. Use AssociateChecked and report its error.
func Associate(tokens []Token, concat string) []Site {
	sites, err := AssociateChecked(tokens, concat)
	if err == nil {
		return sites
	}
	for _, tok := range tokens {
		if tok.Kind == Str || tok.Kind == Num {
			sites = append(sites, Site{Value: tok.Text, Number: tok.Kind == Num, Index: len(sites), Line: tok.Line})
		}
	}
	return sites
}

// AssociateChecked returns one site per literal of tokens, in order, with
// the columns each is associated with (behavior.md, "Association in code").
// concat is the language's concatenation operator: "&" for DAX and M, "||"
// for SQL. Code past the association limits returns a *LexError with
// MsgTooLarge and no sites.
func AssociateChecked(tokens []Token, concat string) ([]Site, error) {
	a := analyze(tokens, concat)
	for i, tok := range a.t {
		if a.err != nil {
			break
		}
		a.line = tok.Line
		switch {
		case tok.Kind == Op && comparisons[tok.Text]:
			a.pair(a.operandBack(i-1), i-1, i+1, a.operandFwd(i+1))
		case tok.Kind == Ident && patternOps[strings.ToLower(tok.Text)]:
			end := i - 1
			if a.word(end, "not") {
				end--
			}
			a.pair(a.operandBack(end), end, i+1, a.operandFwd(i+1))
		case tok.Kind == Ident && strings.EqualFold(tok.Text, "in"):
			a.inList(i)
		case tok.Kind == Ident && matchingFuncs[strings.ToLower(tok.Text)] && a.isCall(i+1):
			a.matching(i)
		}
	}
	var sites []Site
	for i, tok := range a.t {
		if a.err != nil {
			break
		}
		if a.lit[i] < 0 {
			continue
		}
		a.line = tok.Line
		sites = append(sites, Site{
			Value:   tok.Text,
			Number:  tok.Kind == Num,
			Columns: a.columnsOf(i),
			Index:   a.lit[i],
			Line:    tok.Line,
		})
	}
	if a.err != nil {
		return nil, a.err
	}
	return sites, nil
}

// columnsOf is the distinct columns a literal is tied to, in order of
// appearance.
func (a *analysis) columnsOf(lit int) []finding.Column {
	gs := a.ties[lit]
	if len(gs) == 0 {
		return nil
	}
	var pos []int
	for _, g := range gs {
		pos = append(pos, a.resolve(g)...)
	}
	if len(gs) > 1 {
		slices.Sort(pos)
		seen := map[int]bool{}
		pos = slices.DeleteFunc(pos, func(p int) bool {
			if seen[a.key[p]] {
				return true
			}
			seen[a.key[p]] = true
			return false
		})
	}
	if !a.spend(len(pos)) {
		return nil
	}
	if len(pos) > MaxColumnsPerLiteral {
		a.err = &LexError{Line: a.line, Msg: MsgTooLarge}
		return nil
	}
	out := make([]finding.Column, len(pos))
	for k, p := range pos {
		t := a.t[a.colTok[p]]
		out[k] = finding.Column{Table: t.Table, Name: t.Text}
	}
	return out
}

// resolve finds the distinct columns of group g once: in each of its
// ranges, the positions whose key does not occur earlier in the range,
// then those not found in an earlier range.
func (a *analysis) resolve(g int) []int {
	if a.done[g] {
		return a.resolved[g]
	}
	a.done[g] = true
	if a.seg == nil {
		a.buildTree()
	}
	seen := map[int]bool{}
	var out []int
	for _, r := range a.groups[g] {
		for p := r[0]; ; p++ {
			p = a.firstBelow(1, 0, a.size, p, r[1], r[0])
			if p < 0 || !a.spend(1) {
				break
			}
			if seen[a.key[p]] {
				continue
			}
			seen[a.key[p]] = true
			out = append(out, p)
			if len(out) > MaxColumnsPerLiteral {
				a.err = &LexError{Line: a.line, Msg: MsgTooLarge}
				return nil
			}
		}
	}
	a.resolved[g] = out
	return out
}

func (a *analysis) buildTree() {
	a.size = 1
	for a.size < len(a.prev) {
		a.size <<= 1
	}
	a.seg = make([]int, 2*a.size)
	for i := range a.seg {
		a.seg[i] = math.MaxInt
	}
	copy(a.seg[a.size:], a.prev)
	for i := a.size - 1; i >= 1; i-- {
		a.seg[i] = min(a.seg[2*i], a.seg[2*i+1])
	}
}

// firstBelow returns the first position in [from, hi) whose prev is below
// thr, searching node, which covers [nl, nr); or -1.
func (a *analysis) firstBelow(node, nl, nr, from, hi, thr int) int {
	if nr <= from || nl >= hi || a.seg[node] >= thr {
		return -1
	}
	if nr-nl == 1 {
		return nl
	}
	mid := (nl + nr) / 2
	if p := a.firstBelow(2*node, nl, mid, from, hi, thr); p >= 0 {
		return p
	}
	return a.firstBelow(2*node+1, mid, nr, from, hi, thr)
}

// inList handles x IN (...) and x NOT IN {...}: each literal of the list,
// and of a tuple one level down, takes the columns of x.
func (a *analysis) inList(i int) {
	open := i + 1
	if !a.is(open, Open) || a.match[open] < 0 {
		return
	}
	if a.word(open+1, "select") || a.word(open+1, "with") {
		return // a subquery, whose own filters are associated on their own
	}
	end := i - 1
	if a.word(end, "not") {
		end--
	}
	s := a.operandBack(end)
	if s < 0 {
		return
	}
	if g := a.group(a.span(s, end)); g >= 0 {
		a.tie(a.listLiterals(open), g)
	}
}

// listLiterals lists the literals directly inside the list opened at open,
// and those inside a tuple (a bracket that is not a call) one level down.
func (a *analysis) listLiterals(open int) []int {
	var out []int
	for i := open + 1; i < a.match[open] && a.spend(1); i++ {
		switch {
		case a.lit[i] >= 0:
			out = append(out, i)
		case a.t[i].Kind == Open && a.match[i] > i:
			if !a.isCall(i) {
				for k := i + 1; k < a.match[i] && a.spend(1); k++ {
					if a.lit[k] >= 0 {
						out = append(out, k)
					}
				}
			}
			i = a.match[i]
		}
	}
	return out
}

// args splits the call whose ( is at open into its arguments, as inclusive
// spans; an empty argument is skipped.
func (a *analysis) args(open int) [][2]int {
	var out [][2]int
	start := open + 1
	closeAt := a.match[open]
	for i := open + 1; i <= closeAt && a.spend(1); i++ {
		if i == closeAt || a.t[i].Kind == Comma {
			if i > start {
				out = append(out, [2]int{start, i - 1})
			}
			start = i + 1
			continue
		}
		if a.t[i].Kind == Open && a.match[i] > i {
			i = a.match[i]
		}
	}
	return out
}

func (a *analysis) hasComparison(s, e int) bool { return a.cmpPre[e+1] > a.cmpPre[s] }

// matching handles a matching function: each literal argument (or literal in
// a list argument) takes the columns passed to the same call. An argument
// that is a condition (it holds a comparison) passes no columns.
//
// Two refinements keep positions and results from taking a column: SEARCH
// and FIND tie no number (their numbers are a start position and a
// not-found value), and SWITCH ties only its match values to the columns of
// its first argument.
func (a *analysis) matching(fn int) {
	name := strings.ToLower(a.t[fn].Text)
	args := a.args(fn + 1)
	var ranges [][2]int
	for k, arg := range args {
		s, e := arg[0], arg[1]
		if !a.hasColumns(s, e) || a.hasComparison(s, e) || (name == "switch" && k != 0) {
			continue
		}
		ranges = append(ranges, a.span(s, e))
	}
	g := a.group(ranges...)
	if g < 0 {
		return
	}
	var lits []int
	for k, arg := range args {
		s, e := arg[0], arg[1]
		if a.hasColumns(s, e) {
			continue
		}
		if name == "switch" {
			if k%2 == 1 && k+1 < len(args) {
				lits = append(lits, a.literals(s, e)...)
			}
			continue
		}
		for i := s; i <= e && a.spend(1); i++ {
			if a.lit[i] < 0 {
				continue
			}
			if (name == "search" || name == "find") && a.t[i].Kind == Num {
				continue
			}
			lits = append(lits, i)
		}
	}
	a.tie(lits, g)
}

// DatatableHeaders maps the Index of each literal in the rows of a DATATABLE
// call to the header names that call declares.
func DatatableHeaders(tokens []Token) map[int][]string {
	a := analyze(tokens, "&")
	a.budget = math.MaxInt // the rows are visited once each
	out := map[int][]string{}
	for i := range a.t {
		if !a.word(i, "datatable") || !a.isCall(i+1) {
			continue
		}
		args := a.args(i + 1)
		var headers []string
		for _, arg := range args {
			if arg[0] == arg[1] && a.t[arg[0]].Kind == Str {
				headers = append(headers, a.t[arg[0]].Text)
			}
		}
		for _, arg := range args {
			if !a.is(arg[0], Open) || a.t[arg[0]].Text != "{" {
				continue
			}
			for k := arg[0]; k <= arg[1]; k++ {
				if a.lit[k] >= 0 {
					out[a.lit[k]] = headers
				}
			}
		}
	}
	return out
}

// InBraces maps the Index of each literal inside any {...} to true.
func InBraces(tokens []Token) map[int]bool {
	a := analyze(tokens, "&")
	out := map[int]bool{}
	depth := 0
	for i, tok := range a.t {
		switch {
		case tok.Kind == Open && tok.Text == "{":
			depth++
		case tok.Kind == Close && tok.Text == "}" && depth > 0:
			depth--
		case a.lit[i] >= 0 && depth > 0:
			out[a.lit[i]] = true
		}
	}
	return out
}

var sqlRE = regexp.MustCompile(`(?is)\bselect\b.*\bfrom\b`)

// LooksLikeSQL reports whether s reads as a query: select ... from, in any
// case.
func LooksLikeSQL(s string) bool { return sqlRE.MatchString(s) }

// SQLSites lexes src as SQL under every reading dialects disagree on, and
// merges them (behavior.md, "SQL files"):
//
//   - "..." is read as a string and as a quoted identifier; a literal takes
//     the columns either reading ties to it, and a double-quoted literal has
//     only the string reading's;
//   - a quote inside a string is escaped by doubling it, and also, in a
//     second reading, by a backslash;
//   - # is an operator, and also, in a third reading, starts a line comment
//     (and a fourth reading has both backslash escapes and # comments);
//   - a dollar-quoted string is a string, and also, in a copy of each
//     reading before, a body whose content is lexed as SQL.
//
// readingsOf leaves out a reading that cannot differ from an earlier one.
//
// The sites are the union of the sites of every reading that lexes; a lex
// error is returned only when none does, and it is the first reading's. A
// literal at the same offset with the same value under two readings is one
// site, with the columns of both. Index stays stable: the sites of the
// first reading that lexes keep their ordinals, in order, and a site found
// only under a later reading is numbered after them, in reading order, so a
// file that every reading lexes alike keeps the locators it always had.
// Comments are merged the same way, the later readings' marked Extra and
// listed after the first reading's.
//
// Code past the association limits is an error under any reading.
func SQLSites(src string) ([]Site, []Comment, error) {
	type siteKey struct {
		off    int
		value  string
		number bool
	}
	type comKey struct {
		off  int
		text string
	}
	var sites []Site
	var coms []Comment
	siteAt := map[siteKey]int{}
	comSeen := map[comKey]bool{}
	var lexErr error
	lexed := false
	var associated [][2]*lexer // the token streams associated so far
	for _, rd := range readingsOf(src) {
		ls, err := lexSQL(src, QuoteString, rd)
		var li *lexer // nil when src has no ", and both readings are ls
		if err == nil && strings.Contains(src, `"`) {
			li, err = lexSQL(src, QuoteIdent, rd)
		}
		if err != nil {
			if lexErr == nil {
				lexErr = err
			}
			continue
		}
		var ss []Site
		// A reading whose tokens are an earlier one's (it differs only in its
		// comments) has that reading's sites, already merged.
		if !slices.ContainsFunc(associated, func(p [2]*lexer) bool { return sameTokens(p[0], ls) && sameTokens(p[1], li) }) {
			associated = append(associated, [2]*lexer{ls, li})
			if ss, err = mergeReadings(ls, li); err != nil {
				return nil, nil, err
			}
		}
		for _, s := range ss {
			k := siteKey{s.off, s.Value, s.Number}
			if j, ok := siteAt[k]; ok {
				sites[j].Columns = addColumns(sites[j].Columns, s.Columns)
				if len(sites[j].Columns) > MaxColumnsPerLiteral {
					return nil, nil, &LexError{Line: s.Line, Msg: MsgTooLarge}
				}
				continue
			}
			s.Index = len(sites)
			siteAt[k] = len(sites)
			sites = append(sites, s)
		}
		for _, c := range ls.coms {
			k := comKey{c.off, c.Text}
			if comSeen[k] {
				continue
			}
			comSeen[k] = true
			c.Extra = lexed
			coms = append(coms, c)
		}
		lexed = true
	}
	if !lexed {
		return nil, nil, lexErr
	}
	return sites, coms, nil
}

// SQLCode marks each byte of src that is code under every reading SQLSites
// lexes it with: outside every comment, string and quoted name. Its error
// is SQLSites's lex error, when no reading lexes.
func SQLCode(src string) ([]bool, error) {
	code := make([]bool, len(src))
	for i := range code {
		code[i] = true
	}
	var lexErr error
	lexed := false
	for _, rd := range readingsOf(src) {
		l, err := lexSQL(src, QuoteIdent, rd)
		if err != nil {
			if lexErr == nil {
				lexErr = err
			}
			continue
		}
		lexed = true
		for _, sp := range l.spans {
			for i := sp[0]; i < sp[1]; i++ {
				code[i] = false
			}
		}
	}
	if !lexed {
		return nil, lexErr
	}
	return code, nil
}

// sameTokens reports whether two lexers (either nil) hold the same tokens at
// the same offsets.
func sameTokens(a, b *lexer) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.toks) != len(b.toks) || !slices.Equal(a.offs, b.offs) || !slices.Equal(a.dq, b.dq) {
		return false
	}
	for i := range a.toks {
		x, y := a.toks[i], b.toks[i]
		if x.Kind != y.Kind || x.Text != y.Text || x.Table != y.Table {
			return false
		}
	}
	return true
}

func addColumns(to, cols []finding.Column) []finding.Column {
	for _, c := range cols {
		if !slices.Contains(to, c) {
			to = append(to, c)
		}
	}
	return to
}

// mergeReadings adds the identifier reading's columns to the string
// reading's sites, and sets each site's offset. The identifier reading has
// the same literals minus the double-quoted ones, in the same order. A nil
// li is the same as ls.
func mergeReadings(ls, li *lexer) ([]Site, error) {
	ss, err := AssociateChecked(ls.toks, "||")
	if err != nil {
		return nil, err
	}
	var si []Site
	if li != nil {
		if si, err = AssociateChecked(li.toks, "||"); err != nil {
			return nil, err
		}
	}
	var dq []bool
	var offs []int
	for i, tok := range ls.toks {
		if tok.Kind == Str || tok.Kind == Num {
			dq = append(dq, ls.dq[i])
			offs = append(offs, ls.offs[i])
		}
	}
	j := 0
	for k := range ss {
		ss[k].off = offs[k]
		if li == nil || dq[k] || j >= len(si) {
			continue
		}
		ss[k].Columns = addColumns(ss[k].Columns, si[j].Columns)
		if len(ss[k].Columns) > MaxColumnsPerLiteral {
			return nil, &LexError{Line: ss[k].Line, Msg: MsgTooLarge}
		}
		j++
	}
	return ss, nil
}
