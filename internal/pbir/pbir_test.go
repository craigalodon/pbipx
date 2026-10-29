package pbir_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/pbir"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/secret"
)

const testPolicy = `version = 1
protected_roots = ["pbip"]
[scan]
report = ["pbip/**/*.json"]
[columns]
block = ["full_name", "student_id", "date_of_birth"]
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

const testPath = "pbip/Contoso.Report/definition/pages/0001/visuals/0002/visual.json"

func scan(t *testing.T, doc string) []finding.Finding {
	t.Helper()
	fs, err := pbir.Scan(testPath, []byte(doc), loadPolicy(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return fs
}

// Builders for the PBIR expression objects the tests need.

func col(source, entity, name string) string {
	ref := `"Source": "` + source + `"`
	if entity != "" {
		ref = `"Entity": "` + entity + `"`
	}
	return `{"Column": {"Expression": {"SourceRef": {` + ref + `}}, "Property": "` + name + `"}}`
}

func lit(v string) string {
	return `{"Literal": {"Value": "` + v + `"}}`
}

func from(alias, entity string) string {
	return `{"Name": "` + alias + `", "Entity": "` + entity + `", "Type": 0}`
}

// in is a filter body: From, and Where column In values.
func in(froms, column string, values ...string) string {
	rows := make([]string, len(values))
	for i, v := range values {
		rows[i] = "[" + v + "]"
	}
	return `{"Version": 2, "From": [` + froms + `], "Where": [{"Condition": {"In": {"Expressions": [` + column +
		`], "Values": [` + strings.Join(rows, ", ") + `]}}}]}`
}

// want is a finding by what the tests compare.
type want struct {
	ctx     finding.Context
	column  string
	tier    finding.Tier
	reasons []finding.Reason
	locator string // compared when set
}

func check(t *testing.T, got []finding.Finding, wants ...want) {
	t.Helper()
	if len(got) != len(wants) {
		t.Fatalf("got %d findings, want %d: %v", len(got), len(wants), describe(got))
	}
	for i, w := range wants {
		g := got[i]
		column := ""
		if g.Column != nil {
			column = g.Column.String()
		}
		if g.Path != testPath || g.Context != w.ctx || column != w.column || g.Tier != w.tier || !slices.Equal(g.Reasons, w.reasons) {
			t.Errorf("finding %d = %s, want %s %s %s %v", i, describe(got[i:i+1]), w.ctx, w.column, w.tier, w.reasons)
		}
		if w.locator != "" && g.Locator != w.locator {
			t.Errorf("finding %d locator = %s, want %s", i, g.Locator, w.locator)
		}
	}
}

func describe(fs []finding.Finding) string {
	var parts []string
	for _, f := range fs {
		column := "-"
		if f.Column != nil {
			column = f.Column.String()
		}
		parts = append(parts, string(f.Context)+" "+column+" "+string(f.Tier)+" "+f.Locator)
	}
	return strings.Join(parts, "; ")
}

var blockCol = []finding.Reason{finding.BlockColumn}

func TestFilterContext(t *testing.T) {
	doc := `{"filterConfig": {"filters": [{"name": "Filter1", "field": ` + col("", "student", "student_id") +
		`, "type": "Categorical", "filter": ` + in(from("s", "student"), col("s", "", "student_id"), lit("'S-1001'")) + `}]}}`
	check(t, scan(t, doc), want{
		ctx: finding.Filter, column: "student.student_id", tier: finding.Block, reasons: blockCol,
		locator: "filterConfig.filters[0].filter.Where[0].Condition.In.Values[0][0].Literal.Value",
	})
}

func TestFilterFieldColumnIsLast(t *testing.T) {
	// Two block columns: the one the walk meets in the filter comes before
	// the entry's own field.
	doc := `{"filterConfig": {"filters": [{"field": ` + col("", "student", "full_name") +
		`, "filter": ` + in(from("s", "student"), col("s", "", "student_id"), lit("'S-1001'")) + `}]}}`
	check(t, scan(t, doc), want{ctx: finding.Filter, column: "student.student_id", tier: finding.Block, reasons: blockCol})

	// The field alone still associates: an unlisted column in the filter,
	// a block column in the field.
	doc = `{"filterConfig": {"filters": [{"field": ` + col("", "student", "full_name") +
		`, "filter": ` + in(from("e", "enrollment"), col("e", "", "term_name"), lit("'Autumn Term'")) + `}]}}`
	check(t, scan(t, doc), want{ctx: finding.Filter, column: "student.full_name", tier: finding.Block, reasons: blockCol})
}

func TestFiltersAreSeparateContexts(t *testing.T) {
	doc := `{"filterConfig": {"filters": [
		{"field": ` + col("", "student", "full_name") + `},
		{"field": ` + col("", "enrollment", "term_name") + `, "filter": ` + in(from("e", "enrollment"), col("e", "", "term_name"), lit("'Autumn Term'")) + `}]}}`
	check(t, scan(t, doc))
}

func TestSlicerSelectionAnyObjectClass(t *testing.T) {
	for _, class := range []string{"general", "data", "merge"} {
		doc := `{"visual": {"objects": {"` + class + `": [{"properties": {"filter": {"filter": ` +
			in(from("s", "student"), col("s", "", "full_name"), lit("'Pat Example'")) + `}}}]}}}`
		check(t, scan(t, doc), want{
			ctx: finding.SlicerSelection, column: "student.full_name", tier: finding.Block, reasons: blockCol,
			locator: "visual.objects." + class + "[0].properties.filter.filter.Where[0].Condition.In.Values[0][0].Literal.Value",
		})
	}
}

func TestSlicerSelectionIgnoresTheQuery(t *testing.T) {
	// The visual's projections name a block column; the selection is on
	// an unlisted one. The query is not a context.
	doc := `{"visual": {"query": {"queryState": {"Values": {"projections": [{"field": ` + col("", "student", "full_name") + `}]}}},
		"objects": {"general": [{"properties": {"filter": {"filter": ` +
		in(from("e", "enrollment"), col("e", "", "term_name"), lit("'Autumn Term'")) + `}}}],
		"data": [{"properties": {"mode": {"expr": ` + lit("'Doe, Jane'") + `}}}]}}}`
	check(t, scan(t, doc))
}

func TestInsightSelector(t *testing.T) {
	selector := `{"data": [{"scopeId": {"Comparison": {"ComparisonKind": 0, "Left": ` + col("", "student", "student_id") +
		`, "Right": ` + lit("'S-1004'") + `}}}]}`
	doc := `{"visual": {"objects": {"insights": [{"properties": {"show": {"expr": ` + lit("true") + `}}, "selector": ` + selector + `}]}}}`
	check(t, scan(t, doc), want{
		ctx: finding.InsightSelector, column: "student.student_id", tier: finding.Block, reasons: blockCol,
		locator: "visual.objects.insights[0].selector.data[0].scopeId.Comparison.Right.Literal.Value",
	})

	// A selector under any other class is not an insight selector.
	doc = `{"visual": {"objects": {"values": [{"selector": ` + selector + `}]}}}`
	check(t, scan(t, doc))
}

func TestConditionalFormatAtAnyDepth(t *testing.T) {
	cond := `{"Conditional": {"Cases": [{"Condition": {"Comparison": {"ComparisonKind": 0, "Left": ` +
		col("", "student", "full_name") + `, "Right": ` + lit("'Pat Example'") + `}}, "Value": ` + lit("'#FF0000'") + `}]}}`
	doc := `{"visual": {"objects": {"values": [{"properties": {"backColor": {"solid": {"color": {"expr": ` + cond + `}}}},
		"selector": {"metadata": "student.full_name"}}]}}}`
	loc := "visual.objects.values[0].properties.backColor.solid.color.expr.Conditional.Cases[0]"
	check(t, scan(t, doc),
		want{ctx: finding.ConditionalFormat, column: "student.full_name", tier: finding.Block, reasons: blockCol, locator: loc + ".Condition.Comparison.Right.Literal.Value"},
		want{ctx: finding.ConditionalFormat, column: "student.full_name", tier: finding.Block, reasons: blockCol, locator: loc + ".Value.Literal.Value"},
	)
}

func TestExpansionState(t *testing.T) {
	doc := `{"visual": {"expansionStates": [{"roles": ["Rows"], "levels": [{"queryRefs": ["student.full_name"], "identityKeys": [` +
		col("", "student", "full_name") + `]}], "root": {"children": [{"identityValues": [` + lit("'Pat Example'") + `], "isToggled": true}]}}]}}`
	check(t, scan(t, doc), want{
		ctx: finding.ExpansionState, column: "student.full_name", tier: finding.Block, reasons: blockCol,
		locator: "visual.expansionStates[0].root.children[0].identityValues[0].Literal.Value",
	})
}

func TestTitleTextHasNoColumns(t *testing.T) {
	doc := `{"visual": {"visualContainerObjects": {
		"title": [{"properties": {"show": {"expr": ` + lit("true") + `}, "text": {"expr": ` + lit("'Doe, Jane'") + `}}}],
		"subTitle": [{"properties": {"text": {"expr": ` + lit("'jane.doe@example.com'") + `}}}],
		"background": [{"properties": {"text": {"expr": ` + lit("'Roe, Richard'") + `}}}]}}}`
	check(t, scan(t, doc),
		want{ctx: finding.TitleText, tier: finding.Block, reasons: []finding.Reason{finding.EmailShape}, locator: "visual.visualContainerObjects.subTitle[0].properties.text.expr.Literal.Value"},
		want{ctx: finding.TitleText, tier: finding.Block, reasons: []finding.Reason{finding.PersonNameShape}, locator: "visual.visualContainerObjects.title[0].properties.text.expr.Literal.Value"},
	)

	// A column in the text expression does not associate.
	doc = `{"visual": {"visualContainerObjects": {"title": [{"properties": {"text": {"expr": {"Comparison": {"Left": ` +
		col("", "student", "full_name") + `, "Right": ` + lit("'Pat Example'") + `}}}}}]}}}`
	check(t, scan(t, doc))
}

func bookmark(state string) string {
	return `{"name": "Bookmark1", "explorationState": ` + state + `, "options": {"targetVisualNames": ["v1"]}}`
}

func slicerContainer(column, value string) string {
	return `{"singleVisual": {"objects": {"merge": {"general": [{"properties": {"filter": {"filter": ` +
		in(from("e", "enrollment"), column, value) + `}}}]}}}}`
}

func TestBookmarkContainersAreSeparate(t *testing.T) {
	// Container v1 names a block column; v2's value is on an unlisted one.
	state := `{"sections": {"page1": {"visualContainers": {
		"v1": {"filters": {"byExpr": [{"expression": ` + col("", "student", "full_name") + `}]}},
		"v2": ` + slicerContainer(col("e", "", "term_name"), lit("'Autumn Term'")) + `}}}}`
	check(t, scan(t, bookmark(state)))

	state = `{"sections": {"page1": {"visualContainers": {
		"v2": ` + slicerContainer(col("e", "", "full_name"), lit("'Pat Example'")) + `}}}}`
	check(t, scan(t, bookmark(state)), want{
		ctx: finding.Bookmark, column: "enrollment.full_name", tier: finding.Block, reasons: blockCol,
		locator: "explorationState.sections.page1.visualContainers.v2.singleVisual.objects.merge.general[0].properties.filter.filter.Where[0].Condition.In.Values[0][0].Literal.Value",
	})
}

func TestBookmarkRemainderIsOneContext(t *testing.T) {
	// A report filter and a page filter share the remainder context; the
	// containers' columns do not join it.
	state := `{"filters": {"byExpr": [{"expression": ` + col("", "student", "student_id") + `}]},
		"sections": {"page1": {
			"filters": {"byExpr": [{"filter": ` + in(from("e", "enrollment"), col("e", "", "term_name"), lit("'Autumn Term'")) + `}]},
			"visualContainers": {"v1": ` + slicerContainer(col("e", "", "term_name"), lit("'Spring Term'")) + `}}}}`
	check(t, scan(t, bookmark(state)), want{
		ctx: finding.Bookmark, column: "student.student_id", tier: finding.Block, reasons: blockCol,
		locator: "explorationState.sections.page1.filters.byExpr[0].filter.Where[0].Condition.In.Values[0][0].Literal.Value",
	})
}

func TestBookmarkCachedDisplayLabel(t *testing.T) {
	state := `{"sections": {"page1": {"visualContainers": {"v1": {"filters": {"byExpr": [{
		"expression": ` + col("", "student", "student_id") + `,
		"filter": ` + in(from("s", "student"), col("s", "", "student_id"), lit("90817L")) + `,
		"cachedFilterDisplayItems": [{"id": {}, "label": "Doe, Jane"}]}]}}}}}}`
	check(t, scan(t, bookmark(state)), want{
		ctx: finding.Bookmark, column: "student.student_id", tier: finding.Block,
		reasons: []finding.Reason{finding.BlockColumn, finding.PersonNameShape},
		locator: "explorationState.sections.page1.visualContainers.v1.filters.byExpr[0].cachedFilterDisplayItems[0].label",
	})
}

func TestAliases(t *testing.T) {
	sub := `{"Name": "subquery", "Expression": {"Subquery": {"Query": {"Version": 2, "From": [` + from("p", "student") +
		`], "Where": [{"Condition": {"Comparison": {"ComparisonKind": 0, "Left": ` + col("p", "", "student_id") +
		`, "Right": ` + lit("'S-1006'") + `}}}]}}}, "Type": 2}`
	tests := []struct {
		name   string
		filter string
		column string
	}{
		{"alias through the filter's From", in(from("s", "student"), col("s", "", "full_name"), lit("'Pat Example'")), "student.full_name"},
		{"entity named directly", in("", col("", "student", "full_name"), lit("'Pat Example'")), "student.full_name"},
		{"unresolvable alias stays bare", in(from("s", "student"), col("x", "", "full_name"), lit("'Pat Example'")), "full_name"},
		{"alias through a nested From", `{"Version": 2, "From": [` + sub + `, ` + from("e", "enrollment") + `], "Where": []}`, "student.student_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := `{"filterConfig": {"filters": [{"filter": ` + tt.filter + `}]}}`
			check(t, scan(t, doc), want{ctx: finding.Filter, column: tt.column, tier: finding.Block, reasons: blockCol})
		})
	}
}

func TestLiteralKinds(t *testing.T) {
	tests := []struct {
		name    string
		column  string
		value   string
		reasons []finding.Reason // nil: no finding
	}{
		{"boolean", "full_name", "true", nil},
		{"null", "full_name", "null", nil},
		{"long", "student_id", "1001L", nil},
		{"double", "student_id", "1D", nil},
		{"decimal", "student_id", "2.5M", nil},
		{"other typed literal", "student_id", "X'00'", nil},
		{"unterminated quote", "full_name", "'Pat", nil},
		{"string of digits is a string", "student_id", "'2105'", blockCol},
		{"datetime on a block column", "date_of_birth", "datetime'2105-03-14T00:00:00'", blockCol},
		{"datetime on an ordinary column", "term_start_date", "datetime'2105-03-14T00:00:00'", nil},
		{"string date on an ordinary column has the shape", "term_start_date", "'2105-03-14'", []finding.Reason{finding.DateShape}},
		{"allowed sentinel", "full_name", "'(none selected)'", nil},
		{"empty string", "full_name", "''", nil},
		{"warn column", "cohort_year", "'2105'", []finding.Reason{finding.WarnColumn}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := `{"filterConfig": {"filters": [{"filter": ` + in(from("s", "student"), col("s", "", tt.column), lit(tt.value)) + `}]}}`
			got := scan(t, doc)
			if tt.reasons == nil {
				check(t, got)
				return
			}
			if len(got) != 1 || !slices.Equal(got[0].Reasons, tt.reasons) {
				t.Fatalf("got %s, want reasons %v", describe(got), tt.reasons)
			}
		})
	}
}

func TestQuotesAreUndoubled(t *testing.T) {
	doc := `{"filterConfig": {"filters": [{"filter": ` + in(from("s", "student"), col("s", "", "advisor"), lit("'O''Roe, Richard'")) + `}]}}`
	got := scan(t, doc)
	check(t, got, want{ctx: finding.Filter, tier: finding.Block, reasons: []finding.Reason{finding.PersonNameShape}})
	if s := secret.Shape(got[0].Value); s != "A'Aa{2}, Aa{6}" {
		t.Errorf("value shape = %s, want the un-doubled shape", s)
	}
}

func TestEachLiteralIsOneFinding(t *testing.T) {
	doc := `{"filterConfig": {"filters": [{"filter": ` +
		in(from("s", "student"), col("s", "", "full_name"), lit("'Pat Example'"), lit("'Pat Example'"), lit("'Sam Example'")) + `}]}}`
	got := scan(t, doc)
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3", len(got))
	}
}

func TestDuplicateKeysAreAllWalked(t *testing.T) {
	doc := `{"filterConfig": {"filters": [{"filter": {"From": [` + from("s", "student") + `], "Where": [
		{"Condition": {"In": {"Expressions": [` + col("s", "", "full_name") + `], "Values": [[` + lit("'Pat Example'") + `]], "Values": [[` + lit("'Sam Example'") + `]]}}}]}}]}}`
	if got := scan(t, doc); len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
}

func TestLocatorsAreDeterministic(t *testing.T) {
	a := `{"visual": {"objects": {"general": [{"properties": {"filter": {"filter": {"From": [` + from("s", "student") +
		`], "Where": [{"Condition": {"In": {"Expressions": [` + col("s", "", "full_name") + `], "Values": [[` + lit("'Pat Example'") + `]]}}}]}}}}]}}}`
	// The same document with its keys written in another order.
	b := `{"visual": {"objects": {"general": [{"properties": {"filter": {"filter": {"Where": [{"Condition": {"In": {"Values": [[` +
		lit("'Pat Example'") + `]], "Expressions": [` + col("s", "", "full_name") + `]}}}], "From": [` + from("s", "student") + `]}}}}]}}}`
	ga, gb := scan(t, a), scan(t, b)
	if len(ga) != 1 || len(gb) != 1 {
		t.Fatalf("got %d and %d findings, want 1 each", len(ga), len(gb))
	}
	if ga[0].Locator != gb[0].Locator {
		t.Errorf("locators differ: %s and %s", ga[0].Locator, gb[0].Locator)
	}
	// A key that is not a plain name is not written into a locator.
	c := `{"visual": {"objects": {"odd key, Doe": [{"properties": {"filter": {"filter": ` +
		in(from("s", "student"), col("s", "", "full_name"), lit("'Pat Example'")) + `}}}]}}}`
	gc := scan(t, c)
	if len(gc) != 1 || strings.Contains(gc[0].Locator, "Doe") {
		t.Errorf("locator = %v, want the key left out", describe(gc))
	}
}

func TestOutsideContextsIsNotVisited(t *testing.T) {
	doc := `{"name": "Doe, Jane", "visual": {"visualType": "tableEx",
		"query": {"queryState": {"Values": {"projections": [{"field": ` + col("", "student", "full_name") + `, "queryRef": "student.full_name"}]}}},
		"objects": {"columnWidth": [{"properties": {"value": {"expr": ` + lit("180D") + `}}, "selector": {"metadata": "student.full_name"}}]}},
		"filterConfig": {"filters": [{"filter": ` + in(from("e", "enrollment"), col("e", "", "term_name"), lit("'Autumn Term'")) + `}]}}`
	check(t, scan(t, doc))
}

func TestByteOrderMarkAccepted(t *testing.T) {
	doc := "\xEF\xBB\xBF" + `{"filterConfig": {"filters": [{"filter": ` + in(from("s", "student"), col("s", "", "full_name"), lit("'Pat Example'")) + `}]}}`
	check(t, scan(t, doc), want{ctx: finding.Filter, column: "student.full_name", tier: finding.Block, reasons: blockCol})
}

func TestNonObjectDocumentsAreClean(t *testing.T) {
	for _, doc := range []string{`[]`, `"Doe, Jane"`, `null`, `{}`} {
		check(t, scan(t, doc))
	}
}

func TestParseErrorsCarryNoSourceText(t *testing.T) {
	const sentinel = "Doe, Jane"
	tests := []struct {
		name   string
		doc    string
		reason finding.Reason
		line   int
	}{
		{"truncated string", "{\n  \"filter\": \"'Doe, Jane", finding.UnparseableJSON, 2},
		{"bad character", "{\n  \"a\": 1,\n  Doe, Jane\n}", finding.UnparseableJSON, 3},
		{"bad value", "{\n  \"a\": Doe, Jane\n}", finding.UnparseableJSON, 2},
		{"missing comma", "{\"a\": \"Doe, Jane\" \"b\": 1}", finding.UnparseableJSON, 1},
		{"trailing data", "{\"a\": 1}\n\"Doe, Jane\"", finding.UnparseableJSON, 2},
		{"empty", "", finding.UnparseableJSON, 1},
		{"not UTF-8", "{\"a\": \"Doe, Jane\xff\"}", finding.NotUTF8, 0},
	}
	p := loadPolicy(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pbir.Scan(testPath, []byte(tt.doc), p)
			if got != nil {
				t.Errorf("findings returned with an error")
			}
			var perr *judge.ParseError
			if !errors.As(err, &perr) {
				t.Fatalf("err = %T, want *judge.ParseError", err)
			}
			if perr.Reason != tt.reason || perr.Line != tt.line {
				t.Errorf("reason %s line %d, want %s line %d", perr.Reason, perr.Line, tt.reason, tt.line)
			}
			for _, s := range []string{sentinel, "Doe", "Jane"} {
				if strings.Contains(err.Error(), s) {
					t.Errorf("the error message carries source text")
				}
			}
		})
	}
}
