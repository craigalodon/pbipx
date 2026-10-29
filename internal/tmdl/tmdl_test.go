package tmdl_test

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/tmdl"
)

// sentinel is planted in every error case: no message may carry it.
const sentinel = "Doe, Jane"

func parse(t *testing.T, src string) *tmdl.Node {
	t.Helper()
	root, err := tmdl.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return root
}

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []*tmdl.Node
	}{
		{
			name: "declaration with properties",
			src:  "table student\n\n\tcolumn full_name\n\t\tdataType: string\n\t\tsourceColumn: full_name\n",
			want: []*tmdl.Node{{
				Kind: "table", Name: "student", Line: 1,
				Children: []*tmdl.Node{{
					Kind: "column", Name: "full_name", Line: 3,
					Children: []*tmdl.Node{
						{Kind: "dataType", Sep: tmdl.Colon, Value: "string", Line: 4, ValueLine: 4},
						{Kind: "sourceColumn", Sep: tmdl.Colon, Value: "full_name", Line: 5, ValueLine: 5},
					},
				}},
			}},
		},
		{
			name: "property value keeps inner spaces and trims the ends",
			src:  "table t\n\tdisplayFolder:   Doe, Jane  \n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "displayFolder", Sep: tmdl.Colon, Value: "Doe, Jane", Line: 2, ValueLine: 2},
			}}},
		},
		{
			name: "empty property value",
			src:  "table t\n\tdescription:\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "description", Sep: tmdl.Colon, Line: 2, ValueLine: 2},
			}}},
		},
		{
			name: "bare flags",
			src:  "table t\n\tcolumn c\n\t\tisHidden\n\t\tisKey\n\tcalculationGroup\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "column", Name: "c", Line: 2, Children: []*tmdl.Node{
					{Kind: "isHidden", Line: 3},
					{Kind: "isKey", Line: 4},
				}},
				{Kind: "calculationGroup", Line: 5},
			}}},
		},
		{
			name: "quoted name with a doubled quote",
			src:  "table 'student''s roster'\n",
			want: []*tmdl.Node{{Kind: "table", Name: "student's roster", Line: 1}},
		},
		{
			name: "unquoted name with hyphens",
			src:  "relationship 00000000-0000-0000-0000-000000000000\n\tfromColumn: enrollment.student_id\n",
			want: []*tmdl.Node{{Kind: "relationship", Name: "00000000-0000-0000-0000-000000000000", Line: 1, Children: []*tmdl.Node{
				{Kind: "fromColumn", Sep: tmdl.Colon, Value: "enrollment.student_id", Line: 2, ValueLine: 2},
			}}},
		},
		{
			name: "named expression on one line",
			src:  "table t\n\tmeasure 'Active Rows' = COUNTROWS(t)\n\t\tformatString: 0\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "Active Rows", Sep: tmdl.Equals, Value: "COUNTROWS(t)", Line: 2, ValueLine: 2, Children: []*tmdl.Node{
					{Kind: "formatString", Sep: tmdl.Colon, Value: "0", Line: 3, ValueLine: 3},
				}},
			}}},
		},
		{
			name: "name followed directly by equals",
			src:  "table t\n\tmeasure m=1\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "m", Sep: tmdl.Equals, Value: "1", Line: 2, ValueLine: 2},
			}}},
		},
		{
			name: "unnamed expression",
			src:  "table t\n\tpartition p = calculated\n\t\tsource = {\"Doe, Jane\"}\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "partition", Name: "p", Sep: tmdl.Equals, Value: "calculated", Line: 2, ValueLine: 2, Children: []*tmdl.Node{
					{Kind: "source", Sep: tmdl.Equals, Value: `{"Doe, Jane"}`, Line: 3, ValueLine: 3},
				}},
			}}},
		},
		{
			name: "multi-line expression keeps relative indentation and inner blank lines",
			src: "table t\n\tpartition p = m\n\t\tsource =\n\t\t\t\tlet\n\t\t\t\t\tSource = 1\n\n\t\t\t\tin\n\t\t\t\t\tSource\n\n" +
				"\t\tmode: import\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "partition", Name: "p", Sep: tmdl.Equals, Value: "m", Line: 2, ValueLine: 2, Children: []*tmdl.Node{
					{Kind: "source", Sep: tmdl.Equals, Value: "let\n\tSource = 1\n\nin\n\tSource", Line: 3, ValueLine: 4},
					{Kind: "mode", Sep: tmdl.Colon, Value: "import", Line: 10, ValueLine: 10},
				}},
			}}},
		},
		{
			name: "expression continued after text on the declaration line",
			src:  "table t\n\tmeasure m = CALCULATE(\n\t\t\tCOUNTROWS(t))\n\t\tformatString: 0\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "m", Sep: tmdl.Equals, Value: "CALCULATE(\nCOUNTROWS(t))", Line: 2, ValueLine: 2, Children: []*tmdl.Node{
					{Kind: "formatString", Sep: tmdl.Colon, Value: "0", Line: 4, ValueLine: 4},
				}},
			}}},
		},
		{
			name: "expression body may hold lines that look like statements or descriptions",
			src:  "table t\n\tmeasure m =\n\t\t\t/// not a description\n\t\t\t  1 + 'x\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "m", Sep: tmdl.Equals, Value: "/// not a description\n  1 + 'x", Line: 2, ValueLine: 3},
			}}},
		},
		{
			name: "empty expression",
			src:  "table t\n\tmeasure m =\n\t\tformatString: 0\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "m", Sep: tmdl.Equals, Line: 2, ValueLine: 2, Children: []*tmdl.Node{
					{Kind: "formatString", Sep: tmdl.Colon, Value: "0", Line: 3, ValueLine: 3},
				}},
			}}},
		},
		{
			name: "fenced expression ignores indentation inside",
			src:  "table t\n\tmeasure m = ```\n\t\t\t\tVAR x = 1\n\t  RETURN x\n\n\t\t\t```\n\t\tformatString: 0\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "m", Sep: tmdl.Equals, Value: "\t\t\tVAR x = 1\n  RETURN x\n", Line: 2, ValueLine: 3, Children: []*tmdl.Node{
					{Kind: "formatString", Sep: tmdl.Colon, Value: "0", Line: 7, ValueLine: 7},
				}},
			}}},
		},
		{
			name: "fenced expression on one line",
			src:  "table t\n\tmeasure m = ```1 + 2```\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "m", Sep: tmdl.Equals, Value: "1 + 2", Line: 2, ValueLine: 2},
			}}},
		},
		{
			name: "ref keeps the rest as its name",
			src:  "model Model\n\tculture: en-US\n\nref table 'student roster'\nref cultureInfo en-US\n",
			want: []*tmdl.Node{
				{Kind: "model", Name: "Model", Line: 1, Children: []*tmdl.Node{
					{Kind: "culture", Sep: tmdl.Colon, Value: "en-US", Line: 2, ValueLine: 2},
				}},
				{Kind: "ref", Name: "table 'student roster'", Line: 4},
				{Kind: "ref", Name: "cultureInfo en-US", Line: 5},
			},
		},
		{
			name: "description attaches to the next declaration",
			src:  "table t\n\t/// Owner: Doe, Jane\n\t///   second line\n\n\tmeasure m = 1\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "m", Sep: tmdl.Equals, Value: "1", Line: 5, ValueLine: 5, Description: "Owner: Doe, Jane\nsecond line"},
			}}},
		},
		{
			name: "trailing description becomes its own node",
			src:  "table t\n\tmeasure m = 1\n\n/// call 555-0142\n",
			want: []*tmdl.Node{
				{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
					{Kind: "measure", Name: "m", Sep: tmdl.Equals, Value: "1", Line: 2, ValueLine: 2},
				}},
				{Kind: tmdl.DescriptionKind, Description: "call 555-0142", Line: 4, ValueLine: 4},
			},
		},
		{
			name: "trailing nested description stays at its depth",
			src:  "table t\n\tcolumn c\n\t\t/// note\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "column", Name: "c", Line: 2, Children: []*tmdl.Node{
					{Kind: tmdl.DescriptionKind, Description: "note", Line: 3, ValueLine: 3},
				}},
			}}},
		},
		{
			name: "CRLF and a byte order mark",
			src:  "\xef\xbb\xbftable t\r\n\tmeasure m =\r\n\t\t\tA\r\n\r\n\t\t\tB\r\n",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "measure", Name: "m", Sep: tmdl.Equals, Value: "A\n\nB", Line: 2, ValueLine: 3},
			}}},
		},
		{
			name: "lone CR",
			src:  "table t\r\tisHidden\r",
			want: []*tmdl.Node{{Kind: "table", Name: "t", Line: 1, Children: []*tmdl.Node{
				{Kind: "isHidden", Line: 2},
			}}},
		},
		{
			name: "dedent back several levels",
			src:  "table a\n\tcolumn c\n\t\tisHidden\ntable b\n",
			want: []*tmdl.Node{
				{Kind: "table", Name: "a", Line: 1, Children: []*tmdl.Node{
					{Kind: "column", Name: "c", Line: 2, Children: []*tmdl.Node{{Kind: "isHidden", Line: 3}}},
				}},
				{Kind: "table", Name: "b", Line: 4},
			},
		},
		{
			name: "empty file",
			src:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := parse(t, tc.src)
			if root.Kind != "" {
				t.Errorf("root kind is not empty")
			}
			if !reflect.DeepEqual(root.Children, tc.want) {
				t.Errorf("tree mismatch\n got: %s\nwant: %s", dump(root.Children), dump(tc.want))
			}
		})
	}
}

// dump renders a tree for a failure message. The inputs are synthetic.
func dump(ns []*tmdl.Node) string {
	var b strings.Builder
	var walk func([]*tmdl.Node, int)
	walk = func(ns []*tmdl.Node, d int) {
		for _, n := range ns {
			b.WriteString("\n" + strings.Repeat("  ", d))
			b.WriteString(strings.Join([]string{n.Kind, n.Name}, " "))
			b.WriteString(" sep=" + itoa(int(n.Sep)))
			b.WriteString(" value=" + strings.ReplaceAll(n.Value, "\n", `\n`))
			b.WriteString(" desc=" + strings.ReplaceAll(n.Description, "\n", `\n`))
			b.WriteString(" line=" + itoa(n.Line) + " valueLine=" + itoa(n.ValueLine))
			walk(n.Children, d+1)
		}
	}
	walk(ns, 0)
	return b.String()
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int
		msg  string
	}{
		{"space indentation", "table t\n    column c\n\t\tdisplayFolder: Doe, Jane\n", 2, "space indentation"},
		{"tab then space indentation", "table t\n\t column c\n\t\tdisplayFolder: Doe, Jane\n", 2, "space indentation"},
		{"space before a description", "table t\n  /// Doe, Jane\n\tcolumn c\n", 2, "space indentation"},
		{"indentation jump", "table t\n\tcolumn c\n\t\t\tdisplayFolder: Doe, Jane\n", 3, "indentation jumps more than one level"},
		{"indentation jump at the top", "\ttable t\n\tdisplayFolder: Doe, Jane\n", 1, "indentation jumps more than one level"},
		{"description jump", "table t\n\t\t/// Doe, Jane\n", 2, "indentation jumps more than one level"},
		{"statement starts with a quote", "table t\n\t'Doe, Jane'\n", 2, "statement does not start with an identifier"},
		{"statement starts with a digit", "table t\n\t1 Doe, Jane\n", 2, "statement does not start with an identifier"},
		{"comment at statement level", "table t\n\t// Doe, Jane\n", 2, "statement does not start with an identifier"},
		{"text glued to the kind", "table t\n\tcolumn-x 'Doe, Jane'\n", 2, "unexpected text after a kind"},
		{"unterminated quoted name", "table t\n\tcolumn 'Doe, Jane\n\t\tdataType: string\n", 2, "unterminated quoted name"},
		{"missing name", "table t\n\tcolumn\n\t\tdisplayFolder: Doe, Jane\n", 2, "missing name"},
		{"missing name before an expression", "table t\n\tmeasure = \"Doe, Jane\"\n", 2, "missing name"},
		{"missing name after ref", "ref \n\tdisplayFolder: Doe, Jane\n", 1, "missing name"},
		{"text after a name", "table t\n\tcolumn c Doe, Jane\n", 2, "unexpected text after a name"},
		{"text after a quoted name", "table t\n\tcolumn 'c' 'Doe, Jane'\n", 2, "unexpected text after a name"},
		{"colon after a name", "table t\n\tcolumn c: Doe, Jane\n", 2, "unexpected text after a name"},
		{"unterminated fence", "table t\n\tmeasure m = ```\n\t\t\t\"Doe, Jane\"\n\n\t\tformatString: 0\n", 2, "unterminated fenced expression"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.src, sentinel) {
				t.Fatal("the case does not plant the sentinel")
			}
			root, err := tmdl.Parse([]byte(tc.src))
			if root != nil {
				t.Error("a tree returned with an error")
			}
			var perr *judge.ParseError
			if !errors.As(err, &perr) {
				t.Fatalf("got %v, want a *judge.ParseError", err)
			}
			if perr.Reason != finding.UnparseableTMDL {
				t.Errorf("reason %q", perr.Reason)
			}
			if perr.Line != tc.line {
				t.Errorf("line %d, want %d", perr.Line, tc.line)
			}
			if perr.Msg != tc.msg {
				t.Errorf("message %q, want %q", perr.Msg, tc.msg)
			}
			for _, part := range []string{sentinel, "Doe", "Jane"} {
				if strings.Contains(err.Error(), part) {
					t.Error("the error carries source text")
				}
			}
		})
	}
}
