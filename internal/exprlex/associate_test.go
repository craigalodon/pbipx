package exprlex

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// Every value here is synthetic (CONTRIBUTING.md#synthetic-data).

// describe writes sites compactly: value, then =col,table.col when
// associated; a number is prefixed #.
func describe(sites []Site) []string {
	out := make([]string, len(sites))
	for i, s := range sites {
		d := s.Value
		if s.Number {
			d = "#" + d
		}
		if len(s.Columns) > 0 {
			cols := make([]string, len(s.Columns))
			for k, c := range s.Columns {
				cols[k] = c.String()
			}
			d += "=" + strings.Join(cols, ",")
		}
		out[i] = d
	}
	return out
}

func mustAssociate(t *testing.T, toks []Token, concat string) []Site {
	t.Helper()
	s, err := AssociateChecked(toks, concat)
	if err != nil {
		t.Fatalf("AssociateChecked: %v", err)
	}
	return s
}

type assocCase struct {
	name string
	src  string
	want []string
}

func sitesOf(t *testing.T, lang, src string) []Site {
	t.Helper()
	var toks []Token
	var err error
	concat := "&"
	switch lang {
	case "DAX":
		toks, _, err = DAX(src)
	case "M":
		toks, _, err = M(src)
	case "SQL":
		toks, _, err = SQL(src, QuoteSkip)
		concat = "||"
	case "SQL both":
		var s []Site
		s, _, err = SQLSites(src)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		return s
	}
	if err != nil {
		t.Fatalf("%s: %v", lang, err)
	}
	return mustAssociate(t, toks, concat)
}

func runAssoc(t *testing.T, lang string, cases []assocCase) {
	t.Helper()
	for _, c := range cases {
		sites := sitesOf(t, lang, c.src)
		if got := describe(sites); !slices.Equal(got, c.want) {
			t.Errorf("%s: %s:\n got %q\nwant %q", lang, c.name, got, c.want)
		}
		for i, s := range sites {
			if s.Index != i {
				t.Errorf("%s: %s: site %d has index %d", lang, c.name, i, s.Index)
			}
		}
	}
}

func TestComparison(t *testing.T) {
	runAssoc(t, "DAX", []assocCase{
		{"column on the left", `CALCULATE(COUNTROWS(student), student[preferred_name] = "Jane")`,
			[]string{"Jane=student.preferred_name"}},
		{"literal on the left", `COUNTROWS(FILTER(student, "Jane" = student[first_name]))`,
			[]string{"Jane=student.first_name"}},
		{"every operator", `[a] == "1" || [b] <> "2" || [c] < 3 || [d] > 4 || [e] <= 5 || [f] >= 6 || [g] != "7"`,
			[]string{"1=a", "2=b", "#3=c", "#4=d", "#5=e", "#6=f", "7=g"}},
		{"wrapped column", `LOWER(student[email_address]) = "jane.doe"`, []string{"jane.doe=student.email_address"}},
		{"concatenation chain", `student[campus_code] & student[student_id] = "N9900417"`,
			[]string{"N9900417=student.campus_code,student.student_id"}},
		{"chain of literals", `[full_name] = "Doe" & ", Jane"`, []string{"Doe=full_name", ", Jane=full_name"}},
		{"wrapped literal", `[email] = LOWER("Jane")`, []string{"Jane=email"}},
		{"negative number", `[student_id] = -9900417`, []string{"#9900417=student_id"}},
		{"number on a column", `student[student_id] = 9900417`, []string{"#9900417=student.student_id"}},
		{"unassociated number", `IF(COUNTROWS(student) > 9900417, 1, 0)`, []string{"#9900417", "#1", "#0"}},
		{"other conditions stay apart", `[a] = "x" && [b] = "y"`, []string{"x=a", "y=b"}},
		{"duplicate column once", `[a] & [a] = "x"`, []string{"x=a"}},
		{"nested condition is not the operand", `"x" = IF([b] = "y", 1, 2)`, []string{"x=b", "y=b", "#1", "#2"}},
		{"a quoted table is not a value", `COUNTROWS('Doe, Jane')`, nil},
	})
	runAssoc(t, "M", []assocCase{
		{"field compared", `Table.SelectRows(student, each [last_name] <> "Doe")`, []string{"Doe=last_name"}},
		{"step names are not values", `let Source = x, #"Merged, Expanded" = Table.SelectRows(Source, each [status] = "Active") in #"Merged, Expanded"`,
			[]string{"Active=status"}},
		{"record fields are not columns", `Source{[Schema = "enrollment", Item = "student"]}[Data]`,
			[]string{"enrollment", "student"}},
		{"concatenation chain", `each [first_name] & " " & [last_name] = "Jane Doe"`,
			[]string{" ", "Jane Doe=first_name,last_name"}},
	})
	runAssoc(t, "SQL", []assocCase{
		{"bare column", `select student_id from enrollment where first_name = 'Jane'`, []string{"Jane=first_name"}},
		{"qualified column", `where analytics.student.first_name = 'Richard'`, []string{"Richard=student.first_name"}},
		{"wrapped column", `where lower(email) = 'jane.doe@example.com'`, []string{"jane.doe@example.com=email"}},
		{"concatenation chain", `where last_name || suffix = 'Roe Jr'`, []string{"Roe Jr=last_name,suffix"}},
		{"backticked column", "where `full_name` = 'Jane Doe'", []string{"Jane Doe=full_name"}},
		{"number", `where student_id = 7041293`, []string{"#7041293=student_id"}},
		{"and splits operands", `where a = 'x' and b = 'y'`, []string{"x=a", "y=b"}},
		{"case when", `case when status = 'A' then 'Active' else 'Other' end`, []string{"A=status", "Active", "Other"}},
	})
}

func TestPattern(t *testing.T) {
	runAssoc(t, "SQL", []assocCase{
		{"like", `where preferred_name like 'Jan%'`, []string{"Jan%=preferred_name"}},
		{"not ilike", `where preferred_name not ilike 'Rich%'`, []string{"Rich%=preferred_name"}},
		{"rlike", `where s.email RLIKE '^jane'`, []string{"^jane=s.email"}},
		{"not regexp", `where email NOT REGEXP 'doe$'`, []string{"doe$=email"}},
		{"escape is not the pattern", `where a like 'x!%' escape '!'`, []string{"x!%=a", "!"}},
	})
}

func TestInList(t *testing.T) {
	runAssoc(t, "DAX", []assocCase{
		{"in list", `CALCULATE(COUNTROWS(student), student[last_name] IN {"Doe", "Roe"})`,
			[]string{"Doe=student.last_name", "Roe=student.last_name"}},
		{"not in", `NOT [last_name] IN {"Doe"}`, []string{"Doe=last_name"}},
		{"tuples one level down", `([a], [b]) IN {("x", 1), ("y", 2)}`,
			[]string{"x=a,b", "#1=a,b", "y=a,b", "#2=a,b"}},
		{"deeper is not in the list", `[a] IN {LOWER("x")}`, []string{"x"}},
	})
	runAssoc(t, "SQL", []assocCase{
		{"in list", `where full_name in ('Doe, Jane', 'Roe, Richard')`,
			[]string{"Doe, Jane=full_name", "Roe, Richard=full_name"}},
		{"not in list", `where student.first_name not in ('Jane', 'Richard')`,
			[]string{"Jane=student.first_name", "Richard=student.first_name"}},
		{"subquery is not a list", `where student_id in (select student_id from hold where kind = 'x')`,
			[]string{"x=kind"}},
	})
	runAssoc(t, "M", []assocCase{
		{"let ... in is not a list", `let a = "x" in a`, []string{"x"}},
	})
}

func TestMatchingFunctions(t *testing.T) {
	runAssoc(t, "DAX", []assocCase{
		{"containsstring", `CONTAINSSTRING(student[full_name], "Roe")`, []string{"Roe=student.full_name"}},
		{"case-insensitive", `containsStringExact("Roe", [full_name])`, []string{"Roe=full_name"}},
		{"treatas list argument", `TREATAS({"Jane", "Richard"}, student[first_name])`,
			[]string{"Jane=student.first_name", "Richard=student.first_name"}},
		{"lookupvalue", `LOOKUPVALUE(student[email], student[student_id], 9900417)`,
			[]string{"#9900417=student.email,student.student_id"}},
		{"search numbers are positions", `SEARCH("Doe", student[full_name], 1, 0)`,
			[]string{"Doe=student.full_name", "#1", "#0"}},
		{"switch ties match values only", `SWITCH(student[campus], "N", "North", "S", "South", "Other")`,
			[]string{"N=student.campus", "North", "S=student.campus", "South", "Other"}},
		{"switch on a condition ties nothing", `SWITCH(TRUE(), [a] = "x", "y")`, []string{"x=a", "y"}},
		{"a condition argument passes no column", `CONTAINS(student, [a] = "x", "y")`, []string{"x=a", "y"}},
		{"exact", `EXACT([last_name], "Doe")`, []string{"Doe=last_name"}},
		{"pathcontains", `PATHCONTAINS([path], "Doe")`, []string{"Doe=path"}},
		{"containsrow", `CONTAINSROW({"Doe"}, [last_name])`, []string{"Doe=last_name"}},
		{"other functions tie nothing", `CONCATENATE([last_name], "Doe")`, []string{"Doe"}},
	})
	runAssoc(t, "M", []assocCase{
		{"text.contains", `Table.SelectRows(student, each Text.Contains([email], "jane.doe"))`,
			[]string{"jane.doe=email"}},
		{"list.contains", `Table.SelectRows(student, each List.Contains({"Jane", "Richard"}, [first_name]))`,
			[]string{"Jane=first_name", "Richard=first_name"}},
		{"wrapped column argument", `Text.StartsWith(Text.Lower([last_name]), "doe")`, []string{"doe=last_name"}},
		{"every function", `Text.EndsWith([a], "1") and List.ContainsAny([b], {"2"}) and List.ContainsAll([c], {"3"}) and Value.Equals([d], "4") and Comparer.Equals(Comparer.Ordinal, [e], "5")`,
			[]string{"1=a", "2=b", "3=c", "4=d", "5=e"}},
	})
}

// A partition that only selects a column ties no string to it (principle 9).
func TestNoise(t *testing.T) {
	runAssoc(t, "M", []assocCase{
		{"selecting a column", `let
	Source = Sql.Database("contoso.example.cloud", "school"),
	student = Source{[Schema = "enrollment", Item = "student"]}[Data],
	Kept = Table.SelectColumns(student, {"student_id", "full_name", "status"}),
	Filtered = Table.SelectRows(Kept, each [status] = "Active")
in
	Filtered`, []string{"contoso.example.cloud", "school", "enrollment", "student", "student_id", "full_name", "status", "Active=status"}},
		{"type names and column lists", `#"Changed Type" = Table.TransformColumnTypes(student, {{"full_name", type text}, {"student_id", Int64.Type}})`,
			[]string{"full_name", "student_id"}},
	})
	runAssoc(t, "SQL", []assocCase{
		{"select of a block column", `select 'Contoso' as campus, full_name, email from student where status = 'Withdrawn'`,
			[]string{"Contoso", "Withdrawn=status"}},
	})
}

func TestSQLSitesMerges(t *testing.T) {
	runAssoc(t, "SQL both", []assocCase{
		{"double-quoted column", `where "full_name" = 'Example, Pat'`, []string{"full_name", "Example, Pat=full_name"}},
		{"double-quoted value", `where nickname = "Doe, Jane"`, []string{"Doe, Jane=nickname"}},
		{"both", `where "a" = 'x' and b = "y"`, []string{"a", "x=a", "y=b"}},
	})
}

func TestDatatableHeaders(t *testing.T) {
	toks, _, err := DAX(`DATATABLE("full_name", STRING, "student_id", INTEGER, {{"Doe, Jane", 9900417}, {"Roe, Richard", 9900418}})`)
	if err != nil {
		t.Fatal(err)
	}
	got := DatatableHeaders(toks)
	want := []string{"full_name", "student_id"}
	if keys := slices.Sorted(maps.Keys(got)); !slices.Equal(keys, []int{2, 3, 4, 5}) {
		t.Fatalf("rows %v", keys)
	}
	for k, h := range got {
		if !slices.Equal(h, want) {
			t.Errorf("literal %d: headers %q", k, h)
		}
	}
	if toks2, _, _ := DAX(`ROW("a", "x")`); len(DatatableHeaders(toks2)) != 0 {
		t.Error("ROW is not a DATATABLE")
	}
}

func TestInBraces(t *testing.T) {
	toks, _, err := DAX(`UNION({"Jane", "Richard"}, ROW("x", "y"), {("Doe", 1)})`)
	if err != nil {
		t.Fatal(err)
	}
	got := InBraces(toks)
	want := map[int]bool{0: true, 1: true, 4: true, 5: true}
	if !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLooksLikeSQL(t *testing.T) {
	for s, want := range map[string]bool{
		"select student_id from enrollment.student":   true,
		"SELECT *\nFROM t":                            true,
		"Selected from the list":                      false,
		"from x select y":                             false,
		"selection fromage":                           false,
		"contoso.example.cloud":                       false,
		"with a as (select 1 from b) select * from a": true,
	} {
		if got := LooksLikeSQL(s); got != want {
			t.Errorf("%q: got %v", s, got)
		}
	}
}
