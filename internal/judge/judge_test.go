package judge

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
)

func loadPolicy(t *testing.T, body string) *policy.Policy {
	t.Helper()
	path := filepath.Join(t.TempDir(), policy.FileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

const testPolicy = `version = 1
protected_roots = ["pbip"]
[scan]
report = ["pbip/**/*.json"]
[columns]
block = ["full_name", "student_id"]
warn  = ["cohort_year"]
[values]
allow = ["(none selected)"]
`

func TestLiteral(t *testing.T) {
	p := loadPolicy(t, testPolicy)
	fullName := finding.Column{Table: "student", Name: "full_name"}
	studentID := finding.Column{Table: "student", Name: "Student_ID"}
	cohort := finding.Column{Table: "enrollment", Name: "cohort_year"}
	term := finding.Column{Table: "enrollment", Name: "term_start_date"}
	tests := []struct {
		name    string
		value   string
		kind    Kind
		columns []finding.Column
		fires   bool
		tier    finding.Tier
		reasons []finding.Reason
		column  string
	}{
		{"empty", "", String, []finding.Column{fullName}, false, "", nil, ""},
		{"allowed sentinel on a block column", "(none selected)", String, []finding.Column{fullName}, false, "", nil, ""},
		{"block column and shape", "Doe, Jane", String, []finding.Column{fullName}, true, finding.Block, []finding.Reason{finding.BlockColumn, finding.PersonNameShape}, "student.full_name"},
		{"block column alone, matched case-insensitively", "S-1001", String, []finding.Column{studentID}, true, finding.Block, []finding.Reason{finding.BlockColumn}, "student.Student_ID"},
		{"first block column wins", "S-1001", String, []finding.Column{cohort, studentID, fullName}, true, finding.Block, []finding.Reason{finding.BlockColumn}, "student.Student_ID"},
		{"shape on no column", "jane.doe@example.com", String, nil, true, finding.Block, []finding.Reason{finding.EmailShape}, ""},
		{"shape on a warn column drops the warn reason", "Roe, Richard", String, []finding.Column{cohort}, true, finding.Block, []finding.Reason{finding.PersonNameShape}, ""},
		{"warn column", "2105", String, []finding.Column{cohort}, true, finding.Warn, []finding.Reason{finding.WarnColumn}, "enrollment.cohort_year"},
		{"number on no column", "555", Number, nil, false, "", nil, ""},
		{"number on a block column", "1001", Number, []finding.Column{studentID}, true, finding.Block, []finding.Reason{finding.BlockColumn}, "student.Student_ID"},
		{"typed date on a block column", "2105-03-14T00:00:00", TypedDate, []finding.Column{{Table: "student", Name: "full_name"}}, true, finding.Block, []finding.Reason{finding.BlockColumn}, "student.full_name"},
		{"typed date on an ordinary column", "2105-08-24T00:00:00", TypedDate, []finding.Column{term}, false, "", nil, ""},
		{"benign string", "Algebra I", String, []finding.Column{term}, false, "", nil, ""},
	}
	for _, tt := range tests {
		v, fires := Literal(tt.value, tt.kind, tt.columns, p)
		if fires != tt.fires {
			t.Errorf("%s: fires = %v", tt.name, fires)
			continue
		}
		if !fires {
			continue
		}
		col := ""
		if v.Column != nil {
			col = v.Column.String()
		}
		if v.Tier != tt.tier || !slices.Equal(v.Reasons, tt.reasons) || col != tt.column {
			t.Errorf("%s: got %s %v %q", tt.name, v.Tier, v.Reasons, col)
		}
	}
}

func TestShapeSwitch(t *testing.T) {
	p := loadPolicy(t, testPolicy+"[shapes]\nemail = false\n")
	if _, fires := Literal("jane.doe@example.com", String, nil, p); fires {
		t.Error("a switched-off shape fired")
	}
}

func TestParseErrorCarriesNoText(t *testing.T) {
	e := &ParseError{Reason: finding.UnparseableTMDL, Line: 3, Msg: "unterminated string"}
	if e.Error() != "unparseable-tmdl: line 3: unterminated string" {
		t.Errorf("got %q", e.Error())
	}
}
