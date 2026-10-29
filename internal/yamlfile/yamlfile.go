// Package yamlfile scans YAML files, dbt's project and test files among
// them (docs/design/behavior.md, "YAML files").
//
// Every key is judged on shape. Every scalar except nulls, booleans and
// empty strings is judged, a number by its column only, and is associated
// with its nearest mapping key, through sequences. dbt's conventions add to
// that: an accepted_values test's values are also tied to the column the
// test is on, where and expression values are lexed as SQL, and unit-test
// rows are tied to nothing. Comments and the quoted runs inside values are
// judged as free text. An anchored node is walked once, and every document
// of a file is scanned. A key or column name is judged as written, and a
// finding prints it as {?} unless shape.PrintableKey allows it.
//
// The parser is go.yaml.in/yaml/v3, through its Node API. Its error
// messages can quote the source, so none of one is kept: a file that does
// not parse is reported by its line only.
package yamlfile

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/shape"
	"github.com/craigalodon/pbipx/internal/sqlfile"
)

var _ judge.Scanner = Scan

// Scan judges every key, scalar and comment of one YAML file. A file that
// does not parse, or holds a SQL value that does not lex, returns a
// *judge.ParseError (unparseable-yaml) with its line, and no findings.
func Scan(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	docs, err := parse(data)
	if err != nil {
		return nil, err
	}
	w := &walker{path: path, p: p}
	for i, doc := range docs {
		base := ""
		if i > 0 {
			base = "doc[" + strconv.Itoa(i) + "]"
		}
		w.node(doc, state{path: base})
	}
	if w.err != nil {
		return nil, w.err
	}
	for i, c := range comments(string(data)) {
		loc := "comment[" + strconv.Itoa(i) + "]"
		w.judge(finding.YAMLComment, sqlfile.Value{Text: c.text, Kind: judge.String, Locator: loc})
		for _, r := range sqlfile.Readings(c.text, false, loc) {
			w.judge(finding.YAMLComment, r)
		}
	}
	finding.Sort(w.out)
	return w.out, nil
}

// errLine is the line in the parser's message. Nothing else of the message
// is kept.
var errLine = regexp.MustCompile(`^yaml: line ([0-9]+):`)

// parse decodes every document of data.
func parse(data []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			pe := &judge.ParseError{Reason: finding.UnparseableYAML, Msg: "the YAML does not parse"}
			if m := errLine.FindStringSubmatch(err.Error()); m != nil {
				pe.Line, _ = strconv.Atoi(m[1])
			}
			return nil, pe
		}
		docs = append(docs, &n)
	}
}

// state is what a node inherits from the nodes above it.
type state struct {
	path string // the locator: keys that shape.PrintableKey allows, or {?}
	// key is the nearest scalar mapping key, which names the column the
	// node is tied to. Sequences pass it through. It is judged as written;
	// a finding prints it only if shape.PrintableKey allows it.
	key string
	// untied is set inside unit-test rows, which are tied to nothing.
	untied bool
	// column is the dbt column a test is on: the name of the columns[] item
	// the node is under.
	column string
	// accepted is set inside an accepted_values test, to the column its
	// values are tied to ("" when none is known); inAccepted says whether
	// the node is in one.
	accepted   string
	inAccepted bool
	// values is set under an accepted_values test's values key.
	values bool
	// unitTests is set under the top-level unit_tests key.
	unitTests bool
	// root is set for a document's top node.
	root bool
}

type walker struct {
	path string
	p    *policy.Policy
	out  []finding.Finding
	// err is the first SQL value that does not lex. The file then has not
	// been vetted, and is reported by this error alone.
	err *judge.ParseError
}

func (w *walker) judge(ctx finding.Context, v sqlfile.Value) {
	if verdict, ok := v.Judge(w.p); ok {
		w.out = append(w.out, verdict.Finding(w.path, ctx, v.Locator, v.Text))
	}
}

// node walks one node. An alias is not followed: the node it names is
// walked where it is anchored, once.
func (w *walker) node(n *yaml.Node, s state) {
	switch n.Kind {
	case yaml.DocumentNode:
		s.root = true
		for _, c := range n.Content {
			w.node(c, s)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			cs := s
			cs.path = index(s.path, i)
			w.node(c, cs)
		}
	case yaml.MappingNode:
		w.mapping(n, s)
	case yaml.ScalarNode:
		w.scalar(n, s)
	}
}

// mapping walks a mapping's keys and values.
func (w *walker) mapping(n *yaml.Node, s state) {
	if s.key == "columns" && !s.untied {
		// An item of a model's columns: the tests below it are on its name.
		if name := scalarOf(n, "name"); name != "" {
			s.column = name
		}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		cs := s
		cs.values, cs.root = false, false
		if k.Kind != yaml.ScalarNode {
			// A complex key: walked as a value tied to nothing.
			ks := s
			ks.path, ks.key = join(s.path, ""), ""
			w.node(k, ks)
			cs.path, cs.key = join(s.path, ""), ""
		} else {
			w.key(k, s)
			cs.path = join(s.path, k.Value)
			cs.key = ""
			if !s.untied {
				cs.key = k.Value
			}
		}
		name := ""
		if k.Kind == yaml.ScalarNode {
			name = k.Value
		}
		switch {
		case name == "unit_tests" && s.root:
			cs.unitTests = true
		case name == "accepted_values" && v.Kind == yaml.MappingNode && !s.untied:
			cs.inAccepted, cs.accepted = true, acceptedColumn(v, s.column)
		case s.inAccepted && name == "arguments":
			// dbt's newer form: accepted_values: {arguments: {values: [...]}}
		case s.inAccepted && name == "values":
			cs.values = true
		default:
			cs.inAccepted, cs.accepted = false, ""
		}
		if s.unitTests && name == "rows" {
			w.rows(v, cs, scalarOf(n, "format"))
			continue
		}
		w.node(v, cs)
		if (name == "where" || name == "expression") && isString(v) {
			w.sql(v, cs.path, finding.YAMLSQL, false)
		}
	}
}

// key judges a mapping key on shape.
func (w *walker) key(k *yaml.Node, s state) {
	kind, ok := scalarKind(k)
	if !ok {
		return
	}
	w.judge(finding.YAMLKey, sqlfile.Value{Text: k.Value, Kind: kind, Locator: join(s.path, k.Value) + "#key"})
}

// scalar judges a scalar value: whole, with its columns, and for the quoted
// runs inside it.
func (w *walker) scalar(n *yaml.Node, s state) {
	kind, ok := scalarKind(n)
	if !ok {
		return
	}
	var cols []finding.Column
	if !s.untied {
		if s.key != "" {
			cols = append(cols, finding.Column{Name: s.key})
		}
		if s.values && s.accepted != "" && s.accepted != s.key {
			cols = append(cols, finding.Column{Name: s.accepted})
		}
	}
	w.judge(finding.YAML, sqlfile.Value{Text: n.Value, Kind: kind, Columns: cols, Locator: s.path})
	if kind == judge.String {
		for _, r := range sqlfile.Readings(n.Value, false, s.path) {
			w.judge(finding.YAML, r)
		}
	}
}

// sql judges a SQL-valued string: its literals with their columns, and its
// comments. untied drops every column, for unit-test rows. A value that
// does not lex has not been vetted: the file is then reported by one
// unparseable-yaml error at the value's line, and nothing else (principle
// 10).
func (w *walker) sql(n *yaml.Node, path string, ctx finding.Context, untied bool) {
	vals, err := sqlfile.Values(n.Value)
	if err != nil {
		if w.err == nil {
			w.err = &judge.ParseError{Reason: finding.UnparseableYAML, Line: n.Line, Msg: "a SQL value does not lex"}
		}
		return
	}
	for _, v := range vals {
		v.Locator = path + "#" + v.Locator
		if untied {
			if v.Comment && len(v.Columns) > 0 {
				continue // a comment's reading kept only for its columns
			}
			v.Columns = nil
		}
		w.judge(ctx, v)
	}
}

// rows judges a unit test's rows, tied to nothing: given as YAML (dict), as
// a CSV string cell by cell, or as a SQL string.
func (w *walker) rows(v *yaml.Node, s state, format string) {
	s.untied, s.key, s.inAccepted, s.values = true, "", false, false
	if !isString(v) {
		w.node(v, s)
		return
	}
	if strings.EqualFold(format, "sql") {
		w.sql(v, s.path, finding.YAML, true)
		return
	}
	for r, rec := range csvRecords(v.Value) {
		for c, cell := range rec {
			loc := s.path + "#cell[" + strconv.Itoa(r) + "][" + strconv.Itoa(c) + "]"
			w.judge(finding.YAML, sqlfile.Value{Text: strings.TrimSpace(cell), Kind: judge.String, Locator: loc})
		}
	}
}

// csvRecords splits CSV text into records, leniently: a malformed quote is
// read as text, and a record may have any number of fields. Text that still
// does not read as CSV is split at line breaks and commas.
func csvRecords(text string) [][]string {
	r := csv.NewReader(strings.NewReader(text))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	recs, err := r.ReadAll()
	if err == nil {
		return recs
	}
	recs = nil
	for _, line := range strings.Split(text, "\n") {
		recs = append(recs, strings.Split(line, ","))
	}
	return recs
}

// acceptedColumn is the column an accepted_values test's values are tied
// to: its column_name (directly, or under arguments), else the column the
// test is on.
func acceptedColumn(test *yaml.Node, column string) string {
	name := scalarOf(test, "column_name")
	if name == "" {
		if args := valueOf(test, "arguments"); args != nil && args.Kind == yaml.MappingNode {
			name = scalarOf(args, "column_name")
		}
	}
	if name != "" {
		return name
	}
	return column
}

// valueOf is the value of a mapping's key, or nil.
func valueOf(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// scalarOf is the string value of a mapping's key, or "".
func scalarOf(m *yaml.Node, key string) string {
	if v := valueOf(m, key); v != nil && isString(v) {
		return v.Value
	}
	return ""
}

func isString(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str"
}

// scalarKind is how a scalar is judged, and false for one that is not:
// a null, a boolean or an empty string. Integers and floats are numbers.
func scalarKind(n *yaml.Node) (judge.Kind, bool) {
	if n.Kind != yaml.ScalarNode || n.Value == "" {
		return judge.String, false
	}
	switch n.ShortTag() {
	case "!!null", "!!bool":
		return judge.String, false
	case "!!int", "!!float":
		return judge.Number, true
	}
	return judge.String, true
}

// join appends a mapping key to a locator. A key that shape.PrintableKey
// refuses is written as {?}, so a locator, and the fingerprint that hashes
// it, never carries text that is not schema.
func join(path, key string) string {
	if !shape.PrintableKey(key) {
		key = "{?}"
	}
	if path == "" {
		return key
	}
	return path + "." + key
}

// index appends a sequence index to a locator.
func index(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}
