package exprlex

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// Every value here is synthetic (CONTRIBUTING.md#synthetic-data).

func TestSQLDollarQuotedStrings(t *testing.T) {
	sql := func(s string) ([]Token, []Comment, error) { return SQL(s, QuoteString) }
	runLex(t, "SQL", sql, []lexCase{
		{"empty tag", `select $$Doe, Jane$$ as a`, []string{"Ident:select", "Str:Doe, Jane", "Ident:as", "Col:a"}},
		{"named tag", `$x$it's -- not a comment$x$`, []string{"Str:it's -- not a comment"}},
		{"other tags inside", `$a$ $$ $b$ $a$`, []string{"Str: $$ $b$ "}},
		{"parameter", `where a = $1`, []string{"Ident:where", "Col:a", "Op:=", "Num:1"}},
		{"name with a dollar", `a$b$ = 1`, []string{"Col:a", "Col:b", "Op:=", "Num:1"}},
	})
	_, _, err := SQL("where a = $tag$"+sentinel+"$ta$", QuoteString)
	var le *LexError
	if !errors.As(err, &le) || le.Msg != msgDollar || strings.Contains(err.Error(), "Doe") {
		t.Errorf("unterminated: got %v", err)
	}
	if _, _, err := SQLSites("$$" + sentinel); err == nil || strings.Contains(err.Error(), "Doe") {
		t.Errorf("SQLSites: got %v", err)
	}
}

func TestSQLSitesBackslashEscapes(t *testing.T) {
	runAssoc(t, "SQL both", []assocCase{
		// Under the first reading the escaped quotes shift every literal; the
		// backslash reading finds each one.
		{"shifted literals", `select 'O\'Brien' as a, 'Doe, Jane' as b, 'D\'Arcy' as c`,
			[]string{`O\`, " as a, ", " as b, ", "Arcy", "O'Brien", "Doe, Jane", "D'Arcy"}},
		{"escaped quote in a compared value", `where full_name = 'O\'Doe, Jane'`,
			[]string{"O'Doe, Jane=full_name"}},
		{"escaped double quote", `where nickname = "Doe \"Jay\""`, []string{`Doe \=nickname`, "", `Doe "Jay"=nickname`}},
		// The first reading's sites keep their ordinals; a site only the
		// backslash reading finds comes after them.
		{"order", `select 'a\\' as p, 'b' as q`, []string{`a\\`, "b", `a\`}},
		{"other pairs kept as written", `where code rlike '\d+'`, []string{`\d+=code`}},
	})
	if _, _, err := SQLSites(`select 'it\'s'`); err != nil {
		t.Errorf("a file the backslash reading lexes: %v", err)
	}
}

func TestSQLSitesHashComments(t *testing.T) {
	sites, coms, err := SQLSites("select 1 # Doe, Jane\nfrom t -- note\nwhere a = 'x' # full_name = 'Roe, Richard'")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range coms {
		got = append(got, c.Text)
		if c.Extra != (c.Text != "note") {
			t.Errorf("%q: extra = %v", c.Text, c.Extra)
		}
	}
	want := []string{"note", "Doe, Jane", "full_name = 'Roe, Richard'"}
	if !slices.Equal(got, want) {
		t.Errorf("comments = %q, want %q", got, want)
	}
	// The first reading still lexes # as an operator, and its literal is
	// kept.
	if d := describe(sites); !slices.Equal(d, []string{"#1", "x=a", "Roe, Richard=full_name"}) {
		t.Errorf("sites = %q", d)
	}
	// A # inside a string is not a comment under any reading.
	if _, coms, _ := SQLSites("select '# Doe, Jane'"); len(coms) != 0 {
		t.Errorf("comments = %+v", coms)
	}
}

func TestSQLSitesLexErrorOnlyWhenNoReadingLexes(t *testing.T) {
	for _, src := range []string{
		"'" + sentinel,
		`'` + sentinel + `\`,
		"select 1 # x\n'" + sentinel,
	} {
		_, _, err := SQLSites(src)
		var le *LexError
		if !errors.As(err, &le) || le.Msg != msgString || strings.Contains(err.Error(), "Doe") {
			t.Errorf("%q: got %v", src, err)
		}
	}
}

func TestSQLCode(t *testing.T) {
	src := "select a -- b\n, 'c' # d\nfrom $$e$$"
	code, err := SQLCode(src)
	if err != nil {
		t.Fatal(err)
	}
	var kept strings.Builder
	for i := range src {
		if code[i] {
			kept.WriteByte(src[i])
		}
	}
	if got := strings.Join(strings.Fields(kept.String()), " "); got != "select a , from" {
		t.Errorf("code = %q", got)
	}
	if _, err := SQLCode("'" + sentinel); err == nil {
		t.Error("an unlexable text has no code")
	}
}

// adversarial is n bytes or so of 'a'=(x,'a'=(x, ... x)...): each literal's
// other side holds every column after it.
func adversarial(n int) string {
	k := n / len("'a'=(x,)")
	return "select 1 where " + strings.Repeat("'a'=(x,", k) + "x" + strings.Repeat(")", k)
}

func TestAssociationIsNearLinear(t *testing.T) {
	src := adversarial(256 << 10)
	start := time.Now()
	sites, _, err := SQLSites(src)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites[1:] { // after the 1 of select 1
		if len(s.Columns) != 1 || s.Columns[0].Name != "x" {
			t.Fatalf("site %d: columns %v", s.Index, s.Columns)
		}
	}
	// Quadratic association took 2.8s and 589MB on a quarter of this; the
	// benchmark below takes about 150ms. The race detector slows it tenfold.
	if !raceEnabled && elapsed > time.Second {
		t.Errorf("256KB took %v", elapsed)
	}
}

func BenchmarkSQLSitesAdversarial(b *testing.B) {
	src := adversarial(256 << 10)
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		if _, _, err := SQLSites(src); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAssociationLimits(t *testing.T) {
	cols := func(n int) string {
		names := make([]string, n)
		for i := range names {
			names[i] = "c" + strings.Repeat("x", i)
		}
		return strings.Join(names, ", ")
	}
	tooLarge := func(name, src string) {
		t.Helper()
		start := time.Now()
		_, _, err := SQLSites(src)
		var le *LexError
		if !errors.As(err, &le) || le.Msg != MsgTooLarge || strings.Contains(err.Error(), "Doe") {
			t.Errorf("%s: got %v", name, err)
		}
		if d := time.Since(start); !raceEnabled && d > time.Second {
			t.Errorf("%s: took %v", name, d)
		}
	}
	// One literal may take MaxColumnsPerLiteral distinct columns; one more
	// is an error, not a literal judged on some of them.
	src := "where '" + sentinel + "' = coalesce(" + cols(MaxColumnsPerLiteral) + ")"
	sites, _, err := SQLSites(src)
	if err != nil || len(sites[0].Columns) != MaxColumnsPerLiteral {
		t.Fatalf("at the limit: %v", err)
	}
	tooLarge("columns", "where '"+sentinel+"' = coalesce("+cols(MaxColumnsPerLiteral+1)+")")
	// Nested tuples make IN quadratic: past the work budget it is an error.
	k := (256 << 10) / len("x in (('a',))")
	tooLarge("work", "select 1 where "+strings.Repeat("x in (('"+sentinel+"',", k)+"'a'"+strings.Repeat("))", k))
	// Associate (unchecked) falls back to every literal with no columns.
	toks, _, err := SQL("where '"+sentinel+"' = coalesce("+cols(MaxColumnsPerLiteral+1)+")", QuoteString)
	if err != nil {
		t.Fatal(err)
	}
	if s := Associate(toks, "||"); len(s) != 1 || s[0].Columns != nil {
		t.Errorf("fallback: %v", describe(s))
	}
}
