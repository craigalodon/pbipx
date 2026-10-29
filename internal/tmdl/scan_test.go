package tmdl_test

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/secret"
	"github.com/craigalodon/pbipx/internal/tmdl"
)

const scanPolicy = `version = 1
protected_roots = ["pbip"]
[scan]
model = ["pbip/**/*.tmdl"]
[columns]
block = ["full_name", "last_name", "student_id", "email"]
warn  = ["cohort_year"]
[values]
allow = ["(none selected)"]
`

const scanPath = "pbip/School.SemanticModel/definition/tables/student.tmdl"

func loadScanPolicy(t *testing.T) *policy.Policy {
	t.Helper()
	path := filepath.Join(t.TempDir(), policy.FileName)
	if err := os.WriteFile(path, []byte(scanPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// got is a finding without its value.
type got struct {
	ctx     finding.Context
	column  string
	tier    finding.Tier
	reasons string
	locator string
}

func summarizeFindings(fs []finding.Finding) []got {
	out := make([]got, 0, len(fs))
	for _, f := range fs {
		g := got{ctx: f.Context, tier: f.Tier, locator: f.Locator}
		if f.Column != nil {
			g.column = f.Column.String()
		}
		rs := make([]string, len(f.Reasons))
		for i, r := range f.Reasons {
			rs[i] = string(r)
		}
		g.reasons = strings.Join(rs, ",")
		out = append(out, g)
	}
	return out
}

func scanTMDL(t *testing.T, src string) []got {
	t.Helper()
	fs, err := tmdl.Scan(scanPath, []byte(src), loadScanPolicy(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return summarizeFindings(fs)
}

func check(t *testing.T, name string, have []got, want ...got) {
	t.Helper()
	slices.SortFunc(have, cmpGot)
	slices.SortFunc(want, cmpGot)
	if !slices.Equal(have, want) {
		t.Errorf("%s:\n have %+v\n want %+v", name, have, want)
	}
}

func cmpGot(a, b got) int {
	return strings.Compare(a.locator+"|"+string(a.ctx)+"|"+a.reasons, b.locator+"|"+string(b.ctx)+"|"+b.reasons)
}

func block(ctx finding.Context, column, reasons, locator string) got {
	return got{ctx: ctx, column: column, tier: finding.Block, reasons: reasons, locator: locator}
}

// blob encodes an Enter data blob as Power BI writes it: raw deflate, then
// base64.
func blob(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestScanDAX(t *testing.T) {
	check(t, "comparison", scanTMDL(t,
		"table student\n\tmeasure Count = CALCULATE(COUNTROWS(student), student[full_name] = \"Jane\", student[campus] = \"North\")\n"),
		block(finding.TMDLDAX, "student.full_name", "block-column", "table[student]/measure[Count]#lit0"))

	check(t, "numbers by column only", scanTMDL(t,
		"table student\n\tmeasure Count = IF(COUNTROWS(student) > 5550100, student[student_id] = 1, student[cohort_year] = 2101)\n"),
		block(finding.TMDLDAX, "student.student_id", "block-column", "table[student]/measure[Count]#lit1"),
		got{ctx: finding.TMDLDAX, column: "student.cohort_year", tier: finding.Warn, reasons: "warn-column", locator: "table[student]/measure[Count]#lit2"})

	check(t, "a bare DAX column stays bare", scanTMDL(t,
		"table student\n\tcolumn Flag = IF([last_name] = \"Doe\", 1, 0)\n"),
		block(finding.TMDLDAX, "last_name", "block-column", "table[student]/column[Flag]#lit0"))

	check(t, "DAX-valued properties and an unnamed expression", scanTMDL(t,
		"table pick\n"+
			"\tcalculationGroup\n"+
			"\t\tcalculationItem Only = IF(SELECTEDVALUE(student[email]) = \"x\", 1)\n"+
			"\t\t\tformatStringDefinition = IF(student[full_name] = \"y\", \"0\")\n"+
			"\t\tmultipleOrEmptySelectionExpression = IF(student[last_name] = \"z\", 1)\n"+
			"\t\t\texpression = student[student_id] = \"w\"\n"),
		block(finding.TMDLDAX, "student.email", "block-column", "table[pick]/calculationGroup/calculationItem[Only]#lit0"),
		block(finding.TMDLDAX, "student.full_name", "block-column", "table[pick]/calculationGroup/calculationItem[Only]/formatStringDefinition#lit0"),
		block(finding.TMDLDAX, "student.last_name", "block-column", "table[pick]/calculationGroup/multipleOrEmptySelectionExpression#lit0"),
		block(finding.TMDLDAX, "student.student_id", "block-column", "table[pick]/calculationGroup/multipleOrEmptySelectionExpression/expression#lit0"))

	check(t, "a table's default detail rows are DAX", scanTMDL(t,
		"table student\n\tdefaultDetailRowsDefinition = FILTER(student, student[full_name] = \"x\")\n"),
		block(finding.TMDLDAX, "student.full_name", "block-column", "table[student]/defaultDetailRowsDefinition#lit0"))

	check(t, "role table permission", scanTMDL(t,
		"role self\n\ttablePermission student = student[email] = \"jane.doe@example.com\"\n"),
		block(finding.TMDLDAX, "student.email", "block-column,email-shape", "role[self]/tablePermission[student]#lit0"))

	check(t, "allowlisted", scanTMDL(t,
		"table student\n\tmeasure Count = student[full_name] = \"(none selected)\"\n"))
}

func TestScanDAXTables(t *testing.T) {
	check(t, "DATATABLE rows take the headers, qualified", scanTMDL(t,
		"table roster\n\tcolumn full_name\n\tpartition roster = calculated\n\t\tsource = DATATABLE(\"full_name\", STRING, \"code\", INTEGER, {{\"Jane\", 1}})\n"),
		block(finding.TMDLDAX, "roster.full_name", "block-column", "table[roster]/partition[roster]/source#lit2"),
		block(finding.TMDLDAX, "roster.full_name", "block-column", "table[roster]/partition[roster]/source#lit3"))

	check(t, "a constructor in a calculated table takes every column", scanTMDL(t,
		"table pick\n\tcolumn code\n\tcolumn last_name\n\tpartition pick = calculated\n\t\tsource = {(\"A\", \"Doe\")}\n"),
		block(finding.TMDLDAX, "pick.last_name", "block-column", "table[pick]/partition[pick]/source#lit0"),
		block(finding.TMDLDAX, "pick.last_name", "block-column", "table[pick]/partition[pick]/source#lit1"))

	check(t, "a constructor in a measure is not a table", scanTMDL(t,
		"table pick\n\tcolumn last_name\n\tmeasure Count = COUNTROWS({\"Doe\"})\n"))
}

func TestScanM(t *testing.T) {
	check(t, "an M column takes the declared table", scanTMDL(t,
		"table advisee\n\tpartition advisee = m\n\t\tsource =\n\t\t\t\tlet\n\t\t\t\t\tF = Table.SelectRows(S, each [last_name] = \"Doe\")\n\t\t\t\tin\n\t\t\t\t\tF\n"),
		block(finding.TMDLM, "advisee.last_name", "block-column", "table[advisee]/partition[advisee]/source#lit0"))

	check(t, "a named expression is M, with no table", scanTMDL(t,
		"expression Pick = Table.SelectRows(S, each Text.Contains([email], \"jane\"))\n"),
		block(finding.TMDLM, "email", "block-column", "expression[Pick]#lit0"))

	check(t, "refresh policy expressions and policyRange are M", scanTMDL(t,
		"table student\n"+
			"\trefreshPolicy\n"+
			"\t\tsourceExpression = Table.SelectRows(S, each [email] = \"a\")\n"+
			"\t\tpollingExpression = Table.SelectRows(S, each [email] = \"b\")\n"+
			"\tpartition p = policyRange\n"+
			"\t\tsource = Table.SelectRows(S, each [email] = \"c\")\n"),
		block(finding.TMDLM, "student.email", "block-column", "table[student]/refreshPolicy/sourceExpression#lit0"),
		block(finding.TMDLM, "student.email", "block-column", "table[student]/refreshPolicy/pollingExpression#lit0"),
		block(finding.TMDLM, "student.email", "block-column", "table[student]/partition[p]/source#lit0"))
}

func TestScanNativeSQL(t *testing.T) {
	src := "table student\n\tpartition student = m\n\t\tsource =\n\t\t\t\tlet\n" +
		"\t\t\t\t\tQ = Value.NativeQuery(S, \"select a from student where full_name = 'Doe' and s.student_id = 7 -- or last_name = 'Roe'\")\n" +
		"\t\t\t\tin\n\t\t\t\t\tQ\n"
	at := "table[student]/partition[student]/source#lit0"
	check(t, "SQL in an M string", scanTMDL(t, src),
		// A bare SQL column stays bare; a qualified one keeps its table.
		block(finding.TMDLNativeSQL, "full_name", "block-column", at+"/sql#lit0"),
		block(finding.TMDLNativeSQL, "s.student_id", "block-column", at+"/sql#lit1"),
		block(finding.TMDLComment, "last_name", "block-column", at+"/sql#comment0.compared0"))

	check(t, "the M string is also an M literal", scanTMDL(t,
		"expression Q = Value.NativeQuery(S, \"select a from t where x = 'jane.doe@example.com'\")\n"),
		block(finding.TMDLM, "", "email-shape", "expression[Q]#lit0"),
		block(finding.TMDLNativeSQL, "", "email-shape", "expression[Q]#lit0/sql#lit0"))
}

func TestScanEnterData(t *testing.T) {
	typed := func(b64 string) string {
		return "table sample\n\tcolumn first_name\n\tpartition sample = m\n\t\tsource =\n\t\t\t\tlet\n" +
			"\t\t\t\t\tSource = Table.FromRows(Json.Document(Binary.Decompress(Binary.FromText(\"" + b64 +
			"\", BinaryEncoding.Base64), Compression.Deflate)), let _t = ((type nullable text) meta [Serialized.Text = true]) in type table [full_name = _t, #\"code x\" = _t])\n" +
			"\t\t\t\tin\n\t\t\t\t\tSource\n"
	}
	untyped := func(b64 string) string {
		return "table sample\n\tcolumn first_name\n\tcolumn last_name\n\tpartition sample = m\n\t\tsource =\n\t\t\t\tlet\n" +
			"\t\t\t\t\tSource = Table.FromRows(Json.Document(Binary.Decompress(Binary.FromText(\"" + b64 +
			"\", BinaryEncoding.Base64), Compression.Deflate)))\n" +
			"\t\t\t\tin\n\t\t\t\t\tSource\n"
	}
	at := "table[sample]/partition[sample]/source#blob0"

	check(t, "typed: cells take the type table's fields", scanTMDL(t, typed(blob(t, `[["Doe, Jane","A"],[true,null]]`))),
		block(finding.TMDLEnterData, "sample.full_name", "block-column,person-name-shape", at+"/cell0"),
		block(finding.TMDLEnterData, "sample.full_name", "block-column", at+"/cell1"))

	check(t, "untyped: cells take the table's columns; numbers count, booleans and nulls do not", scanTMDL(t, untyped(blob(t, `[["Jane",5550100,false,null]]`))),
		block(finding.TMDLEnterData, "sample.last_name", "block-column", at+"/cell0"),
		block(finding.TMDLEnterData, "sample.last_name", "block-column", at+"/cell1"))

	check(t, "plain base64 JSON", scanTMDL(t, untyped(base64.StdEncoding.EncodeToString([]byte(`[["x"]]`)))),
		block(finding.TMDLEnterData, "sample.last_name", "block-column", at+"/cell0"))

	check(t, "text that is not JSON is one cell", scanTMDL(t, untyped(blob(t, "Doe, Jane and more"))),
		block(finding.TMDLEnterData, "sample.last_name", "block-column,person-name-shape", at+"/cell0"))
}

func TestScanProperties(t *testing.T) {
	check(t, "a property is judged on shape only", scanTMDL(t,
		"table student\n\tcolumn full_name\n\t\tsourceColumn: full_name\n\t\tdisplayFolder: 'Roe, Richard'\n\t\tisHidden\n"),
		block(finding.TMDLProperty, "", "person-name-shape", "table[student]/column[full_name]/displayFolder"))
}

func TestScanDescriptions(t *testing.T) {
	check(t, "whole, quoted runs, and trailing", scanTMDL(t,
		"table student\n\t/// Owner jane.doe@example.com\n\tmeasure Count = 1\n\t/// asked for by \"Roe, Richard\"\n\tcolumn a\n\n/// call 555-555-0142\n"),
		block(finding.TMDLDescription, "", "email-shape", "table[student]/measure[Count]#description"),
		block(finding.TMDLDescription, "", "person-name-shape", "table[student]/column[a]#description.quoted0"),
		block(finding.TMDLDescription, "", "phone-shape", "description#description"))
}

func TestScanComments(t *testing.T) {
	check(t, "DAX and M comments", scanTMDL(t,
		"table student\n\tmeasure Count =\n\t\t\t// Roe, Richard asked\n\t\t\t1 /* see \"Doe, Jane\" */\n"+
			"\tpartition student = m\n\t\tsource =\n\t\t\t\t// contact jane.doe@example.com\n\t\t\t\tS\n"),
		block(finding.TMDLComment, "", "person-name-shape", "table[student]/measure[Count]#comment0"),
		block(finding.TMDLComment, "", "person-name-shape", "table[student]/measure[Count]#comment1.quoted0"),
		block(finding.TMDLComment, "", "email-shape", "table[student]/partition[student]/source#comment0"))

	check(t, "a DAX comment is not lexed as SQL", scanTMDL(t,
		"table student\n\tmeasure Count =\n\t\t\t// where last_name = 'x'\n\t\t\t1\n"))
}

func TestScanText(t *testing.T) {
	check(t, "an annotation is judged whole", scanTMDL(t,
		"table student\n\tannotation Reviewer = jane.doe@example.com\n"),
		block(finding.TMDLText, "", "email-shape", "table[student]/annotation[Reviewer]"))

	check(t, "JSON strings one by one", scanTMDL(t,
		"table student\n\textendedProperty Meta = {\"owner\": \"jane.doe@example.com\", \"n\": 5550100, \"by\": \"Roe, Richard\"}\n"),
		block(finding.TMDLText, "", "email-shape", "table[student]/extendedProperty[Meta]#json1"),
		block(finding.TMDLText, "", "person-name-shape", "table[student]/extendedProperty[Meta]#json4"))

	check(t, "a partition type is not a value", scanTMDL(t,
		"table student\n\tpartition p = entity\n\t\tentityName: student\n"))
}

func TestScanRoleMembers(t *testing.T) {
	check(t, "every member blocks, by ordinal", scanTMDL(t,
		"role advisors\n\tmodelPermission: read\n\tmember 'jane.doe@example.com'\n\t\tmemberType: user\n\tmember '(none selected)'\n"),
		block(finding.TMDLRoleMember, "", "role-member", "role[advisors]/member#0"),
		block(finding.TMDLRoleMember, "", "role-member", "role[advisors]/member#1"))
}

func TestScanErrors(t *testing.T) {
	notUTF8 := base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0x00, 0x81})
	cases := []struct {
		name string
		src  string
		line int
	}{
		{"parse error", "table student\n    column x\n", 2},
		{"unterminated DAX string", "table student\n\n\tmeasure m =\n\t\t\tIF(1,\n\t\t\t\"" + sentinel + ")\n", 5},
		{"unterminated DAX name", "table student\n\tmeasure m = COUNTROWS('" + sentinel + ")\n", 2},
		{"unterminated M comment", "expression E =\n\t\tlet\n\t\t\t/* " + sentinel + "\n\t\tin 1\n", 3},
		{"unterminated native SQL string", "expression E =\n\t\tlet\n\t\t\tQ = Value.NativeQuery(S, \"select a from t where b = '" + sentinel + "\")\n\t\tin Q\n", 3},
		{"blob not base64", "expression E = Binary.FromText(\"" + sentinel + "\")\n", 1},
		{"blob neither JSON nor text", "expression E = Binary.FromText(\"" + notUTF8 + "\")\n", 1},
	}
	for _, tc := range cases {
		fs, err := tmdl.Scan(scanPath, []byte(tc.src), loadScanPolicy(t))
		var pe *judge.ParseError
		switch {
		case !errors.As(err, &pe):
			t.Errorf("%s: err = %v, want a parse error", tc.name, err)
			continue
		case len(fs) != 0:
			t.Errorf("%s: findings returned with an error", tc.name)
		case pe.Reason != finding.UnparseableTMDL || pe.Line != tc.line:
			t.Errorf("%s: reason %s line %d, want line %d", tc.name, pe.Reason, pe.Line, tc.line)
		}
		if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "Doe") {
			t.Errorf("%s: the error carries source text", tc.name)
		}
	}
}

// TestScanLocatorsCarryNoValue plants values wherever a name or an identity
// can go, and checks that no locator or error carries them, and that each
// finding carries its value only as a secret.
func TestScanLocatorsCarryNoValue(t *testing.T) {
	src := "table 'Doe, Jane'\n" +
		"\tcolumn 'Roe, Richard'\n" +
		"\t\tdisplayFolder: jane.doe@example.com\n" +
		"\tmeasure 'jane.doe@example.com' = [full_name] = \"Doe, Jane\"\n" +
		"\tannotation 'Roe, Richard' = Roe, Richard\n" +
		"\tpartition 'Doe, Jane' = m\n" +
		"\t\tsource = Value.NativeQuery(S, \"select a from t where full_name = 'Roe, Richard'\")\n" +
		"role 'Doe, Jane'\n" +
		"\tmember 'jane.doe@example.com'\n"
	fs, err := tmdl.Scan(scanPath, []byte(src), loadScanPolicy(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 5 {
		t.Fatalf("only %d findings", len(fs))
	}
	for _, f := range fs {
		for _, s := range []string{"Doe", "Roe", "jane"} {
			if strings.Contains(f.Locator, s) {
				t.Errorf("locator %q carries a planted value", f.Locator)
			}
		}
		if f.Value == (secret.Value{}) {
			t.Errorf("%s: no value", f.Locator)
		}
	}
	member := fs[slices.IndexFunc(fs, func(f finding.Finding) bool { return f.Context == finding.TMDLRoleMember })]
	if member.Locator != "role[{?}]/member#0" {
		t.Errorf("member locator %q", member.Locator)
	}
}

func TestScanFunctions(t *testing.T) {
	check(t, "a function body is DAX", scanTMDL(t,
		"function Pick = (x) => IF([student_id] = \"S-1001\", 1, 0)\n"),
		block(finding.TMDLDAX, "student_id", "block-column", "function[Pick]#lit0"))

	check(t, "a typed function body over lines", scanTMDL(t,
		"function Pick =\n\t\t( x : STRING ) =>\n\t\tCALCULATE(1, student[full_name] = \"Doe, Jane\") // Roe, Richard\n"),
		block(finding.TMDLDAX, "student.full_name", "block-column,person-name-shape", "function[Pick]#lit1"),
		block(finding.TMDLComment, "", "person-name-shape", "function[Pick]#comment0"))
}

func TestScanQueryPartitions(t *testing.T) {
	check(t, "a query property under a source", scanTMDL(t,
		"table student\n\tpartition student = query\n\t\tmode: import\n\t\tsource\n"+
			"\t\t\tquery = SELECT student_id FROM enrollment.student WHERE student_id = 'S-1001'\n"+
			"\t\t\tdataSource: School\n"),
		block(finding.TMDLNativeSQL, "student_id", "block-column", "table[student]/partition[student]/source/query#lit0"))

	check(t, "the source of a query partition", scanTMDL(t,
		"table student\n\tpartition student = query\n\t\tsource =\n"+
			"\t\t\t\tSELECT a FROM t s\n"+
			"\t\t\t\tWHERE s.full_name = \"Doe, Jane\" -- and last_name = 'Roe'\n"),
		// "..." is read as a string and as a name, as in a SQL file.
		block(finding.TMDLNativeSQL, "s.full_name", "block-column,person-name-shape", "table[student]/partition[student]/source#lit0"),
		block(finding.TMDLComment, "last_name", "block-column", "table[student]/partition[student]/source#comment0.compared0"))

	src := "table student\n\tpartition student = query\n\t\tsource\n\t\t\tquery =\n\t\t\t\t\tSELECT a FROM t\n\t\t\t\t\tWHERE b = 'Doe, Jane\n"
	fs, err := tmdl.Scan(scanPath, []byte(src), loadScanPolicy(t))
	var pe *judge.ParseError
	if !errors.As(err, &pe) || len(fs) != 0 || pe.Reason != finding.UnparseableTMDL || pe.Line != 6 {
		t.Errorf("an unterminated string in query SQL: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "Doe") {
		t.Error("the error carries source text")
	}
}

// A table name with a shape is judged by its real name and printed as
// {?}, so the column a finding prints, and the fingerprint that hashes it,
// carry no text that is not schema.
func TestScanColumnsArePrintable(t *testing.T) {
	check(t, "a quoted table in DAX", scanTMDL(t,
		"table 'Doe, Jane'\n\tmeasure m = CALCULATE(1, 'Doe, Jane'[full_name] = \"S-1\")\n"),
		block(finding.TMDLDAX, "{?}.full_name", "block-column", "table[{?}]/measure[m]#lit1"))

	check(t, "the declared table in M", scanTMDL(t,
		"table 'Doe, Jane'\n\tpartition p = m\n\t\tsource = Table.SelectRows(S, each [last_name] = \"x\")\n"),
		block(finding.TMDLM, "{?}.last_name", "block-column", "table[{?}]/partition[p]/source#lit0"))

	check(t, "a DATATABLE header", scanTMDL(t,
		"table 'Doe, Jane'\n\tpartition p = calculated\n\t\tsource = DATATABLE(\"full_name\", STRING, {{\"x\"}})\n"),
		block(finding.TMDLDAX, "{?}.full_name", "block-column", "table[{?}]/partition[p]/source#lit1"))
}
