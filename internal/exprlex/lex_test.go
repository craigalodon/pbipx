package exprlex

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Every value here is synthetic (CONTRIBUTING.md#synthetic-data).

// render writes tokens compactly: Kind:Text, with a column's table as
// Col:table.name.
func render(toks []Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		text := t.Text
		if t.Table != "" {
			text = t.Table + "." + text
		}
		out[i] = t.Kind.String() + ":" + text
	}
	return out
}

type lexCase struct {
	name string
	src  string
	want []string
}

func runLex(t *testing.T, lang string, lex func(string) ([]Token, []Comment, error), cases []lexCase) {
	t.Helper()
	for _, c := range cases {
		toks, _, err := lex(c.src)
		if err != nil {
			t.Errorf("%s: %s: unexpected error %v", lang, c.name, err)
			continue
		}
		if got := render(toks); !slices.Equal(got, c.want) {
			t.Errorf("%s: %s:\n got %q\nwant %q", lang, c.name, got, c.want)
		}
	}
}

func TestDAX(t *testing.T) {
	runLex(t, "DAX", DAX, []lexCase{
		{"string with doubled quote", `"say ""hi"""`, []string{`Str:say "hi"`}},
		{"quoted table is a name", `COUNTROWS('Enrollment, Current')`,
			[]string{"Ident:COUNTROWS", "Open:(", "Ident:Enrollment, Current", "Close:)"}},
		{"bare column", `[full_name]`, []string{"Col:full_name"}},
		{"table column adjacent", `student[full_name]`, []string{"Col:student.full_name"}},
		{"quoted table column adjacent", `'student roster'[full_name]`, []string{"Col:student roster.full_name"}},
		{"not adjacent keeps the name", `student [full_name]`, []string{"Ident:student", "Col:full_name"}},
		{"escaped bracket", `[a]]b]`, []string{"Col:a]b"}},
		{"numbers", `1 2.5 .5 3e4 1.5E-3`, []string{"Num:1", "Num:2.5", "Num:.5", "Num:3e4", "Num:1.5E-3"}},
		{"operators", `a = b == c <> d <= e >= f && g || h & i`, []string{
			"Ident:a", "Op:=", "Ident:b", "Op:==", "Ident:c", "Op:<>", "Ident:d", "Op:<=", "Ident:e",
			"Op:>=", "Ident:f", "Op:&&", "Ident:g", "Op:||", "Ident:h", "Op:&", "Ident:i"}},
		{"dotted function", `PERCENTILE.INC(x, 0.5)`,
			[]string{"Ident:PERCENTILE.INC", "Open:(", "Ident:x", "Comma:,", "Num:0.5", "Close:)"}},
		{"comments are not tokens", "a // x\n-- y\n/* z */ b", []string{"Ident:a", "Ident:b"}},
		{"unknown punctuation skipped", `a ; ~ b`, []string{"Ident:a", "Ident:b"}},
		{"braces", `{"a", 1}`, []string{"Open:{", "Str:a", "Comma:,", "Num:1", "Close:}"}},
	})
}

func TestM(t *testing.T) {
	runLex(t, "M", M, []lexCase{
		{"string", `"a ""b"""`, []string{`Str:a "b"`}},
		{"quoted identifier is a name", `#"Changed Type"`, []string{"Ident:Changed Type"}},
		{"hash keyword", `#date(2101, 1, 2)`, []string{"Ident:#date", "Open:(", "Num:2101", "Comma:,", "Num:1", "Comma:,", "Num:2", "Close:)"}},
		{"field access", `[status]`, []string{"Col:status"}},
		{"field with spaces", `[First Name]`, []string{"Col:First Name"}},
		{"quoted field", `[#"a b"]`, []string{"Col:a b"}},
		{"optional field", `[status]?`, []string{"Col:status"}},
		{"record is not a column", `[Name = "x"]`, []string{"Open:[", "Ident:Name", "Op:=", "Str:x", "Close:]"}},
		{"navigation", `Source{[Schema = "s", Item = "t"]}[Data]`, []string{
			"Ident:Source", "Open:{", "Open:[", "Ident:Schema", "Op:=", "Str:s", "Comma:,",
			"Ident:Item", "Op:=", "Str:t", "Close:]", "Close:}", "Col:Data"}},
		{"empty record", `[]`, []string{"Open:[", "Close:]"}},
		{"projection", `t[[a], [b]]`, []string{"Ident:t", "Open:[", "Col:a", "Comma:,", "Col:b", "Close:]"}},
		{"type names", `{{"full_name", type text}, {"student_id", Int64.Type}}`, []string{
			"Open:{", "Open:{", "Str:full_name", "Comma:,", "Ident:type", "Ident:text", "Close:}", "Comma:,",
			"Open:{", "Str:student_id", "Comma:,", "Ident:Int64.Type", "Close:}", "Close:}"}},
		{"lambda arrow is not a comparison", `(x) => x`, []string{"Open:(", "Ident:x", "Close:)", "Op:=>", "Ident:x"}},
		{"comments", "a // x\n/* y */ b", []string{"Ident:a", "Ident:b"}},
	})
}

func TestSQL(t *testing.T) {
	sql := func(dq DoubleQuote) func(string) ([]Token, []Comment, error) {
		return func(s string) ([]Token, []Comment, error) { return SQL(s, dq) }
	}
	runLex(t, "SQL", sql(QuoteString), []lexCase{
		{"string with doubled quote", `'O''Doe'`, []string{"Str:O'Doe"}},
		{"keywords and columns", `select full_name from student where status = 'A'`, []string{
			"Ident:select", "Col:full_name", "Ident:from", "Col:student", "Ident:where", "Col:status", "Op:=", "Str:A"}},
		{"function is not a column", `lower (email)`, []string{"Ident:lower", "Open:(", "Col:email", "Close:)"}},
		{"qualified column", `analytics.student.first_name`, []string{"Col:student.first_name"}},
		{"backticked column", "`full name`", []string{"Col:full name"}},
		{"backticked qualified", "`s`.`full_name`", []string{"Col:s.full_name"}},
		{"double quote as string", `"Doe"`, []string{"Str:Doe"}},
		{"keyword in any case", `SELECT x FROM t`, []string{"Ident:SELECT", "Col:x", "Ident:FROM", "Col:t"}},
		{"concatenation", `a || b`, []string{"Col:a", "Op:||", "Col:b"}},
		{"star kept, casts skipped", `s.*, x::int`, []string{"Col:s", "Op:*", "Comma:,", "Col:x", "Col:int"}},
	})
	runLex(t, "SQL ident", sql(QuoteIdent), []lexCase{
		{"double quote as column", `"full_name" = 'x'`, []string{"Col:full_name", "Op:=", "Str:x"}},
		{"double quote qualified", `"s"."full_name"`, []string{"Col:s.full_name"}},
	})
	runLex(t, "SQL skip", sql(QuoteSkip), []lexCase{
		{"quotes skipped, words lexed", `"full_name" = 'x'`, []string{"Col:full_name", "Op:=", "Str:x"}},
	})
}

func TestLines(t *testing.T) {
	toks, coms, err := DAX("a\n/* one\ntwo */\n\"x\"\n// tail")
	if err != nil {
		t.Fatal(err)
	}
	if got := []int{toks[0].Line, toks[1].Line}; !slices.Equal(got, []int{1, 4}) {
		t.Errorf("token lines %v", got)
	}
	if len(coms) != 2 || coms[0].Line != 2 || coms[0].Text != "one\ntwo" || coms[1].Line != 5 || coms[1].Text != "tail" {
		t.Errorf("comments %+v", coms)
	}
}

func TestComments(t *testing.T) {
	cases := []struct {
		lang string
		src  string
		want []string
	}{
		{"DAX", "x // Roe, Richard asked \n -- two\n/*  three  */", []string{"Roe, Richard asked", "two", "three"}},
		{"M", "x // one\n/* two */ -- not a comment", []string{"one", "two"}},
		{"SQL", "-- one\nselect /* two */ 1 // not a comment", []string{"one", "two"}},
	}
	for _, c := range cases {
		var coms []Comment
		var err error
		switch c.lang {
		case "DAX":
			_, coms, err = DAX(c.src)
		case "M":
			_, coms, err = M(c.src)
		case "SQL":
			_, coms, err = SQL(c.src, QuoteString)
		}
		if err != nil {
			t.Fatalf("%s: %v", c.lang, err)
		}
		var got []string
		for _, cm := range coms {
			got = append(got, cm.Text)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.lang, got, c.want)
		}
	}
}

// sentinel is planted in every malformed input: no error message may hold
// it, or any part of it.
const sentinel = "Doe, Jane"

func TestLexErrors(t *testing.T) {
	cases := []struct {
		name string
		lex  func(string) ([]Token, []Comment, error)
		src  string
		line int
		msg  string
	}{
		{"DAX string", DAX, "x =\n\"" + sentinel, 2, msgString},
		{"DAX quoted name", DAX, "'" + sentinel, 1, msgName},
		{"DAX column", DAX, "t[" + sentinel, 1, msgColumn},
		{"DAX block comment", DAX, "a\n\n/* " + sentinel, 3, msgBlockComment},
		{"M string", M, `"` + sentinel, 1, msgString},
		{"M quoted name", M, `#"` + sentinel, 1, msgName},
		{"M column", M, "[" + strings.ReplaceAll(sentinel, ",", "") + " x", 1, msgColumn},
		{"M quoted column", M, `[#"` + sentinel, 1, msgName},
		{"M block comment", M, "/* " + sentinel, 1, msgBlockComment},
		{"SQL string", func(s string) ([]Token, []Comment, error) { return SQL(s, QuoteString) }, "where a =\n'" + sentinel, 2, msgString},
		{"SQL double quote as string", func(s string) ([]Token, []Comment, error) { return SQL(s, QuoteString) }, `"` + sentinel, 1, msgString},
		{"SQL double quote as name", func(s string) ([]Token, []Comment, error) { return SQL(s, QuoteIdent) }, `"` + sentinel, 1, msgName},
		{"SQL backtick", func(s string) ([]Token, []Comment, error) { return SQL(s, QuoteSkip) }, "`" + sentinel, 1, msgName},
		{"SQL block comment", func(s string) ([]Token, []Comment, error) { return SQL(s, QuoteSkip) }, "/* " + sentinel, 1, msgBlockComment},
	}
	for _, c := range cases {
		_, _, err := c.lex(c.src)
		var le *LexError
		if !errors.As(err, &le) {
			t.Errorf("%s: got %v, want a LexError", c.name, err)
			continue
		}
		if le.Line != c.line || le.Msg != c.msg {
			t.Errorf("%s: got line %d %q, want line %d %q", c.name, le.Line, le.Msg, c.line, c.msg)
		}
		for _, part := range []string{"Doe", "Jane"} {
			if strings.Contains(err.Error(), part) || strings.Contains(fmt.Sprintf("%+v", le), part) {
				t.Errorf("%s: the error holds source text", c.name)
			}
		}
	}
	if _, _, err := SQLSites("'" + sentinel); err == nil || strings.Contains(err.Error(), "Doe") {
		t.Errorf("SQLSites: got %v", err)
	}
}

func TestQuoteSkipIgnoresUnbalancedQuotes(t *testing.T) {
	if _, _, err := SQL(`where "a = 'x'`, QuoteSkip); err != nil {
		t.Errorf("got %v", err)
	}
}
