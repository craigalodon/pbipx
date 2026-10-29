package tmdl

import (
	"regexp"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/exprlex"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/shape"
	"github.com/craigalodon/pbipx/internal/sqlfile"
)

// Description is the structure of one TMDL file, as explain prints it
// (docs/design/explain.md). It holds names, types, modes and counts, never a
// value: a name with a personal-data shape is shape.Withheld, and a name
// read from a string literal (a navigation step, a relation) is printed only
// when it is a plain identifier. A section the file does not declare is
// left out.
type Description struct {
	Tables        []Table        `json:"tables,omitempty"`
	Relationships []Relationship `json:"relationships,omitempty"`
	Roles         []Role         `json:"roles,omitempty"`
	Expressions   []Expression   `json:"expressions,omitempty"`
	Model         *ModelInfo     `json:"model,omitempty"`
	// Literals counts the literals of the file's DAX, M and native SQL, and
	// the cells of its Enter data tables, by context.
	Literals map[string]int `json:"literals"`
}

// Table is a table and what it declares.
type Table struct {
	Name       string      `json:"name"`
	Columns    []Column    `json:"columns"`
	Measures   []string    `json:"measures"`
	Partitions []Partition `json:"partitions"`
}

// Column is a column: its data type, when declared, and whether it is
// calculated (declared with a DAX expression).
type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type,omitempty"`
	Calculated bool   `json:"calculated"`
}

// Partition is where a table's rows come from.
type Partition struct {
	Name   string `json:"name"`
	Source string `json:"source"` // m, calculated, entity, ...
	Mode   string `json:"mode"`   // the partition's, or else the model's default
	// Connectors are the data-access functions its M calls.
	Connectors []string `json:"connectors"`
	// Reads are the navigation paths its M reads, each step printable.
	Reads []string `json:"reads"`
	// NativeSQLReads are the relations SQL in its M strings reads.
	NativeSQLReads []string `json:"native_sql_reads"`
}

// Relationship is a relationship's columns, as table.column.
type Relationship struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Role is a role: the tables it filters and how many members it has. The
// members themselves are never described (principle 9).
type Role struct {
	Name    string   `json:"name"`
	Filters []string `json:"filters"`
	Members int      `json:"members"`
}

// Expression is a named M expression: a parameter or a query.
type Expression struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ModelInfo is the model declaration's settings.
type ModelInfo struct {
	DefaultMode string `json:"default_mode"`
}

// Expression kinds.
const (
	kindParameter = "parameter"
	kindQuery     = "query"
)

// defaultMode is a model's storage mode when it declares none.
const defaultMode = "import"

// countedContexts are the contexts Literals counts: values in code and
// data, not property values, text or role members.
var countedContexts = []finding.Context{finding.TMDLDAX, finding.TMDLM, finding.TMDLNativeSQL, finding.TMDLEnterData}

// Describe describes the parsed files of one model. All of them are needed
// first: a partition can read through a parameter or a query that another
// file defines. It returns, for each root, its description or the
// *judge.ParseError its scan would fail with.
func Describe(roots []*Node) ([]*Description, []error) {
	m := newModel(roots)
	descs := make([]*Description, len(roots))
	errs := make([]error, len(roots))
	for i, root := range roots {
		descs[i], errs[i] = m.describe(root)
	}
	return descs, errs
}

// model is what every file of a model can refer to.
type model struct {
	params      map[string]string // parameter name to its literal value
	queries     map[string]*Node  // query expressions by name
	kinds       map[string]string // every named expression's kind
	defaultMode string
	evals       map[string]*mResult // query results, memoized
	evaluating  map[string]bool
}

func newModel(roots []*Node) *model {
	m := &model{
		params: map[string]string{}, queries: map[string]*Node{}, kinds: map[string]string{},
		defaultMode: defaultMode,
		evals:       map[string]*mResult{}, evaluating: map[string]bool{},
	}
	for _, root := range roots {
		for _, n := range root.Children {
			switch {
			case is(n, "expression") && n.Name != "" && n.Sep == Equals:
				toks, _, err := exprlex.M(n.Value)
				if err != nil {
					continue
				}
				if isParameter(toks) {
					m.kinds[n.Name] = kindParameter
					if len(toks) > 0 && toks[0].Kind == exprlex.Str {
						m.params[n.Name] = toks[0].Text
					}
					continue
				}
				m.queries[n.Name] = n
				m.kinds[n.Name] = kindQuery
			case is(n, "model"):
				if v, ok := property(n, "defaultMode"); ok {
					m.defaultMode = v
				}
			}
		}
	}
	return m
}

func (m *model) describe(root *Node) (*Description, error) {
	s := &scanner{counts: map[finding.Context]int{}}
	if err := s.children(root, "", scope{}); err != nil {
		return nil, err
	}
	d := &Description{Literals: map[string]int{}}
	for _, ctx := range countedContexts {
		if n := s.counts[ctx]; n > 0 {
			d.Literals[string(ctx)] = n
		}
	}
	for _, n := range root.Children {
		switch {
		case is(n, "table"):
			d.Tables = append(d.Tables, m.table(n))
		case is(n, "relationship"):
			from, _ := rawProperty(n, "fromColumn")
			to, _ := rawProperty(n, "toColumn")
			d.Relationships = append(d.Relationships, Relationship{From: columnRef(from), To: columnRef(to)})
		case is(n, "role"):
			d.Roles = append(d.Roles, role(n))
		case is(n, "expression") && n.Name != "":
			d.Expressions = append(d.Expressions, Expression{Name: shape.ObjectName(n.Name), Kind: m.kinds[n.Name]})
		case is(n, "model"):
			d.Model = &ModelInfo{DefaultMode: shape.IdentifierPart(m.defaultMode)}
		}
	}
	return d, nil
}

func (m *model) table(n *Node) Table {
	t := Table{Name: shape.ObjectName(n.Name), Columns: []Column{}, Measures: []string{}, Partitions: []Partition{}}
	for _, c := range n.Children {
		switch {
		case is(c, "column") && c.Name != "":
			col := Column{Name: shape.ObjectName(c.Name), Calculated: c.Sep == Equals}
			if v, ok := property(c, "dataType"); ok {
				col.Type = shape.IdentifierPart(v)
			}
			t.Columns = append(t.Columns, col)
		case is(c, "measure") && c.Name != "":
			t.Measures = append(t.Measures, shape.ObjectName(c.Name))
		case is(c, "partition") && c.Name != "":
			t.Partitions = append(t.Partitions, m.partition(c))
		}
	}
	return t
}

func (m *model) partition(n *Node) Partition {
	p := Partition{
		Name:       shape.ObjectName(n.Name),
		Source:     shape.IdentifierPart(strings.ToLower(strings.TrimSpace(n.Value))),
		Mode:       shape.IdentifierPart(m.defaultMode),
		Connectors: []string{}, Reads: []string{}, NativeSQLReads: []string{},
	}
	if v, ok := property(n, "mode"); ok {
		p.Mode = shape.IdentifierPart(v)
	}
	var src *Node
	for _, c := range n.Children {
		if is(c, "source") {
			src = c
		}
	}
	if src == nil {
		return p
	}
	switch strings.ToLower(strings.TrimSpace(n.Value)) {
	case "m", "policyrange":
		toks, _, err := exprlex.M(src.Value)
		if err != nil {
			return p // the literal count has already failed the file
		}
		r := m.eval(toks)
		p.Connectors, p.Reads, p.NativeSQLReads = r.lists()
	case "entity":
		// Direct Lake: the entity a named expression's source holds.
		var parts []string
		if v, ok := property(src, "schemaName"); ok {
			parts = append(parts, shape.IdentifierPart(v))
		}
		if v, ok := property(src, "entityName"); ok {
			parts = append(parts, shape.IdentifierPart(v))
			p.Reads = []string{strings.Join(parts, ".")}
		}
		if v, ok := property(src, "expressionSource"); ok {
			if r := m.query(v); r != nil {
				p.Connectors, _, p.NativeSQLReads = r.lists()
			}
		}
	}
	return p
}

func role(n *Node) Role {
	r := Role{Name: shape.ObjectName(n.Name), Filters: []string{}}
	for _, c := range n.Children {
		switch {
		case is(c, "member"):
			r.Members++
		case is(c, "tablePermission") && c.Name != "" && c.Sep == Equals:
			r.Filters = append(r.Filters, shape.ObjectName(c.Name))
		}
	}
	return r
}

// columnRef is a relationship's table.column, each part printable. TMDL
// quotes a name that is not a plain word: 'Table Name'.'Column Name'.
func columnRef(ref string) string {
	table, rest, ok := refPart(strings.TrimSpace(ref))
	if !ok || !strings.HasPrefix(rest, ".") {
		return shape.Withheld
	}
	col, rest, ok := refPart(rest[1:])
	if !ok || rest != "" {
		return shape.Withheld
	}
	return shape.ObjectName(table) + "." + shape.ObjectName(col)
}

// refPart reads a quoted or bare name and returns the text after it.
func refPart(s string) (string, string, bool) {
	if !strings.HasPrefix(s, "'") {
		end := strings.IndexByte(s, '.')
		if end < 0 {
			end = len(s)
		}
		return s[:end], s[end:], end > 0
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
		return b.String(), s[k+1:], true
	}
	return "", "", false
}

func is(n *Node, kind string) bool { return strings.EqualFold(n.Kind, kind) }

// property is the unquoted value of n's first `kind: value` child.
func property(n *Node, kind string) (string, bool) {
	v, ok := rawProperty(n, kind)
	return unquote(v), ok
}

// rawProperty is the value of n's first `kind: value` child as written.
func rawProperty(n *Node, kind string) (string, bool) {
	for _, c := range n.Children {
		if c.Sep == Colon && is(c, kind) {
			return c.Value, true
		}
	}
	return "", false
}

// isParameter reports whether an expression's M carries the parameter
// metadata: meta [IsParameterQuery = true, ...].
func isParameter(toks []exprlex.Token) bool {
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].Kind == exprlex.Ident && strings.EqualFold(toks[i].Text, "IsParameterQuery") &&
			toks[i+1].Kind == exprlex.Op && toks[i+1].Text == "=" &&
			toks[i+2].Kind == exprlex.Ident && strings.EqualFold(toks[i+2].Text, "true") {
			return true
		}
	}
	return false
}

// mPath is where a step's value comes from: a data source and the
// navigation steps taken from it, each printable.
type mPath struct {
	rooted bool // it starts at a connector's call
	parts  []string
	nav    int // navigation steps taken
}

// mResult is what one M expression reads.
type mResult struct {
	connectors map[string]bool
	paths      []mPath
	native     map[string]bool
	final      mPath
}

func newResult() *mResult {
	return &mResult{connectors: map[string]bool{}, native: map[string]bool{}}
}

// lists is the result as sorted lists: connectors, the navigation paths
// read (each path that no other extends), and the native SQL reads.
func (r *mResult) lists() (connectors, reads, native []string) {
	var paths []string
	for _, p := range r.paths {
		if p.rooted && p.nav > 0 {
			paths = append(paths, strings.Join(p.parts, "."))
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	reads = []string{}
	for _, p := range paths {
		// paths is sorted, so the first path at or after p+"." is one
		// that extends p, if any does.
		i, _ := slices.BinarySearch(paths, p+".")
		if i == len(paths) || !strings.HasPrefix(paths[i], p+".") {
			reads = append(reads, p)
		}
	}
	return sortedKeys(r.connectors), reads, sortedKeys(r.native)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// query evaluates a query expression by name, once; nil when there is none
// or it refers back to itself.
func (m *model) query(name string) *mResult {
	if r, ok := m.evals[name]; ok {
		return r
	}
	n := m.queries[name]
	if n == nil || m.evaluating[name] {
		return nil
	}
	m.evaluating[name] = true
	defer delete(m.evaluating, name)
	toks, _, err := exprlex.M(n.Value)
	if err != nil {
		return nil
	}
	r := m.eval(toks)
	m.evals[name] = r
	return r
}

// eval reads an M expression: its connectors, the native SQL in its
// strings, and the path of each let step.
func (m *model) eval(toks []exprlex.Token) *mResult {
	e := &mEval{m: m, toks: toks, steps: map[string]mPath{}, res: newResult(), ends: stepEnds(toks)}
	for i, t := range toks {
		if t.Kind == exprlex.Ident && i+1 < len(toks) && toks[i+1].Kind == exprlex.Open && toks[i+1].Text == "(" {
			if name, ok := connector(t.Text); ok {
				e.res.connectors[name] = true
			}
		}
		if t.Kind == exprlex.Str && exprlex.LooksLikeSQL(t.Text) {
			reads, err := sqlfile.NativeReads(mEscape.ReplaceAllString(t.Text, " "))
			if err != nil {
				e.res.native[shape.NotIdentifier] = true
				continue
			}
			for _, r := range reads {
				e.res.native[r] = true
			}
		}
	}
	e.res.final = e.body(0, len(toks))
	e.res.paths = append(e.res.paths, e.res.final)
	return e.res
}

// mEscape is an M string escape such as #(lf), which Desktop writes for the
// line breaks of a native query.
var mEscape = regexp.MustCompile(`#\([^)]*\)`)

type mEval struct {
	m     *model
	toks  []exprlex.Token
	steps map[string]mPath
	res   *mResult
	ends  []int // stepEnds(toks)
}

func (e *mEval) word(i int, w string) bool {
	return i >= 0 && i < len(e.toks) && e.toks[i].Kind == exprlex.Ident && strings.EqualFold(e.toks[i].Text, w)
}

func (e *mEval) open(i int, text string) bool {
	return i >= 0 && i < len(e.toks) && e.toks[i].Kind == exprlex.Open && e.toks[i].Text == text
}

// end is the index of the comma, closing bracket or `in` that ends the
// expression starting at s, at its own depth, or limit when none does
// before it.
func (e *mEval) end(s, limit int) int {
	if s >= limit || e.ends[s] >= limit {
		return limit
	}
	return e.ends[s]
}

// stepEnds maps each token index to the index of the comma, closing
// bracket or `in` that ends the expression starting there, at its own
// depth, or len(toks). A bracket or `let` opens a level; a closing bracket
// or `in` closes one. The end of the expression at k is the first such
// token at or after k on k's level: to fall below that level, the
// expression must first pass a closer on it. One backward pass finds them
// all, so nested let expressions are read in linear time.
func stepEnds(toks []exprlex.Token) []int {
	isWord := func(t exprlex.Token, w string) bool { return t.Kind == exprlex.Ident && strings.EqualFold(t.Text, w) }
	level := make([]int, len(toks))
	d := 0
	for k, t := range toks {
		level[k] = d
		switch {
		case t.Kind == exprlex.Open, isWord(t, "let"):
			d++
		case t.Kind == exprlex.Close, isWord(t, "in"):
			d--
		}
	}
	ends := make([]int, len(toks))
	next := map[int]int{} // each level's nearest terminator from k on
	for k := len(toks) - 1; k >= 0; k-- {
		t := toks[k]
		if t.Kind == exprlex.Close || t.Kind == exprlex.Comma || isWord(t, "in") {
			next[level[k]] = k
		}
		j, ok := next[level[k]]
		if !ok {
			j = len(toks)
		}
		ends[k] = j
	}
	return ends
}

// body evaluates toks[s:limit]: a let expression's steps and its result, or
// a single expression.
func (e *mEval) body(s, limit int) mPath {
	if !e.word(s, "let") {
		return e.expr(s, limit)
	}
	k := s + 1
	for k < limit {
		if e.word(k, "in") {
			return e.body(k+1, limit)
		}
		if e.toks[k].Kind != exprlex.Ident || k+1 >= limit || e.toks[k+1].Kind != exprlex.Op || e.toks[k+1].Text != "=" {
			return mPath{}
		}
		name := e.toks[k].Text
		stop := e.end(k+2, limit)
		p := e.body(k+2, stop)
		e.steps[name] = p
		e.res.paths = append(e.res.paths, p)
		k = stop
		if k < limit && e.toks[k].Kind == exprlex.Comma {
			k++
		}
	}
	return mPath{}
}

// expr evaluates one step's expression: a connector call, a call that
// passes an earlier step on, or a name, each followed by any navigation.
func (e *mEval) expr(s, limit int) mPath {
	if s >= limit || e.toks[s].Kind != exprlex.Ident {
		return mPath{}
	}
	var p mPath
	k := s + 1
	if e.open(k, "(") {
		end := e.closer(k, limit)
		args := e.args(k, end)
		name := e.toks[s].Text
		if _, ok := connector(name); ok {
			p = mPath{rooted: true}
			if strings.HasSuffix(strings.ToLower(name), ".database") && len(args) > 1 {
				p.parts = []string{e.value(args[1][0], args[1][1])}
			}
		} else if len(args) > 0 && args[0][1]-args[0][0] == 1 && e.toks[args[0][0]].Kind == exprlex.Ident {
			p = e.resolve(e.toks[args[0][0]].Text)
		}
		k = end + 1
	} else {
		p = e.resolve(e.toks[s].Text)
	}
	return e.navigate(p, k, limit)
}

// navigate applies the navigation after an expression: {[Schema = "dbo",
// Item = "t"]}[Data], {[Name = "t", Kind = "Table"]}[Data], {0}.
func (e *mEval) navigate(p mPath, k, limit int) mPath {
	for k < limit {
		switch {
		case e.open(k, "{"):
			end := e.closer(k, limit)
			parts := e.selector(k+1, end)
			p.parts = append(slices.Clone(p.parts), parts...)
			p.nav++
			k = end + 1
		case e.toks[k].Kind == exprlex.Col:
			k++ // a field access such as [Data]
		default:
			return p
		}
	}
	return p
}

// selector is the steps of one {...} navigation: each field of the record
// but Kind, in order, or a placeholder for a positional or computed one.
func (e *mEval) selector(s, limit int) []string {
	if !e.open(s, "[") {
		return []string{shape.NotIdentifier}
	}
	var parts []string
	rec := e.closer(s, limit)
	for _, a := range e.args(s, rec) {
		if a[1]-a[0] < 3 || e.toks[a[0]].Kind != exprlex.Ident || e.toks[a[0]+1].Kind != exprlex.Op || e.toks[a[0]+1].Text != "=" {
			parts = append(parts, shape.NotIdentifier)
			continue
		}
		if strings.EqualFold(e.toks[a[0]].Text, "Kind") {
			continue
		}
		parts = append(parts, e.value(a[0]+2, a[1]))
	}
	if len(parts) == 0 {
		return []string{shape.NotIdentifier}
	}
	return parts
}

// value is a name given as one string literal or one parameter, printable.
func (e *mEval) value(s, limit int) string {
	if limit-s != 1 {
		return shape.NotIdentifier
	}
	switch t := e.toks[s]; t.Kind {
	case exprlex.Str:
		return shape.IdentifierPart(t.Text)
	case exprlex.Ident:
		if v, ok := e.m.params[t.Text]; ok {
			return shape.IdentifierPart(v)
		}
	}
	return shape.NotIdentifier
}

// resolve is the path of a name: an earlier step, or a query expression of
// the model, whose connectors and native reads it brings along.
func (e *mEval) resolve(name string) mPath {
	if p, ok := e.steps[name]; ok {
		return p
	}
	r := e.m.query(name)
	if r == nil {
		return mPath{}
	}
	for c := range r.connectors {
		e.res.connectors[c] = true
	}
	for n := range r.native {
		e.res.native[n] = true
	}
	return r.final
}

// closer is the index of the bracket that closes the one at open.
func (e *mEval) closer(open, limit int) int {
	depth := 0
	for k := open; k < limit; k++ {
		switch e.toks[k].Kind {
		case exprlex.Open:
			depth++
		case exprlex.Close:
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return limit
}

// args splits the tokens between the bracket at open and close into its
// comma-separated items, as [start, end) ranges.
func (e *mEval) args(open, end int) [][2]int {
	var out [][2]int
	start := open + 1
	depth := 0
	for k := open + 1; k < end; k++ {
		switch t := e.toks[k]; {
		case t.Kind == exprlex.Open:
			depth++
		case t.Kind == exprlex.Close:
			depth--
		case t.Kind == exprlex.Comma && depth == 0:
			out = append(out, [2]int{start, k})
			start = k + 1
		}
	}
	if start < end {
		out = append(out, [2]int{start, end})
	}
	return out
}

// libraryNamespaces are M's standard library: a call into one transforms
// data, it does not read a source. Csv, Json, Xml and Lines parse what a
// connector read.
var libraryNamespaces = map[string]bool{
	"table": true, "list": true, "text": true, "record": true, "number": true, "date": true,
	"datetime": true, "datetimezone": true, "duration": true, "time": true, "value": true,
	"binary": true, "binaryformat": true, "lines": true, "splitter": true, "combiner": true,
	"comparer": true, "replacer": true, "expression": true, "function": true, "type": true,
	"logical": true, "byte": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"single": true, "double": true, "decimal": true, "currency": true, "percentage": true,
	"character": true, "uri": true, "cube": true, "diagnostics": true, "error": true,
	"action": true, "culture": true, "guid": true, "csv": true, "json": true, "xml": true,
	"variable": true, "rowexpression": true, "itemexpression": true,
}

// connector reports whether a called function reads a data source
// (Sql.Database, Snowflake.Databases, Web.Contents, ...), and its name
// printable: a dotted name outside the standard library.
func connector(fn string) (string, bool) {
	parts := strings.Split(fn, ".")
	if len(parts) < 2 || libraryNamespaces[strings.ToLower(parts[0])] {
		return "", false
	}
	for _, p := range parts {
		if shape.IdentifierPart(p) != p {
			return "", false
		}
	}
	return fn, true
}
