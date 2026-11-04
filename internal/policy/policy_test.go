package policy

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
)

// The value planted in these tests. It must never appear in an error.
const sentinel = "Doe, Jane"

const header = `version = 1
protected_roots = ["pbip"]

[scan]
report = ["pbip/**/*.json"]
`

const columns = `
[columns]
block = ["full_name"]
warn  = ["cohort_year", "section_number"]
`

const acceptEntry = `
[[accept]]
column      = "enrollment.cohort_year"
path        = "pbip/Example.Report/definition"
note        = "reviewed and intended"
reviewed_by = "a reviewer"
`

func mustParse(t *testing.T, doc string) *Policy {
	t.Helper()
	p, err := parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, doc, key string
		line           int
	}{
		{"invalid TOML", "version = 1\nprotected_roots = [\"pbip]\n", "", 2},
		{"duplicate key", header + "version = 1\n", "", 6},
		{"wrong type", `version = "1"` + "\n", "", 0},
		{"unknown top-level key", header + "\nstrict = true\n", "", 0},
		{"unknown key in a table", header + "\n[shapes]\ndob = false\n", "", 8},
		{"unknown table", header + "\n[heuristics]\n", "", 0},
		{"unknown key in accept", header + columns + acceptEntry + "expires = \"2199-12-31\"\n", "", 0},
		{"version missing", `protected_roots = ["pbip"]` + "\n[scan]\nreport = [\"pbip/**\"]\n", "version", 0},
		{"version 2", strings.Replace(header, "version = 1", "version = 2", 1), "version", 0},
		{"roots missing", "version = 1\n[scan]\nreport = [\"pbip/**\"]\n", "protected_roots", 0},
		{"roots empty", strings.Replace(header, `["pbip"]`, `[]`, 1), "protected_roots", 0},
		{"root absolute", strings.Replace(header, `["pbip"]`, `["/pbip"]`, 1), "protected_roots[0]", 0},
		{"root drive", strings.Replace(header, `["pbip"]`, `["C:/pbip"]`, 1), "protected_roots[0]", 0},
		{"root dotdot", strings.Replace(header, `["pbip"]`, `["pbip", "../pbip"]`, 1), "protected_roots[1]", 0},
		{"root dot", strings.Replace(header, `["pbip"]`, `["./pbip"]`, 1), "protected_roots[0]", 0},
		{"root backslash", strings.Replace(header, `["pbip"]`, `["pbip\\x"]`, 1), "protected_roots[0]", 0},
		{"root empty string", strings.Replace(header, `["pbip"]`, `[""]`, 1), "protected_roots[0]", 0},
		{"root trailing slash", strings.Replace(header, `["pbip"]`, `["pbip/"]`, 1), "protected_roots[0]", 0},
		{"root glob", strings.Replace(header, `["pbip"]`, `["pb*"]`, 1), "protected_roots[0]", 0},
		{"root not covered", strings.Replace(header, `["pbip"]`, `["pbip", "warehouse"]`, 1), "protected_roots[1]", 0},
		{"no scan kind", "version = 1\nprotected_roots = [\"pbip\"]\n[scan]\nexclude = [\"**\"]\n", "scan", 0},
		{"no scan table", "version = 1\nprotected_roots = [\"pbip\"]\n", "scan", 0},
		{"bad scan glob", strings.Replace(header, `["pbip/**/*.json"]`, `["pbip/**/*.json", "pbip/**/[.json"]`, 1), "scan.report[1]", 0},
		{"bad exclude glob", header + "exclude = [\"a**\"]\n", "scan.exclude[0]", 0},
		{"bad local_state glob", header + "\n[files]\nlocal_state = [\"/x\"]\n", "files.local_state[0]", 0},
		{"bad unscanned_allow glob", header + "\n[files]\nunscanned_allow = [\"\"]\n", "files.unscanned_allow[0]", 0},
		{"bad block_pattern", header + "\n[columns]\nblock_pattern = \"(^|_)email(\"\n", "columns.block_pattern", 0},
		{"empty block_pattern", header + "\n[columns]\nblock_pattern = \"\"\n", "columns.block_pattern", 0},
		{"accept missing reviewed_by", header + columns + strings.Replace(acceptEntry, "reviewed_by = \"a reviewer\"\n", "", 1), "accept[0].reviewed_by", 0},
		{"accept empty note", header + columns + strings.Replace(acceptEntry, `"reviewed and intended"`, `"  "`, 1), "accept[0].note", 0},
		{"accept missing column", header + columns + strings.Replace(acceptEntry, "column      = \"enrollment.cohort_year\"\n", "", 1), "accept[0].column", 0},
		{"accept missing path", header + columns + strings.Replace(acceptEntry, "path        = \"pbip/Example.Report/definition\"\n", "", 1), "accept[0].path", 0},
		{"accept column bare", header + columns + strings.Replace(acceptEntry, "enrollment.cohort_year", "cohort_year", 1), "accept[0].column", 0},
		{"accept column no table", header + columns + strings.Replace(acceptEntry, "enrollment.cohort_year", ".cohort_year", 1), "accept[0].column", 0},
		{"accept column block", header + columns + strings.Replace(acceptEntry, "enrollment.cohort_year", "student.full_name", 1), "accept[0].column", 0},
		{"accept path absolute", header + columns + strings.Replace(acceptEntry, `"pbip/Example`, `"/pbip/Example`, 1), "accept[0].path", 0},
		{"accept path dotdot", header + columns + strings.Replace(acceptEntry, `"pbip/Example.Report/definition"`, `"pbip/../Example.Report/definition"`, 1), "accept[0].path", 0},
		{"accept path dot", header + columns + strings.Replace(acceptEntry, `"pbip/Example.Report/definition"`, `"pbip/./Example.Report/definition"`, 1), "accept[0].path", 0},
		{"accept path backslash", header + columns + strings.Replace(acceptEntry, `"pbip/Example.Report/definition"`, `'pbip\Example.Report\definition'`, 1), "accept[0].path", 0},
		{"accept path shallow", header + columns + strings.Replace(acceptEntry, `"pbip/Example.Report/definition"`, `"pbip/Example.Report"`, 1), "accept[0].path", 0},
		{"accept second entry", header + columns + acceptEntry + strings.Replace(acceptEntry, `"pbip/Example.Report/definition"`, `"pbip"`, 1), "accept[1].path", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse([]byte(tt.doc))
			if err == nil {
				t.Fatal("parsed")
			}
			if tt.key != "" && err.Key != tt.key {
				t.Errorf("key %q, want %q", err.Key, tt.key)
			}
			if tt.line != 0 && err.Line != tt.line {
				t.Errorf("line %d, want %d", err.Line, tt.line)
			}
			if err.Msg == "" {
				t.Error("no message")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	_, err := Load(path)
	var perr *Error
	if !errors.As(err, &perr) {
		t.Fatalf("got %v, want a policy error", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("a missing file does not unwrap to fs.ErrNotExist")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestLoadDefaultPolicy(t *testing.T) {
	p, err := Load("../../testdata/corpus/policies/default.toml")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.ProtectedRoots(); len(got) != 1 || got[0] != "pbip" {
		t.Errorf("roots %v", got)
	}
}

// TestUnknownKeyIsNotEchoed: a key the policy does not know is text from
// the file, so the error gives the line and that a key is not known, not
// the key. A file that merely parses as TOML must not be a way to read it.
func TestUnknownKeyIsNotEchoed(t *testing.T) {
	const name = "private_marker_key"
	for label, doc := range map[string]string{
		"top level":  header + "\n" + name + " = true\n",
		"in a table": header + "\n[shapes]\n" + name + " = false\n",
		"a table":    header + "\n[" + name + "]\n",
	} {
		_, err := parse([]byte(doc))
		if err == nil {
			t.Fatalf("%s: parsed", label)
		}
		if strings.Contains(err.Error(), name) || strings.Contains(err.Key, name) {
			t.Errorf("%s: the error echoes the key: %v", label, err)
		}
		if err.Line == 0 {
			t.Errorf("%s: no line", label)
		}
	}
}

func TestErrorsNeverCarryAValue(t *testing.T) {
	allow := "\n[values]\nallow = [\"" + sentinel + "\"]\n"
	note := strings.Replace(acceptEntry, "reviewed and intended", sentinel, 1)
	docs := map[string]string{
		"bad accept path after allow": header + columns + allow + strings.Replace(note, `"pbip/Example.Report/definition"`, `"pbip"`, 1),
		"unknown key after a note":    header + columns + note + "expires = \"" + sentinel + "\"\n",
		"a value written as a key":    header + columns + "\n[values]\n\"" + sentinel + "\" = true\n",
		"value as a table name":       header + columns + "\n[\"" + sentinel + "\"]\n",
		"unterminated allow string":   header + columns + "\n[values]\nallow = [\"" + sentinel + "]\n",
		"allow of the wrong type":     header + columns + "\n[values]\nallow = \"" + sentinel + "\"\n",
		"note of the wrong type":      header + columns + "\n[[accept]]\nnote = [\"" + sentinel + "\"]\n",
		"reviewer as a bad root":      strings.Replace(header, `["pbip"]`, `["/`+sentinel+`"]`, 1),
		"bad glob holding a value":    strings.Replace(header, `["pbip/**/*.json"]`, `["pbip/[`+sentinel+`"]`, 1),
		"bad pattern holding a value": header + "\n[columns]\nblock_pattern = \"(" + sentinel + "\"\n",
		"not a warn column":           header + columns + strings.Replace(note, "enrollment.cohort_year", "t."+sentinel, 1),
	}
	dir := t.TempDir()
	for name, doc := range docs {
		path := filepath.Join(dir, FileName)
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path)
		if err == nil {
			t.Errorf("%s: loaded", name)
			continue
		}
		t.Logf("%s: %v", name, err)
		for _, part := range []string{"Doe", "Jane"} {
			if strings.Contains(err.Error(), part) {
				t.Errorf("%s: error %q carries the value", name, err)
			}
		}
	}
}

func TestClassify(t *testing.T) {
	p := mustParse(t, `version = 1
protected_roots = ["pbip", "warehouse/models"]

[scan]
report  = ["pbip/**/*.json"]
model   = ["pbip/**/*.tmdl"]
sql     = ["sql/**/*.sql", "warehouse/**/*.sql"]
yaml    = ["warehouse/**/*.yml"]
prose   = ["notes/**/*.md", "pbip/**/*.json"]
exclude = ["**/.pbi/**", "sql/generated/**"]

[files]
local_state     = ["**/.pbi/localSettings.json", "**/*.json.bak"]
unscanned_allow = ["pbip/**/StaticResources/**/*.png", "pbip/**/*.json"]
`)
	tests := []struct {
		path  string
		class Class
		kind  Kind
	}{
		{"pbip/A.Report/definition/report.json", Scanned, Report},
		{"pbip/A.SemanticModel/definition/model.tmdl", Scanned, Model},
		{"pbip/A.Report/.pbi/localSettings.json", LocalState, ""},
		{"pbip/A.Report/.pbi/cache.abf", Unvettable, ""},
		{"pbip/A.Report/.pbi/unappliedChanges.json", UnscannedAllowed, ""}, // excluded from scan, then allowed
		{"pbip/A.Report/StaticResources/RegisteredResources/logo.png", UnscannedAllowed, ""},
		{"pbip/A.Report/StaticResources/RegisteredResources/logo.svg", Unvettable, ""},
		{"pbip/A.SemanticModel/model.bim", Unvettable, ""},
		{"pbip/A.pbix", Unvettable, ""},
		{"pbip", Unvettable, ""},
		{"sql/staging/enrollment.sql", Scanned, SQL},
		{"sql/generated/enrollment.sql", Outside, ""},
		{"sql/report.json.bak", Outside, ""},
		{"warehouse/models/student.yml", Scanned, YAML},
		{"warehouse/models/student.sql", Scanned, SQL},
		{"warehouse/models/readme.txt", Unvettable, ""},
		{"warehouse/seeds/readme.txt", Outside, ""},
		{"notes/2150-plan.md", Scanned, Prose},
		{"docs/roster.txt", Outside, ""},
		{"pbipx/x.txt", Outside, ""},
		{"PBIP/A.Report/report.json", Outside, ""},
	}
	for _, tt := range tests {
		class, kind := p.Classify(tt.path)
		if class != tt.class || kind != tt.kind {
			t.Errorf("Classify(%q) = %v %q, want %v %q", tt.path, class, kind, tt.class, tt.kind)
		}
	}
}

func TestClassifyAbsentListsMatchNothing(t *testing.T) {
	p := mustParse(t, header)
	for path, want := range map[string]Class{
		"pbip/A.Report/StaticResources/RegisteredResources/logo.png": Unvettable,
		"pbip/A.Report/.pbi/localSettings.json":                      Scanned,
		"pbip/A.Report/.pbi/cache.abf":                               Unvettable,
	} {
		if got, _ := p.Classify(path); got != want {
			t.Errorf("Classify(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestUnderRoot(t *testing.T) {
	p := mustParse(t, header)
	for path, want := range map[string]bool{
		"pbip": true, "pbip/a": true, "pbipx/a": false, "x/pbip/a": false, "": false,
	} {
		if got := p.UnderRoot(path); got != want {
			t.Errorf("UnderRoot(%q) = %v", path, got)
		}
	}
}

func TestLocalStateDir(t *testing.T) {
	p := mustParse(t, header+`
[files]
local_state = ["**/.pbi/localSettings.json", "pbip/cache/**", "**/*.bak"]
`)
	for dir, want := range map[string]bool{
		"pbip/A.Report/.pbi":       true,
		"pbip/.pbi":                true,
		"pbip/cache":               true,
		"pbip/cache/deep/er":       true,
		"pbip/A.Report":            true, // a *.bak file directly in it is local state
		"pbip/A.Report/.pbi/x/y":   true, // as above
		"notes/.pbi":               false,
		"pbipx/.pbi":               false,
		"pbip/A.Report/.pbi/extra": true,
	} {
		if got := p.LocalStateDir(dir); got != want {
			t.Errorf("LocalStateDir(%q) = %v, want %v", dir, got, want)
		}
	}
	q := mustParse(t, header+`
[files]
local_state = ["**/.pbi/localSettings.json"]
`)
	for dir, want := range map[string]bool{
		"pbip/A.Report/.pbi":   true,
		"pbip/A.Report":        false,
		"pbip":                 false,
		"pbip/A.Report/.pbi/x": false,
		".pbi":                 false,
	} {
		if got := q.LocalStateDir(dir); got != want {
			t.Errorf("LocalStateDir(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestColumnVerdicts(t *testing.T) {
	p := mustParse(t, header+`
[columns]
block         = ["full_name", "Student_ID"]
block_pattern = "(^|_)EMAIL(_|$)"
warn          = ["Cohort_Year"]
`)
	tests := []struct {
		name         string
		block, warns bool
	}{
		{"full_name", true, false},
		{"Full_Name", true, false},
		{"FULL_NAME", true, false},
		{"student_id", true, false},
		{"full_name_2", false, false},
		{"guardian_email", true, false},
		{"Guardian_Email_Address", true, false},
		{"email", true, false},
		{"emailed", false, false},
		{"cohort_year", false, true},
		{"COHORT_YEAR", false, true},
		{"term", false, false},
	}
	for _, tt := range tests {
		if got := p.BlocksColumn(tt.name); got != tt.block {
			t.Errorf("BlocksColumn(%q) = %v", tt.name, got)
		}
		if got := p.WarnsColumn(tt.name); got != tt.warns {
			t.Errorf("WarnsColumn(%q) = %v", tt.name, got)
		}
	}
	empty := mustParse(t, header)
	if empty.BlocksColumn("email") || empty.WarnsColumn("cohort_year") {
		t.Error("absent column lists matched")
	}
}

func TestAllowsValue(t *testing.T) {
	p := mustParse(t, header+"\n[values]\nallow = [\"(none selected)\", \"\"]\n")
	for literal, want := range map[string]bool{
		"(none selected)":  true,
		"(None Selected)":  false,
		"(none selected) ": false,
		"":                 true,
		"none":             false,
	} {
		if got := p.AllowsValue(literal); got != want {
			t.Errorf("AllowsValue(%q) = %v", literal, got)
		}
	}
	if mustParse(t, header).AllowsValue("(none selected)") {
		t.Error("an absent allow list allowed a value")
	}
}

func TestShapes(t *testing.T) {
	all := Shapes{PersonName: true, Email: true, SSN: true, Date: true, Phone: true}
	if got := mustParse(t, header).Shapes(); got != all {
		t.Errorf("defaults %+v", got)
	}
	got := mustParse(t, header+"\n[shapes]\nemail = false\ndate = false\nphone = true\n").Shapes()
	want := Shapes{PersonName: true, Email: false, SSN: true, Date: false, Phone: true}
	if got != want {
		t.Errorf("switched %+v, want %+v", got, want)
	}
}

func TestAccepts(t *testing.T) {
	p := mustParse(t, header+columns+acceptEntry)
	warn := func(path, table, column string, reasons ...finding.Reason) finding.Finding {
		f := finding.Finding{Path: path, Context: finding.SlicerSelection, Tier: finding.Warn, Reasons: reasons}
		if column != "" {
			f.Column = &finding.Column{Table: table, Name: column}
		}
		return f
	}
	const under = "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json"
	block := warn(under, "enrollment", "cohort_year", finding.WarnColumn)
	block.Tier = finding.Block
	tests := []struct {
		name string
		f    finding.Finding
		want bool
	}{
		{"warn column under path", warn(under, "enrollment", "cohort_year", finding.WarnColumn), true},
		{"case differs", warn(under, "Enrollment", "Cohort_Year", finding.WarnColumn), true},
		{"at the path itself", warn("pbip/Example.Report/definition", "enrollment", "cohort_year", finding.WarnColumn), true},
		{"other column", warn(under, "enrollment", "section_number", finding.WarnColumn), false},
		{"other table", warn(under, "applicant", "cohort_year", finding.WarnColumn), false},
		{"no table", warn(under, "", "cohort_year", finding.WarnColumn), false},
		{"no column", warn(under, "", "", finding.WarnColumn), false},
		{"other path", warn("pbip/Other.Report/definition/report.json", "enrollment", "cohort_year", finding.WarnColumn), false},
		{"sibling with the prefix's name", warn("pbip/Example.Report/definitions/report.json", "enrollment", "cohort_year", finding.WarnColumn), false},
		{"another reason too", warn(under, "enrollment", "cohort_year", finding.WarnColumn, finding.DateShape), false},
		{"no reason", warn(under, "enrollment", "cohort_year"), false},
		{"a block", block, false},
	}
	for _, tt := range tests {
		if got := p.Accepts(tt.f); got != tt.want {
			t.Errorf("%s: Accepts = %v, want %v", tt.name, got, tt.want)
		}
	}
	if mustParse(t, header+columns).Accepts(tests[0].f) {
		t.Error("a policy with no accept entries accepted")
	}
}

func TestAcceptColumnWithDots(t *testing.T) {
	p := mustParse(t, header+columns+strings.Replace(acceptEntry, "enrollment.cohort_year", "fact.enrollment.cohort_year", 1))
	f := finding.Finding{
		Path:    "pbip/Example.Report/definition/report.json",
		Tier:    finding.Warn,
		Reasons: []finding.Reason{finding.WarnColumn},
		Column:  &finding.Column{Table: "fact.enrollment", Name: "cohort_year"},
	}
	if !p.Accepts(f) {
		t.Error("a dotted table name was not accepted")
	}
}

// TestCorpusPolicyCases loads every policy case's .pbipx.toml: a case that
// expects policy-error must fail to load (or have no file), and every other
// case must load.
func TestCorpusPolicyCases(t *testing.T) {
	cases, err := filepath.Glob("../../testdata/corpus/policy/*/expected.json")
	if err != nil || len(cases) == 0 {
		t.Fatalf("no policy cases: %v", err)
	}
	for _, expected := range cases {
		dir := filepath.Dir(expected)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			data, err := os.ReadFile(expected) //nolint:gosec // G304: a corpus path
			if err != nil {
				t.Fatal(err)
			}
			var want struct {
				Failure string `json:"failure"`
			}
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			_, err = Load(filepath.Join(dir, "tree", FileName))
			switch {
			case want.Failure == string(finding.PolicyError) && err == nil:
				t.Error("loaded, want a policy error")
			case want.Failure != string(finding.PolicyError) && err != nil:
				t.Errorf("want a load, got %v", err)
			}
			var perr *Error
			if err != nil && !errors.As(err, &perr) {
				t.Errorf("error %v is not a *policy.Error", err)
			}
		})
	}
}
