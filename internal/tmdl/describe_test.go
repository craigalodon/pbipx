package tmdl

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/shape"
)

// tabs turns four-space indentation into tabs, so fixtures read easily.
func tabs(s string) string { return strings.ReplaceAll(s, "    ", "\t") }

func describeAll(t *testing.T, files ...string) ([]*Description, []error) {
	t.Helper()
	roots := make([]*Node, len(files))
	for i, f := range files {
		root, err := Parse([]byte(tabs(f)))
		if err != nil {
			t.Fatalf("Parse file %d: %v", i, err)
		}
		roots[i] = root
	}
	return Describe(roots)
}

func describeOK(t *testing.T, files ...string) []*Description {
	t.Helper()
	descs, errs := describeAll(t, files...)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("file %d: %v", i, err)
		}
	}
	return descs
}

const expressionsFile = `expression Server = "contoso.example.cloud" meta [IsParameterQuery = true, Type = "Text"]
expression Database = "warehouse" meta [IsParameterQuery = true, Type = "Text"]
expression Shaped = "Doe, Jane" meta [IsParameterQuery = true, Type = "Text"]
expression Shared =
        let
            Source = Sql.Database(Server, "school")
        in
            Source
`

const modelFile = `model Model
    defaultMode: directQuery
`

const studentFile = `table student

    measure 'Student Count' = COUNTROWS(student)

    column student_id
        dataType: string

    column label = student[student_id] & " (active)"
        dataType: string

    partition student = m
        source =
                let
                    Source = Sql.Database(Server, Database),
                    t = Source{[Schema = "dbo", Item = "student"]}[Data],
                    f = Table.SelectRows(t, each [student_id] <> "Doe, Jane")
                in
                    f

    partition shared = m
        mode: import
        source =
                let
                    s = Shared{[Schema = "dbo", Item = "term"]}[Data]
                in
                    s

    partition shaped = m
        source = Sql.Database(Server, Shaped){[Schema = "dbo", Item = "x"]}[Data]

    partition native = m
        mode: directQuery
        source = Value.NativeQuery(Sql.Database(Server, "school"), "select * from dbo.enrollment e#(lf)join dbo.term t on t.id = e.term_id where e.d = '2105-03-14'")
`

func TestDescribeModel(t *testing.T) {
	descs := describeOK(t, expressionsFile, modelFile, studentFile)

	wantExprs := []Expression{
		{"Server", kindParameter}, {"Database", kindParameter}, {"Shaped", kindParameter}, {"Shared", kindQuery},
	}
	if !slices.Equal(descs[0].Expressions, wantExprs) {
		t.Errorf("expressions = %v", descs[0].Expressions)
	}
	if descs[1].Model == nil || descs[1].Model.DefaultMode != "directQuery" || descs[1].Tables != nil {
		t.Errorf("model.tmdl = %+v", descs[1])
	}

	tables := descs[2].Tables
	if len(tables) != 1 {
		t.Fatalf("tables = %d", len(tables))
	}
	tb := tables[0]
	if !slices.Equal(tb.Measures, []string{"Student Count"}) {
		t.Errorf("measures = %q", tb.Measures)
	}
	wantCols := []Column{{"student_id", "string", false}, {"label", "string", true}}
	if !slices.Equal(tb.Columns, wantCols) {
		t.Errorf("columns = %v", tb.Columns)
	}
	type summary struct {
		mode, connectors, reads, native string
	}
	got := map[string]summary{}
	for _, p := range tb.Partitions {
		got[p.Name] = summary{p.Mode, strings.Join(p.Connectors, ","), strings.Join(p.Reads, ","), strings.Join(p.NativeSQLReads, ",")}
	}
	want := map[string]summary{
		"student": {"directQuery", "Sql.Database", "warehouse.dbo.student", ""},
		"shared":  {"import", "Sql.Database", "school.dbo.term", ""},
		"shaped":  {"directQuery", "Sql.Database", shape.NotIdentifier + ".dbo.x", ""},
		"native":  {"directQuery", "Sql.Database", "", "dbo.enrollment,dbo.term"},
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("partition %s = %+v, want %+v", name, got[name], w)
		}
	}
	if descs[2].Literals["tmdl-dax"] != 1 || descs[2].Literals["tmdl-native-sql"] != 1 {
		t.Errorf("literals = %v", descs[2].Literals)
	}
}

func TestDescribeDefaultModeIsImport(t *testing.T) {
	descs := describeOK(t, "table t\n    partition t = m\n        source = Csv.Document(Web.Contents(\"https://example.com/t.csv\"))\n")
	p := descs[0].Tables[0].Partitions[0]
	if p.Mode != "import" || !slices.Equal(p.Connectors, []string{"Web.Contents"}) || len(p.Reads) != 0 {
		t.Errorf("partition = %+v", p)
	}
}

func TestDescribeRelationshipsAndRoles(t *testing.T) {
	descs := describeOK(t, `relationship 00000000-0000-0000-0000-000000000001
    fromColumn: enrollment.student_id
    toColumn: 'course catalog'.'course code'

relationship 00000000-0000-0000-0000-000000000002
    fromColumn: 'Doe, Jane'.id
    toColumn: student.'jane.doe@example.com'

role advisors
    tablePermission student = [advisor] = USERPRINCIPALNAME()
    tablePermission term
        metadataPermission: none
    member 'jane.doe@example.com'
    member 'Roe, Richard'
        memberType: user
`)
	want := []Relationship{
		{"enrollment.student_id", "course catalog.course code"},
		{shape.Withheld + ".id", "student." + shape.Withheld},
	}
	if !slices.Equal(descs[0].Relationships, want) {
		t.Errorf("relationships = %v", descs[0].Relationships)
	}
	if r := descs[0].Roles; len(r) != 1 || r[0].Members != 2 || !slices.Equal(r[0].Filters, []string{"student"}) {
		t.Errorf("roles = %+v", r)
	}
}

func TestDescribeWithholdsShapedNames(t *testing.T) {
	descs := describeOK(t, `table 'Doe, Jane'
    measure 'Calls to 212-555-0142' = 1
    column 'jane.doe@example.com'
    partition 'Doe, Jane' = calculated
        source = {1}
`)
	tb := descs[0].Tables[0]
	if tb.Name != shape.Withheld || tb.Measures[0] != shape.Withheld || tb.Columns[0].Name != shape.Withheld ||
		tb.Partitions[0].Name != shape.Withheld || tb.Partitions[0].Source != "calculated" {
		t.Errorf("table = %+v", tb)
	}
}

func TestDescribeNavigationSteps(t *testing.T) {
	descs := describeOK(t, `table t
    partition t = m
        source =
                let
                    Source = Snowflake.Databases("contoso.example.cloud", "wh"),
                    db = Source{[Name = "SCHOOL", Kind = "Database"]}[Data],
                    sc = db{[Name = "school-2105", Kind = "Schema"]}[Data],
                    tb = sc{[Name = UnknownName, Kind = "Table"]}[Data],
                    #"First Row" = tb{0}
                in
                    #"First Row"
`)
	p := descs[0].Tables[0].Partitions[0]
	ni := shape.NotIdentifier
	want := "SCHOOL." + ni + "." + ni + "." + ni
	if !slices.Equal(p.Reads, []string{want}) || !slices.Equal(p.Connectors, []string{"Snowflake.Databases"}) {
		t.Errorf("partition = %+v", p)
	}
}

func TestDescribeEntityPartition(t *testing.T) {
	descs := describeOK(t, `expression DatabaseQuery =
        let
            database = Sql.Database("contoso.example.cloud", "00000000-0000-0000-0000-000000000000")
        in
            database
`, `table t
    partition t = entity
        mode: directLake
        source
            entityName: student
            schemaName: dbo
            expressionSource: DatabaseQuery
`)
	p := descs[1].Tables[0].Partitions[0]
	if p.Source != "entity" || p.Mode != "directLake" || !slices.Equal(p.Reads, []string{"dbo.student"}) ||
		!slices.Equal(p.Connectors, []string{"Sql.Database"}) {
		t.Errorf("partition = %+v", p)
	}
}

func TestDescribeLexErrorIsTheFilesError(t *testing.T) {
	_, errs := describeAll(t, "table t\n    measure m = \"Doe, Jane\n", "table u\n")
	var pe *judge.ParseError
	if !errors.As(errs[0], &pe) || pe.Reason != finding.UnparseableTMDL {
		t.Errorf("err = %v, want unparseable-tmdl", errs[0])
	}
	if errs[1] != nil {
		t.Errorf("the other file failed too")
	}
}

// nestedLets is an M partition source of at least size bytes whose let
// expressions nest, each step holding the next: let a = let a = ... in a.
func nestedLets(size int) string {
	const open, closeIn = "let a = ", " in a"
	n := size / (len(open) + len(closeIn))
	return "table t\n\tpartition t = m\n\t\tsource = " +
		strings.Repeat(open, n) + "Sql.Database(\"h\", \"d\"){[Item = \"x\"]}[Data]" + strings.Repeat(closeIn, n) + "\n"
}

// Reading nested let expressions is linear: each level's step is found in
// one pass, not by scanning the rest of the expression again.
func TestDescribeNestedLetsIsLinear(t *testing.T) {
	src := nestedLets(256 << 10)
	root, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	descs, errs := Describe([]*Node{root})
	elapsed := time.Since(start)
	if errs[0] != nil {
		t.Fatal(errs[0])
	}
	if p := descs[0].Tables[0].Partitions[0]; !slices.Equal(p.Reads, []string{"d.x"}) {
		t.Errorf("reads = %v", p.Reads)
	}
	// Quadratic, this took about half a minute; linear, a fraction of a
	// second, most of it lexing.
	if !raceEnabled && elapsed > 5*time.Second {
		t.Errorf("256KB of nested lets took %v", elapsed)
	}
}

func TestDescribeNeverCarriesAValue(t *testing.T) {
	planted := []string{"Doe", "Jane", "contoso.example.cloud", "2105-03-14", "212-555-0142", "Roe, Richard", "school-2105"}
	roles := `role r
    member 'Roe, Richard'
`
	nav := `table n
    partition n = m
        source = Sql.Database("contoso.example.cloud", "school-2105"){[Schema = "dbo", Item = "Doe, Jane"]}[Data]
`
	descs := describeOK(t, expressionsFile, studentFile, roles, nav)
	out, err := json.Marshal(descs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range planted {
		if strings.Contains(string(out), v) {
			t.Errorf("the description carries a planted value")
		}
	}
}
