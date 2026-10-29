package exprlex

import (
	"slices"
	"strings"
	"testing"
)

// Every value here is synthetic (CONTRIBUTING.md#synthetic-data).

func describeFree(vs []FreeValue) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		d := v.Suffix + ":" + v.Value
		if v.Number {
			d = v.Suffix + ":#" + v.Value
		}
		if len(v.Columns) > 0 {
			cols := make([]string, len(v.Columns))
			for k, c := range v.Columns {
				cols[k] = c.String()
			}
			d += "=" + strings.Join(cols, ",")
		}
		out[i] = d
	}
	return out
}

func TestFreeText(t *testing.T) {
	cases := []struct {
		name string
		text string
		sql  bool
		want []string
	}{
		{"straight double quotes", `kept for "Doe, Jane" until the next term`, false, []string{".quoted0:Doe, Jane"}},
		{"curly double quotes", `excludes the test row “Example, Pat” from the extract`, false, []string{".quoted0:Example, Pat"}},
		{"curly single quotes", `ask ‘Roe, Richard’ first`, false, []string{".quoted0:Roe, Richard"}},
		{"to the end of the line", "owner \"Doe, Jane\nnext line", false, []string{".quoted0:Doe, Jane"}},
		{"ordinal counts every run", `"North" or "Doe, Jane"`, false, []string{".quoted1:Doe, Jane"}},
		{"apostrophe in a word opens nothing", `Roe's note, see 'Doe, Jane'`, false, []string{".quoted0:Doe, Jane"}},
		{"apostrophe inside a name", `'O'Doe, Jane'`, false, []string{".quoted0:O'Doe, Jane"}},
		{"not a name", `"Totals by region" only`, false, nil},
		{"anchored at the run start", `"the Doe, Jane row"`, false, nil},
		{"no SQL reading unless asked", `where first_name = 'Jane'`, false, nil},

		{"commented-out filter", `where first_name = 'Jane'`, true, []string{".compared0:Jane=first_name"}},
		{"qualified", `and student.last_name = 'Doe'`, true, []string{".compared0:Doe=student.last_name"}},
		{"number", `where student_id = 7041293`, true, []string{".compared0:#7041293=student_id"}},
		{"quoted and compared once", `where full_name = 'Doe, Jane'`, true, []string{".compared0:Doe, Jane=full_name"}},
		{"quoted, compared elsewhere", `"Roe, Richard" asked; where full_name = 'Doe, Jane'`, true,
			[]string{".quoted0:Roe, Richard", ".compared0:Doe, Jane=full_name"}},
		{"prose apostrophes blanked", `Doe's request: where first_name = 'Jane'`, true, []string{".compared0:Jane=first_name"}},
		{"line by line", "it's 'unbalanced\nwhere last_name = 'Roe'", true, []string{".compared0:Roe=last_name"}},
		{"double quotes as a column", `where "full_name" = 'Example, Pat'`, true,
			[]string{".compared0:Example, Pat=full_name"}},
		{"double quotes as a string", `where nickname = "Doe, Jane"`, true, []string{".compared0:Doe, Jane=nickname"}},
		{"unassociated literal is not compared", `select 'North' from t`, true, nil},
		{"prose", `Roe, Richard asked for this extract`, true, nil},
	}
	for _, c := range cases {
		if got := describeFree(FreeText(c.text, c.sql)); !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// Native SQL inside M: the M string is lexed as SQL with its double quotes
// skipped, and a comment in it is read as free text.
func TestNativeSQLInM(t *testing.T) {
	toks, _, err := M(`Value.NativeQuery(Source, "select student_id from enrollment.student where student.status = 'Active' /* and student.last_name = 'Doe' */")`)
	if err != nil {
		t.Fatal(err)
	}
	var query string
	for _, tok := range toks {
		if tok.Kind == Str && LooksLikeSQL(tok.Text) {
			query = tok.Text
		}
	}
	sqlToks, coms, err := SQL(query, QuoteSkip)
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(mustAssociate(t, sqlToks, "||")); !slices.Equal(got, []string{"Active=student.status"}) {
		t.Errorf("sites %q", got)
	}
	if len(coms) != 1 {
		t.Fatalf("comments %d", len(coms))
	}
	if got := describeFree(FreeText(coms[0].Text, true)); !slices.Equal(got, []string{".compared0:Doe=student.last_name"}) {
		t.Errorf("free text %q", got)
	}
}
