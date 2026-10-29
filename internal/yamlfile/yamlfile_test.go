package yamlfile_test

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
	"github.com/craigalodon/pbipx/internal/yamlfile"
)

const testPolicy = `version = 1
protected_roots = ["pbip"]
[scan]
yaml = ["models/**/*.yml", "pbip/**/*.yml"]
[columns]
block = ["full_name", "first_name", "student_id", "preferred_name"]
block_pattern = "(^|_)email(_|$)"
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

const testPath = "models/student.yml"

func scanYAML(t *testing.T, src string) []finding.Finding {
	t.Helper()
	fs, err := yamlfile.Scan(testPath, []byte(src), loadPolicy(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return fs
}

// want is one expected finding; locator is compared when set. Findings are
// compared in finding.Sort order.
type want struct {
	ctx     finding.Context
	column  string
	tier    finding.Tier
	reasons []finding.Reason
	locator string
}

var (
	blockCol = []finding.Reason{finding.BlockColumn}
	warnCol  = []finding.Reason{finding.WarnColumn}
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

func TestScalars(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"value tied to its key", "meta:\n  full_name: Pat Example\n",
			[]want{{ctx: finding.YAML, column: "full_name", tier: finding.Block, reasons: blockCol, locator: "meta.full_name"}}},
		{"key by pattern", "meta:\n  contact_email: someone\n",
			[]want{{ctx: finding.YAML, column: "contact_email", tier: finding.Block, reasons: blockCol}}},
		{"sequence inherits the key", "first_name:\n  - Pat\n  - [Sam]\n",
			[]want{
				{ctx: finding.YAML, column: "first_name", tier: finding.Block, reasons: blockCol, locator: "first_name[0]"},
				{ctx: finding.YAML, column: "first_name", tier: finding.Block, reasons: blockCol, locator: "first_name[1][0]"},
			}},
		{"nearest key wins", "full_name:\n  label: Pat\n", nil},
		{"integer is a number", "student_id: 7041293\ncount: 5550142\n",
			[]want{{ctx: finding.YAML, column: "student_id", tier: finding.Block, reasons: blockCol}}},
		{"float is a number", "cohort_year: 2101.0\n",
			[]want{{ctx: finding.YAML, column: "cohort_year", tier: finding.Warn, reasons: warnCol}}},
		{"quoted digits are a string", "note: '555-0100'\n", nil},
		{"null, boolean, empty skipped", "full_name: null\nfirst_name: ~\nstudent_id: true\npreferred_name: ''\n", nil},
		{"shape with no key", "- Doe, Jane\n",
			[]want{{ctx: finding.YAML, tier: finding.Block, reasons: name, locator: "[0]"}}},
		{"block scalar", "note: |\n  Doe, Jane\n",
			[]want{{ctx: finding.YAML, tier: finding.Block, reasons: name}}},
		{"quoted run in a value", "description: The row \"Doe, Jane\" is a test.\n",
			[]want{{ctx: finding.YAML, tier: finding.Block, reasons: name, locator: "description.quoted0"}}},
		{"allowed literal", "full_name: (none selected)\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanYAML(t, tt.src), tt.wants...)
		})
	}
}

func TestKeys(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"key with a shape", "reviewers:\n  jane.doe@example.com: approved\n",
			[]want{{ctx: finding.YAMLKey, tier: finding.Block, reasons: email, locator: "reviewers.{?}#key"}}},
		{"a key that is not printable names no column", "\"Doe, Jane\": [x]\nfull_name_jane.doe@example.com: Pat\n",
			[]want{
				{ctx: finding.YAMLKey, tier: finding.Block, reasons: email, locator: "{?}#key"},
				{ctx: finding.YAMLKey, tier: finding.Block, reasons: name, locator: "{?}#key"},
			}},
		{"complex key", "? [Doe, Jane]\n: x\n", nil},
		{"complex key with a shape", "? {k: 'Doe, Jane'}\n: x\n",
			[]want{{ctx: finding.YAML, tier: finding.Block, reasons: name, locator: "{?}.k"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanYAML(t, tt.src), tt.wants...)
		})
	}
}

func TestAcceptedValues(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"column from columns[].name", `
columns:
  - name: preferred_name
    data_tests:
      - accepted_values:
          values: [Pat]
  - name: section
    tests:
      - accepted_values:
          values: [Pat]
`, []want{{ctx: finding.YAML, column: "preferred_name", tier: finding.Block, reasons: blockCol}}},
		{"column_name", `
data_tests:
  - accepted_values:
      column_name: cohort_year
      values: [2101]
`, []want{{ctx: finding.YAML, column: "cohort_year", tier: finding.Warn, reasons: warnCol}}},
		{"arguments", `
columns:
  - name: first_name
    data_tests:
      - accepted_values:
          arguments:
            values: [Pat]
`, []want{{ctx: finding.YAML, column: "first_name", tier: finding.Block, reasons: blockCol}}},
		{"only the values", `
columns:
  - name: first_name
    data_tests:
      - accepted_values:
          values: [Pat]
          config:
            severity: warn
      - unique:
          values: [Sam]
`, []want{{ctx: finding.YAML, column: "first_name", tier: finding.Block, reasons: blockCol}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanYAML(t, tt.src), tt.wants...)
		})
	}
}

func TestSQLValues(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"where", "config:\n  where: \"first_name = 'Pat'\"\n",
			[]want{{ctx: finding.YAMLSQL, column: "first_name", tier: finding.Block, reasons: blockCol, locator: "config.where#lit0"}}},
		{"expression", "expression: student_id <> 7041293\n",
			[]want{{ctx: finding.YAMLSQL, column: "student_id", tier: finding.Block, reasons: blockCol}}},
		{"with jinja", "where: \"first_name = '{{ var(\\\"x\\\") }}' or full_name in ('Pat')\"\n",
			[]want{
				{ctx: finding.YAMLSQL, column: "first_name", tier: finding.Block, reasons: blockCol},
				{ctx: finding.YAMLSQL, column: "full_name", tier: finding.Block, reasons: blockCol},
			}},
		{"a SQL comment", "where: |\n  -- Doe, Jane\n  1 = 1\n",
			[]want{{ctx: finding.YAMLSQL, tier: finding.Block, reasons: name}}},
		{"unlexable degrades to free text", "where: \"the student's first_name = 'Pat'\"\n",
			[]want{{ctx: finding.YAMLSQL, column: "first_name", tier: finding.Block, reasons: blockCol}}},
		{"other keys are not SQL", "description: \"first_name = 'Pat'\"\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanYAML(t, tt.src), tt.wants...)
		})
	}
}

func TestUnitTestRows(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"dict rows", `
unit_tests:
  - name: t
    given:
      - input: ref('student')
        rows:
          - {student_id: 7041293, first_name: Pat, full_name: "Doe, Jane"}
`, []want{{ctx: finding.YAML, tier: finding.Block, reasons: name, locator: "unit_tests[0].given[0].rows[0].full_name"}}},
		{"csv rows", `
unit_tests:
  - name: t
    given:
      - input: ref('student')
        format: csv
        rows: |
          student_id,full_name,email
          7041293,"Doe, Jane",jane.doe@example.com
`, []want{
			{ctx: finding.YAML, tier: finding.Block, reasons: email, locator: "unit_tests[0].given[0].rows#cell[1][2]"},
			{ctx: finding.YAML, tier: finding.Block, reasons: name, locator: "unit_tests[0].given[0].rows#cell[1][1]"},
		}},
		{"sql rows", `
unit_tests:
  - name: t
    expect:
      format: sql
      rows: |
        select 7041293 as student_id, 'Pat' as first_name
        union all
        select 1, 'Doe, Jane' -- where first_name = 'Sam'
`, []want{{ctx: finding.YAML, tier: finding.Block, reasons: name}}},
		{"rows outside unit tests are values", "seeds:\n  rows:\n    - {first_name: Pat}\n",
			[]want{{ctx: finding.YAML, column: "first_name", tier: finding.Block, reasons: blockCol}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanYAML(t, tt.src), tt.wants...)
		})
	}
}

func TestComments(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"line comment", "# Doe, Jane\nversion: 2\n",
			[]want{{ctx: finding.YAMLComment, tier: finding.Block, reasons: name, locator: "comment[0]"}}},
		{"trailing comment", "name: student # jane.doe@example.com\n",
			[]want{{ctx: finding.YAMLComment, tier: finding.Block, reasons: email}}},
		{"quoted run in a comment", "a: 1 # owner 'Doe, Jane'\n",
			[]want{{ctx: finding.YAMLComment, tier: finding.Block, reasons: name, locator: "comment[0].quoted0"}}},
		{"a comment is not SQL", "a: 1 # full_name = 'Pat'\n", nil},
		{"# inside a plain scalar", "a: x#jane.doe@example.com\n",
			[]want{{ctx: finding.YAML, tier: finding.Block, reasons: email}}},
		{"# inside quotes", "a: 'x # jane.doe@example.com'\nb: \"y # Doe, Jane\"\n",
			[]want{{ctx: finding.YAML, tier: finding.Block, reasons: email}}},
		{"# inside a multi-line quote", "a: 'x\n  # jane.doe@example.com'\n",
			[]want{{ctx: finding.YAML, tier: finding.Block, reasons: email}}},
		{"# inside a block scalar", "a: |\n  x\n  # jane.doe@example.com\nb: 1 # Doe, Jane\n",
			[]want{
				{ctx: finding.YAML, tier: finding.Block, reasons: email},
				{ctx: finding.YAMLComment, tier: finding.Block, reasons: name},
			}},
		{"block scalar in a sequence entry", "- k: >-\n    x\n    # jane.doe@example.com\n  j: 1 # Doe, Jane\n",
			[]want{
				{ctx: finding.YAML, tier: finding.Block, reasons: email},
				{ctx: finding.YAMLComment, tier: finding.Block, reasons: name},
			}},
		{"comment after a block header", "a: | # Doe, Jane\n  x\n",
			[]want{{ctx: finding.YAMLComment, tier: finding.Block, reasons: name}}},
		{"comment in a flow collection", "a: [x, # Doe, Jane\n  y]\n",
			[]want{{ctx: finding.YAMLComment, tier: finding.Block, reasons: name}}},
		{"comment after a document marker", "--- # Doe, Jane\na: 1\n",
			[]want{{ctx: finding.YAMLComment, tier: finding.Block, reasons: name}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanYAML(t, tt.src), tt.wants...)
		})
	}
}

func TestAnchorsAndDocuments(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		wants []want
	}{
		{"alias walked once", "a: &x\n  full_name: Pat\nb: *x\nc:\n  <<: *x\n",
			[]want{{ctx: finding.YAML, column: "full_name", tier: finding.Block, reasons: blockCol, locator: "a.full_name"}}},
		{"anchored scalar", "full_name: &n Pat\nfirst_name: *n\n",
			[]want{{ctx: finding.YAML, column: "full_name", tier: finding.Block, reasons: blockCol}}},
		{"every document", "a: 1\n---\nfirst_name: Pat\n---\n- Doe, Jane\n",
			[]want{
				{ctx: finding.YAML, tier: finding.Block, reasons: name, locator: "doc[2][0]"},
				{ctx: finding.YAML, column: "first_name", tier: finding.Block, reasons: blockCol, locator: "doc[1].first_name"},
			}},
		{"empty", "", nil},
		{"comments only", "# Doe, Jane\n",
			[]want{{ctx: finding.YAMLComment, tier: finding.Block, reasons: name}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, scanYAML(t, tt.src), tt.wants...)
		})
	}
}

// The line is the one the parser's message names, which is where it was
// when it failed (an unclosed collection is reported at its start), or none.
func TestParseErrorsCarryNoSourceText(t *testing.T) {
	tests := []struct {
		name string
		src  string
		line int
	}{
		{"unclosed flow", "a:\n  values: [\"Doe, Jane\"\n", 1},
		{"bad indentation", "a:\n  b: Doe\n c: Jane\n", 2},
		{"unknown alias", "a: 1\nb: *Doe_Jane\n", 0},
		{"tab indentation", "a:\n\tb: Doe, Jane\n", 2},
		{"unterminated quote", "a: 'Doe, Jane\n", 2},
		{"error in a later document", "a: 1\n---\nb: [Doe, Jane\n", 2},
	}
	p := loadPolicy(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := yamlfile.Scan(testPath, []byte(tt.src), p)
			if got != nil {
				t.Errorf("findings returned with an error")
			}
			var perr *judge.ParseError
			if !errors.As(err, &perr) {
				t.Fatalf("err = %T, want *judge.ParseError", err)
			}
			if perr.Reason != finding.UnparseableYAML || perr.Line != tt.line {
				t.Errorf("reason %s line %d, want %s line %d", perr.Reason, perr.Line, finding.UnparseableYAML, tt.line)
			}
			for _, s := range []string{"Doe", "Jane"} {
				if strings.Contains(err.Error(), s) {
					t.Errorf("the error message carries source text")
				}
			}
		})
	}
}

func TestLocatorsCarryNoValue(t *testing.T) {
	src := `# Doe, Jane
"Doe, Jane":
  jane.doe@example.com: "Doe, Jane"
  full_name: [Doe, Jane]
  where: "full_name = 'Doe, Jane'"
  ? [Doe, Jane]
  : Doe, Jane
unit_tests:
  - given:
      - format: csv
        rows: |
          a
          "Doe, Jane"
`
	got := scanYAML(t, src)
	if len(got) < 8 {
		t.Fatalf("got %d findings:\n%s", len(got), describe(got))
	}
	for _, f := range got {
		for _, s := range []string{"Doe", "Jane", "jane"} {
			if strings.Contains(f.Locator, s) {
				t.Errorf("locator %s carries source text", f.Locator)
			}
			if f.Column != nil && strings.Contains(f.Column.String(), s) {
				t.Errorf("column %s carries source text", f.Column)
			}
		}
	}
}
