// Package pbir scans report JSON (PBIR) for persisted values.
//
// A report holds model values in seven places, its contexts
// (docs/design/behavior.md, "Reports (PBIR JSON)"). In each context every
// literal and every column is collected at any depth, whatever the shape of
// the expression around them, and every literal is associated with every
// column in its context. Everything outside the contexts is not visited.
package pbir

import (
	"slices"
	"strconv"
	"strings"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/shape"
)

var _ judge.Scanner = Scan

// Scan judges every literal in the contexts of one report JSON file. A file
// that does not parse returns a *judge.ParseError and no findings.
func Scan(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	root, err := decode(data)
	if err != nil {
		return nil, err
	}
	doc, ok := root.(object)
	if !ok {
		// Every PBIR file is an object. Anything else is not a report this
		// scanner can place, and is never a clean file (principle 10).
		return nil, &judge.ParseError{Reason: finding.UnparseableJSON, Line: 1, Msg: "the document is not an object"}
	}
	var out []finding.Finding
	for _, c := range contexts(doc) {
		out = append(out, c.judge(path, p)...)
	}
	finding.Sort(out)
	return out, nil
}

// contexts finds every context in a document, in a fixed order.
func contexts(doc object) []*collector {
	var cs []*collector
	cs = append(cs, filterContexts(doc, "")...)
	if visual, ok := doc.getObject("visual"); ok {
		cs = append(cs, visualContexts(visual, "visual")...)
	}
	if state, ok := doc.getObject("explorationState"); ok {
		cs = append(cs, bookmarkContexts(state, "explorationState")...)
	}
	return cs
}

// filterContexts is one context per filterConfig.filters[i]. The walk meets
// the filter before the entry's own field, so the field's column comes last
// in association order.
func filterContexts(doc object, path string) []*collector {
	config, ok := doc.getObject("filterConfig")
	if !ok {
		return nil
	}
	filters, ok := config.get("filters")
	if !ok {
		return nil
	}
	arr, ok := filters.([]any)
	if !ok {
		return nil
	}
	base := join(join(path, "filterConfig"), "filters")
	var cs []*collector
	for i, f := range arr {
		entry, ok := f.(object)
		if !ok {
			continue
		}
		at := index(base, i)
		c := newCollector(finding.Filter)
		c.walkMembers(entry, at, "field")
		if field, ok := entry.get("field"); ok {
			c.walk(field, join(at, "field"))
		}
		cs = append(cs, c)
	}
	return cs
}

// visualContexts are the contexts under a visual: slicer selections,
// insight selectors and conditional formats in its objects, its expansion
// states, and its title and subtitle text.
func visualContexts(visual object, path string) []*collector {
	var cs []*collector
	if objects, ok := visual.getObject("objects"); ok {
		at := join(path, "objects")
		for _, m := range objects.sorted() {
			for i, item := range asArray(m.val) {
				entry, ok := item.(object)
				if !ok {
					continue
				}
				entryAt := index(join(at, m.key), i)
				cs = append(cs, objectContexts(m.key, entry, entryAt)...)
			}
		}
	}
	if states, ok := visual.get("expansionStates"); ok {
		c := newCollector(finding.ExpansionState)
		c.walk(states, join(path, "expansionStates"))
		cs = append(cs, c)
	}
	if vco, ok := visual.getObject("visualContainerObjects"); ok {
		at := join(path, "visualContainerObjects")
		for _, class := range []string{"title", "subTitle"} {
			items, _ := vco.get(class)
			for i, item := range asArray(items) {
				entry, ok := item.(object)
				if !ok {
					continue
				}
				props, ok := entry.getObject("properties")
				if !ok {
					continue
				}
				text, ok := props.get("text")
				if !ok {
					continue
				}
				c := newCollector(finding.TitleText)
				c.noColumns = true
				c.walk(text, join(join(index(join(at, class), i), "properties"), "text"))
				cs = append(cs, c)
			}
		}
	}
	return cs
}

// objectContexts are the contexts in one visual.objects.<class>[i] entry.
func objectContexts(class string, entry object, path string) []*collector {
	var cs []*collector
	if props, ok := entry.getObject("properties"); ok {
		propsAt := join(path, "properties")
		if f, ok := props.getObject("filter"); ok {
			if inner, ok := f.get("filter"); ok {
				c := newCollector(finding.SlicerSelection)
				c.walk(inner, join(join(propsAt, "filter"), "filter"))
				cs = append(cs, c)
			}
		}
		for _, m := range props.sorted() {
			cs = append(cs, conditionals(m.val, join(propsAt, m.key))...)
		}
	}
	if class == "insights" {
		if sel, ok := entry.get("selector"); ok {
			c := newCollector(finding.InsightSelector)
			c.walk(sel, join(path, "selector"))
			cs = append(cs, c)
		}
	}
	return cs
}

// conditionals finds every Conditional with Cases under v, each its own
// context. A Conditional inside one already found belongs to it.
func conditionals(v any, path string) []*collector {
	var cs []*collector
	switch n := v.(type) {
	case object:
		for _, m := range n.sorted() {
			at := join(path, m.key)
			if cond, ok := m.val.(object); ok && m.key == "Conditional" {
				if _, ok := cond.get("Cases"); ok {
					c := newCollector(finding.ConditionalFormat)
					c.walk(cond, at)
					cs = append(cs, c)
					continue
				}
			}
			cs = append(cs, conditionals(m.val, at)...)
		}
	case []any:
		for i, item := range n {
			cs = append(cs, conditionals(item, index(path, i))...)
		}
	}
	return cs
}

// bookmarkContexts splits a bookmark's state: each visual container of each
// section is its own context, and everything else is one more.
func bookmarkContexts(state object, path string) []*collector {
	rest := newCollector(finding.Bookmark)
	rest.walkMembers(state, path, "sections")
	var containers []*collector
	for _, m := range state.sorted() {
		if m.key != "sections" {
			continue
		}
		at := join(path, m.key)
		sections, ok := m.val.(object)
		if !ok {
			rest.walk(m.val, at)
			continue
		}
		for _, s := range sections.sorted() {
			sectionAt := join(at, s.key)
			section, ok := s.val.(object)
			if !ok {
				rest.walk(s.val, sectionAt)
				continue
			}
			rest.walkMembers(section, sectionAt, "visualContainers")
			for _, vc := range section.sorted() {
				if vc.key != "visualContainers" {
					continue
				}
				vcAt := join(sectionAt, vc.key)
				visuals, ok := vc.val.(object)
				if !ok {
					rest.walk(vc.val, vcAt)
					continue
				}
				for _, v := range visuals.sorted() {
					c := newCollector(finding.Bookmark)
					c.walk(v.val, join(vcAt, v.key))
					containers = append(containers, c)
				}
			}
		}
	}
	return append([]*collector{rest}, containers...)
}

// literal is one value to judge.
type literal struct {
	value   string
	kind    judge.Kind
	locator string
}

// columnRef is a column as written: its table is resolved after the walk,
// when every From in the context is known.
type columnRef struct {
	name   string
	entity string // SourceRef.Entity
	source string // SourceRef.Source, an alias
}

// collector gathers one context's literals, columns and aliases.
type collector struct {
	ctx       finding.Context
	noColumns bool // title text: shapes only
	literals  []literal
	columns   []columnRef
	aliases   map[string]string // From[].Name to Entity; the first wins
}

func newCollector(ctx finding.Context) *collector {
	return &collector{ctx: ctx, aliases: map[string]string{}}
}

// walk collects everything at any depth under v.
func (c *collector) walk(v any, path string) {
	switch n := v.(type) {
	case object:
		c.walkMembers(n, path, "")
	case []any:
		for i, item := range n {
			c.walk(item, index(path, i))
		}
	}
}

// walkMembers walks the members of o in key order, except those named skip.
func (c *collector) walkMembers(o object, path, skip string) {
	for _, m := range o.sorted() {
		if skip != "" && m.key == skip {
			continue
		}
		at := join(path, m.key)
		switch m.key {
		case "Literal":
			if lit, ok := m.val.(object); ok {
				if s, ok := lit.getString("Value"); ok {
					c.addLiteral(s, join(at, "Value"))
				}
			}
		case "Column":
			if col, ok := m.val.(object); ok {
				c.addColumn(col)
			}
		case "HierarchyLevel":
			if lvl, ok := m.val.(object); ok {
				c.addHierarchyLevel(lvl)
			}
		case "From":
			c.addAliases(m.val)
		case "cachedFilterDisplayItems":
			for i, item := range asArray(m.val) {
				if entry, ok := item.(object); ok {
					if s, ok := entry.getString("label"); ok {
						c.literals = append(c.literals, literal{value: s, kind: judge.String, locator: join(index(at, i), "label")})
					}
				}
			}
		}
		c.walk(m.val, at)
	}
}

// addLiteral keeps a quoted string, un-doubling its quotes, and the text of
// a datetime literal. Booleans, nulls, typed numbers and other typed
// literals are not judged (principle 6).
func (c *collector) addLiteral(s, locator string) {
	switch {
	case isQuoted(s, 0):
		c.literals = append(c.literals, literal{value: strings.ReplaceAll(s[1:len(s)-1], "''", "'"), kind: judge.String, locator: locator})
	case strings.HasPrefix(s, "datetime") && isQuoted(s, len("datetime")):
		c.literals = append(c.literals, literal{value: s[len("datetime")+1 : len(s)-1], kind: judge.TypedDate, locator: locator})
	}
}

// isQuoted reports whether s[from:] is a whole '...' literal.
func isQuoted(s string, from int) bool {
	q := s[from:]
	return len(q) >= 2 && q[0] == '\'' && q[len(q)-1] == '\''
}

func (c *collector) addColumn(col object) {
	name, ok := col.getString("Property")
	if !ok {
		return
	}
	ref := columnRef{name: name}
	if expr, ok := col.getObject("Expression"); ok {
		if src, ok := expr.getObject("SourceRef"); ok {
			ref.entity, _ = src.getString("Entity")
			ref.source, _ = src.getString("Source")
		}
	}
	c.columns = append(c.columns, ref)
}

// addHierarchyLevel associates a hierarchy level as a column of the
// hierarchy's table. A level is usually named after the column it shows, so
// a selection on it is judged as one on that column; a level named
// otherwise matches nothing, and the context's other columns still apply.
func (c *collector) addHierarchyLevel(lvl object) {
	name, ok := lvl.getString("Level")
	if !ok {
		return
	}
	ref := columnRef{name: name}
	if expr, ok := lvl.getObject("Expression"); ok {
		if h, ok := expr.getObject("Hierarchy"); ok {
			if hexpr, ok := h.getObject("Expression"); ok {
				if src, ok := hexpr.getObject("SourceRef"); ok {
					ref.entity, _ = src.getString("Entity")
					ref.source, _ = src.getString("Source")
				}
			}
		}
	}
	c.columns = append(c.columns, ref)
}

func (c *collector) addAliases(from any) {
	for _, item := range asArray(from) {
		entry, ok := item.(object)
		if !ok {
			continue
		}
		name, ok := entry.getString("Name")
		if !ok {
			continue
		}
		entity, ok := entry.getString("Entity")
		if !ok {
			continue
		}
		if _, seen := c.aliases[name]; !seen {
			c.aliases[name] = entity
		}
	}
}

// resolved is the context's columns in association order, with tables.
func (c *collector) resolved() []finding.Column {
	if c.noColumns {
		return nil
	}
	cols := make([]finding.Column, 0, len(c.columns))
	for _, ref := range c.columns {
		table := ref.entity
		if table == "" && ref.source != "" {
			table = c.aliases[ref.source]
		}
		cols = append(cols, finding.Column{Table: table, Name: ref.name})
	}
	return cols
}

// judge returns a finding for each literal in the context that fires.
func (c *collector) judge(path string, p *policy.Policy) []finding.Finding {
	cols := c.resolved()
	var out []finding.Finding
	for _, lit := range c.literals {
		if v, ok := judge.Literal(lit.value, lit.kind, cols, p); ok {
			out = append(out, v.Finding(path, c.ctx, lit.locator, lit.value))
		}
	}
	return out
}

func asArray(v any) []any {
	arr, _ := v.([]any)
	return arr
}

func sortMembers(ms []member) {
	slices.SortStableFunc(ms, func(a, b member) int { return strings.Compare(a.key, b.key) })
}

// join appends an object key to a locator. A key that shape.PrintableKey
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

// index appends an array index to a locator.
func index(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}
