package exprlex

import (
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
}

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
type analysis struct {
	t      []Token
	match  []int // partner of an Open or Close; -1 when unmatched or not a bracket
	lit    []int // literal ordinal of a Str or Num; -1 otherwise
	concat string
	cols   map[int][]int // literal token -> associated column tokens
}

func analyze(tokens []Token, concat string) *analysis {
	a := &analysis{t: tokens, match: make([]int, len(tokens)), lit: make([]int, len(tokens)), concat: concat, cols: map[int][]int{}}
	var stack []int
	n := 0
	for i, tok := range tokens {
		a.match[i], a.lit[i] = -1, -1
		switch tok.Kind {
		case Str, Num:
			a.lit[i] = n
			n++
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

// primaryFwd returns where the primary starting at j ends, or -1.
func (a *analysis) primaryFwd(j int) int {
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
	case Op:
		if t := a.t[j].Text; t == "-" || t == "+" {
			return a.primaryFwd(j + 1)
		}
	}
	return -1
}

// operandBack returns the start of the operand (a chain of primaries joined
// by the concatenation operator) that ends at j, or -1.
func (a *analysis) operandBack(j int) int {
	s := a.primaryBack(j)
	for s >= 0 && a.isConcat(s-1) {
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
	for e >= 0 && a.isConcat(e+1) {
		p := a.primaryFwd(e + 2)
		if p < 0 {
			break
		}
		e = p
	}
	return e
}

// columns lists the column tokens in [s, e].
func (a *analysis) columns(s, e int) []int {
	var out []int
	for i := s; i >= 0 && i <= e; i++ {
		if a.t[i].Kind == Col {
			out = append(out, i)
		}
	}
	return out
}

// literals lists the literals of an operand [s, e] that take its other
// side's columns: every literal in it when it holds no column (so
// lower('X') counts), otherwise only the literals of its top-level chain.
func (a *analysis) literals(s, e int) []int {
	if s < 0 || e < s {
		return nil
	}
	whole := len(a.columns(s, e)) == 0
	var out []int
	for i := s; i <= e; i++ {
		if a.lit[i] >= 0 {
			out = append(out, i)
		}
		if !whole && a.t[i].Kind == Open && a.match[i] > i {
			i = a.match[i]
		}
	}
	return out
}

func (a *analysis) associate(lits, cols []int) {
	if len(cols) == 0 {
		return
	}
	for _, l := range lits {
		a.cols[l] = append(a.cols[l], cols...)
	}
}

// pair ties the literals of each operand to the columns of the other.
func (a *analysis) pair(ls, le, rs, re int) {
	if ls < 0 || rs < 0 || le < ls || re < rs {
		return
	}
	a.associate(a.literals(ls, le), a.columns(rs, re))
	a.associate(a.literals(rs, re), a.columns(ls, le))
}

// Associate returns one site per literal of tokens, in order, with the
// columns each is associated with (behavior.md, "Association in code").
// concat is the language's concatenation operator: "&" for DAX and M, "||"
// for SQL.
func Associate(tokens []Token, concat string) []Site {
	a := analyze(tokens, concat)
	for i, tok := range a.t {
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
		if a.lit[i] < 0 {
			continue
		}
		sites = append(sites, Site{
			Value:   tok.Text,
			Number:  tok.Kind == Num,
			Columns: a.columnsOf(i),
			Index:   a.lit[i],
			Line:    tok.Line,
		})
	}
	return sites
}

func (a *analysis) columnsOf(lit int) []finding.Column {
	idx := slices.Clone(a.cols[lit])
	slices.Sort(idx)
	var out []finding.Column
	for _, i := range idx {
		c := finding.Column{Table: a.t[i].Table, Name: a.t[i].Text}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
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
	cols := a.columns(a.operandBack(end), end)
	a.associate(a.listLiterals(open), cols)
}

// listLiterals lists the literals directly inside the list opened at open,
// and those inside a tuple (a bracket that is not a call) one level down.
func (a *analysis) listLiterals(open int) []int {
	var out []int
	for i := open + 1; i < a.match[open]; i++ {
		switch {
		case a.lit[i] >= 0:
			out = append(out, i)
		case a.t[i].Kind == Open && a.match[i] > i:
			if !a.isCall(i) {
				for k := i + 1; k < a.match[i]; k++ {
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
	for i := open + 1; i <= closeAt; i++ {
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

func (a *analysis) hasComparison(s, e int) bool {
	for i := s; i <= e; i++ {
		if a.t[i].Kind == Op && comparisons[a.t[i].Text] {
			return true
		}
	}
	return false
}

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
	var cols, lits []int
	for k, arg := range args {
		s, e := arg[0], arg[1]
		c := a.columns(s, e)
		if name == "switch" {
			switch {
			case k == 0:
				if !a.hasComparison(s, e) {
					cols = c
				}
			case k%2 == 1 && k+1 < len(args) && len(c) == 0:
				lits = append(lits, a.literals(s, e)...)
			}
			continue
		}
		if len(c) > 0 {
			if !a.hasComparison(s, e) {
				cols = append(cols, c...)
			}
			continue
		}
		for i := s; i <= e; i++ {
			if a.lit[i] < 0 {
				continue
			}
			if (name == "search" || name == "find") && a.t[i].Kind == Num {
				continue
			}
			lits = append(lits, i)
		}
	}
	a.associate(lits, cols)
}

// DatatableHeaders maps the Index of each literal in the rows of a DATATABLE
// call to the header names that call declares.
func DatatableHeaders(tokens []Token) map[int][]string {
	a := analyze(tokens, "&")
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

// SQLSites lexes src as SQL under both readings of "..." (a string, and a
// quoted identifier) and merges them (behavior.md, "SQL" lexing). Its sites
// are the string reading's, each with the columns either reading ties to it;
// a double-quoted literal has only the string reading's. Its comments are the
// same under both readings.
func SQLSites(src string) ([]Site, []Comment, error) {
	ls, err := lexSQL(src, QuoteString)
	if err != nil {
		return nil, nil, err
	}
	li, err := lexSQL(src, QuoteIdent)
	if err != nil {
		return nil, nil, err
	}
	return mergeReadings(ls, li), ls.coms, nil
}

// mergeReadings adds the identifier reading's columns to the string
// reading's sites. The identifier reading has the same literals minus the
// double-quoted ones, in the same order.
func mergeReadings(ls, li *lexer) []Site {
	ss := Associate(ls.toks, "||")
	si := Associate(li.toks, "||")
	var dq []bool
	for i, tok := range ls.toks {
		if tok.Kind == Str || tok.Kind == Num {
			dq = append(dq, ls.dq[i])
		}
	}
	j := 0
	for k := range ss {
		if dq[k] || j >= len(si) {
			continue
		}
		for _, c := range si[j].Columns {
			if !slices.Contains(ss[k].Columns, c) {
				ss[k].Columns = append(ss[k].Columns, c)
			}
		}
		j++
	}
	return ss
}
