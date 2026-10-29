package pbir

import (
	"slices"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/shape"
)

// Description is the structure of one report JSON file, as explain prints
// it (docs/design/explain.md): each context, with how many string literals
// it holds and its column names. It holds names and counts, never a value.
type Description struct {
	Contexts []Context `json:"contexts"`
}

// Context is one data-bearing context of the file.
type Context struct {
	Context string `json:"context"`
	// Strings is how many string literals the context holds.
	Strings int `json:"strings"`
	// Columns are its columns as table.column (or the bare column when the
	// table is not known), sorted, each once. A name with a personal-data
	// shape is shape.Withheld.
	Columns []string `json:"columns"`
}

// Describe describes one report JSON file. A file that does not parse
// returns the *judge.ParseError Scan would.
func Describe(data []byte) (*Description, error) {
	root, err := decode(data)
	if err != nil {
		return nil, err
	}
	doc, ok := root.(object)
	if !ok {
		return nil, &judge.ParseError{Reason: finding.UnparseableJSON, Line: 1, Msg: "the document is not an object"}
	}
	d := &Description{Contexts: []Context{}}
	for _, c := range contexts(doc) {
		ctx := Context{Context: string(c.ctx), Columns: []string{}}
		for _, lit := range c.literals {
			if lit.kind == judge.String {
				ctx.Strings++
			}
		}
		for _, col := range c.resolved() {
			name := shape.ObjectName(col.Name)
			if col.Table != "" {
				name = shape.ObjectName(col.Table) + "." + name
			}
			ctx.Columns = append(ctx.Columns, name)
		}
		slices.Sort(ctx.Columns)
		ctx.Columns = slices.Compact(ctx.Columns)
		d.Contexts = append(d.Contexts, ctx)
	}
	return d, nil
}
