package sqlfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/sqlfile"
)

const testPolicy = `version = 1
protected_roots = ["pbip"]
[scan]
sql = ["sql/**/*.sql", "pbip/**/*.sql"]
[columns]
block = ["full_name", "first_name", "student_id"]
warn  = ["cohort_year"]
[values]
allow = ["(none selected)"]
`

func loadPolicy(t *testing.T) *policy.Policy {
	t.Helper()
	path := filepath.Join(t.TempDir(), policy.FileName)
	if err := os.WriteFile(path, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

const testPath = "sql/models/student.sql"

func scanSQL(t *testing.T, src string) []finding.Finding {
	t.Helper()
	fs, err := sqlfile.Scan(testPath, []byte(src), loadPolicy(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return fs
}

// want is one expected finding; locator is compared when set.
type want struct {
	ctx     finding.Context
	column  string
	tier    finding.Tier
	reasons []finding.Reason
	locator string
}

var (
	blockCol = []finding.Reason{finding.BlockColumn}
	name     = []finding.Reason{finding.PersonNameShape}
	email    = []finding.Reason{finding.EmailShape}
)

func check(t *testing.T, got []finding.Finding, wants ...want) {
	t.Helper()
	if len(got) != len(wants) {
		t.Fatalf("got %d findings, want %d:\n%s", len(got), len(wants), describe(got))
	}
	for i, w := range wants {
		g := got[i]
		column := ""
		if g.Column != nil {
			column = g.Column.String()
		}
		if g.Context != w.ctx || column != w.column || g.Tier != w.tier || !slices.Equal(g.Reasons, w.reasons) {
			t.Errorf("finding %d = %s %q %s %v, want %s %q %s %v", i, g.Context, column, g.Tier, g.Reasons, w.ctx, w.column, w.tier, w.reasons)
		}
		if w.locator != "" && g.Locator != w.locator {
			t.Errorf("finding %d locator = %s, want %s", i, g.Locator, w.locator)
		}
		if g.Path != testPath {
			t.Errorf("finding %d path = %s", i, g.Path)
		}
	}
}

func describe(fs []finding.Finding) string {
	var parts []string
	for _, f := range fs {
		column := ""
		if f.Column != nil {
			column = f.Column.String()
		}
		parts = append(parts, string(f.Context)+" "+column+" "+string(f.Tier)+" "+f.Locator)
	}
	return strings.Join(parts, "\n")
}

func TestLiterals(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"comparison", "select 1 from student s where s.first_name = 'Pat'",
			[]want{{ctx: finding.SQL, column: "s.first_name", tier: finding.Block, reasons: blockCol, locator: "lit1"}}},
		{"warn column", "where cohort_year >= 2101",
			[]want{{ctx: finding.SQL, column: "cohort_year", tier: finding.Warn, reasons: []finding.Reason{finding.WarnColumn}}}},
		{"in list", "where full_name not in ('Pat', 'Sam')",
			[]want{{ctx: finding.SQL, column: "full_name", tier: finding.Block, reasons: blockCol}, {ctx: finding.SQL, column: "full_name", tier: finding.Block, reasons: blockCol}}},
		{"like", "where first_name ilike 'Pa%'",
			[]want{{ctx: finding.SQL, column: "first_name", tier: finding.Block, reasons: blockCol}}},
		{"select list is not associated", "select 'Contoso' as campus, full_name from student", nil},
		{"unassociated name has its shape", "select 'Doe, Jane' as label",
			[]want{{ctx: finding.SQL, tier: finding.Block, reasons: name}}},
		{"an unassociated number is not judged", "select 5550142 as n, 000-00-0000", nil},
		{"double quotes as a column", `where "full_name" = 'Pat'`,
			[]want{{ctx: finding.SQL, column: "full_name", tier: finding.Block, reasons: blockCol}}},
		{"double quotes as a string", `where nickname = "Doe, Jane"`,
			[]want{{ctx: finding.SQL, tier: finding.Block, reasons: name}}},
		{"allowed literal", "where full_name = '(none selected)'", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanSQL(t, tt.src), tt.wants...)
		})
	}
}

func TestComments(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"line comment whole", "-- Doe, Jane asked\nselect 1",
			[]want{{ctx: finding.SQLComment, tier: finding.Block, reasons: name, locator: "comment0"}}},
		{"block comment quoted run", "/* the row 'Doe, Jane' is a test */ select 1",
			[]want{{ctx: finding.SQLComment, tier: finding.Block, reasons: name, locator: "comment0.quoted0"}}},
		{"commented-out filter", "select 1\n-- where first_name = 'Pat'\n",
			[]want{{ctx: finding.SQLComment, column: "first_name", tier: finding.Block, reasons: blockCol, locator: "comment0.compared0"}}},
		{"commented-out number", "/* and student_id = 7041293 */ select 1",
			[]want{{ctx: finding.SQLComment, column: "student_id", tier: finding.Block, reasons: blockCol}}},
		{"prose apostrophe", "-- the student's first_name = 'Pat'\nselect 1",
			[]want{{ctx: finding.SQLComment, column: "first_name", tier: finding.Block, reasons: blockCol}}},
		{"clean comment", "-- counts per section\nselect 1", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanSQL(t, tt.src), tt.wants...)
		})
	}
}

func TestJinja(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"expression", "select * from {{ ref('student') }} where first_name = {{ var('who', 'Pat') }}",
			[]want{{ctx: finding.SQL, column: "first_name", tier: finding.Block, reasons: blockCol, locator: "lit1"},
				{ctx: finding.SQL, column: "first_name", tier: finding.Block, reasons: blockCol, locator: "lit2"}}},
		{"value in an expression", "{{ config(meta={'owner': 'jane.doe@example.com'}) }}\nselect 1",
			[]want{{ctx: finding.SQL, tier: finding.Block, reasons: email}}},
		{"set block", "{% set excluded = 'Doe, Jane' %}\nselect 1",
			[]want{{ctx: finding.SQL, tier: finding.Block, reasons: name}}},
		{"trim markers", "{%- if true -%}\nwhere first_name = {{- 'Pat' -}}\n{%+ endif +%}",
			[]want{{ctx: finding.SQL, column: "first_name", tier: finding.Block, reasons: blockCol}}},
		{"comment", "{# owner jane.doe@example.com #}\nselect 1",
			[]want{{ctx: finding.SQLComment, tier: finding.Block, reasons: email, locator: "comment0"}}},
		{"comment with trim markers", "{#- Doe, Jane -#}\nselect 1",
			[]want{{ctx: finding.SQLComment, tier: finding.Block, reasons: name}}},
		{"comment over lines", "{# reviewed\n   Doe, Jane #}\nselect 1",
			nil},
		{"comment before a SQL comment", "-- second: Roe, Richard\n{# first: x #}\n-- Doe, Jane\nselect 1",
			[]want{{ctx: finding.SQLComment, tier: finding.Block, reasons: name, locator: "comment2"}}},
		{"braces outside a block are kept", "select '}}' as a, '%}' as b", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanSQL(t, tt.src), tt.wants...)
		})
	}
}

func TestDialects(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"backslash-escaped quotes", `select 'O\'Pat' as a, 'Doe, Jane' as b, 'D\'Sam' as c`,
			[]want{{ctx: finding.SQL, tier: finding.Block, reasons: name, locator: "lit5"}}},
		{"escaped quote in a compared value", `where first_name = 'O\'Pat'`,
			// Only the backslash reading lexes.
			[]want{{ctx: finding.SQL, column: "first_name", tier: finding.Block, reasons: blockCol, locator: "lit0"}}},
		{"hash comment", "select 1 # Doe, Jane\nfrom t",
			[]want{{ctx: finding.SQLComment, tier: finding.Block, reasons: name, locator: "comment0"}}},
		{"hash comment numbered after the others", "-- Doe, Jane\nselect 1 # Roe, Richard\n-- x",
			[]want{{ctx: finding.SQLComment, tier: finding.Block, reasons: name, locator: "comment0"},
				{ctx: finding.SQLComment, tier: finding.Block, reasons: name, locator: "comment2"}}},
		{"filter after a hash", "select 1\n# where student_id = 7041293",
			// Code under the first reading, a comment under the third.
			[]want{{ctx: finding.SQL, column: "student_id", tier: finding.Block, reasons: blockCol, locator: "lit1"},
				{ctx: finding.SQLComment, column: "student_id", tier: finding.Block, reasons: blockCol, locator: "comment0.compared0"}}},
		{"dollar-quoted value", "where full_name = $$Pat$$",
			[]want{{ctx: finding.SQL, column: "full_name", tier: finding.Block, reasons: blockCol, locator: "lit0"}}},
		{"dollar-quoted body", "create function f() returns text as $body$ select 'Doe, Jane'$body$",
			// The body is a string, and its own literal is found by the body
			// reading.
			[]want{{ctx: finding.SQL, tier: finding.Block, reasons: name, locator: "lit1"}}},
		{"unterminated dollar quote", "select 1\n# $body$\nwhere full_name = 'Pat'",
			// Unterminated in every reading but the third, where it is in a
			// comment.
			[]want{{ctx: finding.SQL, column: "full_name", tier: finding.Block, reasons: blockCol}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanSQL(t, tt.src), tt.wants...)
		})
	}
}

func TestAssociationLimitIsAnError(t *testing.T) {
	cols := make([]string, 300)
	for i := range cols {
		cols[i] = "c" + strings.Repeat("x", i)
	}
	src := "select 1\nwhere 'Doe, Jane' = coalesce(" + strings.Join(cols, ", ") + ")"
	got, err := sqlfile.Scan(testPath, []byte(src), loadPolicy(t))
	var perr *judge.ParseError
	if got != nil || !errors.As(err, &perr) || perr.Reason != finding.UnlexableSQL || perr.Line != 2 {
		t.Fatalf("got %d findings, err %v; want an unlexable-sql error on line 2", len(got), err)
	}
	if strings.Contains(err.Error(), "Doe") {
		t.Error("the error message carries source text")
	}
}

func TestJinjaKeepsLines(t *testing.T) {
	src := "{{\n  config()\n}}\n{#\n\n#}\nselect 1\nwhere full_name = 'Doe, Jane\n"
	_, err := sqlfile.Scan(testPath, []byte(src), loadPolicy(t))
	var perr *judge.ParseError
	if !errors.As(err, &perr) || perr.Line != 8 {
		t.Fatalf("err = %v, want a parse error on line 8", err)
	}
}

func TestLexErrorsCarryNoSourceText(t *testing.T) {
	const sentinel = "Doe, Jane"
	tests := []struct {
		name string
		src  string
		line int
	}{
		{"unterminated string", "select 1\nwhere full_name = 'Doe, Jane\n", 2},
		{"unterminated block comment", "select 1\n/* Doe, Jane\n", 2},
		{"unterminated template comment", "select 1\n\n{# Doe, Jane\nselect 2", 3},
		{"unterminated backtick", "select `Doe, Jane", 1},
		{"unterminated double quote", `select "Doe, Jane`, 1},
	}
	p := loadPolicy(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqlfile.Scan(testPath, []byte(tt.src), p)
			if got != nil {
				t.Errorf("findings returned with an error")
			}
			var perr *judge.ParseError
			if !errors.As(err, &perr) {
				t.Fatalf("err = %T, want *judge.ParseError", err)
			}
			if perr.Reason != finding.UnlexableSQL || perr.Line != tt.line {
				t.Errorf("reason %s line %d, want %s line %d", perr.Reason, perr.Line, finding.UnlexableSQL, tt.line)
			}
			for _, s := range []string{sentinel, "Doe", "Jane"} {
				if strings.Contains(err.Error(), s) {
					t.Errorf("the error message carries source text")
				}
			}
		})
	}
}

func TestLocatorsCarryNoValue(t *testing.T) {
	src := "-- Doe, Jane\n/* 'Doe, Jane' */\n{# Doe, Jane #}\nselect 'Doe, Jane' where first_name = 'Doe, Jane'\n-- full_name = 'Doe, Jane'\n"
	got := scanSQL(t, src)
	if len(got) < 5 {
		t.Fatalf("got %d findings:\n%s", len(got), describe(got))
	}
	for _, f := range got {
		for _, s := range []string{"Doe", "Jane"} {
			if strings.Contains(f.Locator, s) {
				t.Errorf("locator %s carries source text", f.Locator)
			}
		}
	}
}

func TestPrintable(t *testing.T) {
	tests := []struct{ in, want finding.Column }{
		{finding.Column{Table: "student", Name: "full_name"}, finding.Column{Table: "student", Name: "full_name"}},
		{finding.Column{Name: "full-name"}, finding.Column{Name: "full-name"}},
		{finding.Column{Table: "s t", Name: "Full Name"}, finding.Column{Table: "s t", Name: "Full Name"}},
		{finding.Column{Name: "a\tb"}, finding.Column{Name: "{?}"}},
		{finding.Column{Table: "Doe, Jane", Name: "Doe, Jane"}, finding.Column{Table: "{?}", Name: "{?}"}},
	}
	for _, tt := range tests {
		if got := sqlfile.Printable(tt.in); got != tt.want {
			t.Errorf("Printable(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
	// A quoted identifier is judged by its name and printed with it.
	check(t, scanSQL(t, "where `full name` = 'Pat'"))
	v := sqlfile.Value{Text: "Pat", Columns: []finding.Column{{Table: "s t", Name: "first_name"}}}
	got, ok := v.Judge(loadPolicy(t))
	if !ok || got.Column == nil || got.Column.String() != "s t.first_name" {
		t.Errorf("Judge = %+v, %v; want a block on s t.first_name", got, ok)
	}
}

func TestNameShapeHonorsPolicy(t *testing.T) {
	p := loadPolicy(t)
	if _, ok := sqlfile.NameShape("(none selected)", p); ok {
		t.Error("an allowed literal fired")
	}
	if _, ok := sqlfile.NameShape("", p); ok {
		t.Error("an empty run fired")
	}
	v, ok := sqlfile.NameShape("Doe, Jane", p)
	if !ok || v.Tier != finding.Block || !slices.Equal(v.Reasons, name) || v.Column != nil {
		t.Errorf("NameShape = %+v, %v", v, ok)
	}
}
