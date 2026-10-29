package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
)

const explainPolicy = `version = 1
protected_roots = ["pbip"]

[scan]
report = ["pbip/**/*.json"]
model  = ["pbip/**/*.tmdl"]
sql    = ["sql/**/*.sql"]
yaml   = ["models/**/*.yml"]
`

// explainPlanted are the values the explain repository plants. None may
// appear in any output.
var explainPlanted = []string{
	sentinel, "Doe", "jane.doe@example.com", "Advising Staff", "contoso.example.cloud",
	"2105-03-14", "Roe, Richard", "212-555-0142",
}

// newExplainRepo makes a committed repository with a report, a model in
// three files, SQL, YAML and a model file that does not parse, each holding
// planted values. It changes into the repository.
func newExplainRepo(t *testing.T) {
	t.Helper()
	isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "repo")
	files := map[string]string{
		policy.FileName: explainPolicy,
		"pbip/A.Report/definition/visual.json": `{"visual":{"objects":{"general":[{"properties":{"filter":{"filter":{` +
			`"From":[{"Name":"s","Entity":"student"}],"Where":[{"Condition":{"In":{"Expressions":[{"Column":{` +
			`"Expression":{"SourceRef":{"Source":"s"}},"Property":"full_name"}}],"Values":[[{"Literal":{"Value":"'Doe, Jane'"}}]]}}}]}}}}]}}}`,
		"pbip/M.SemanticModel/definition/expressions.tmdl": "expression Database = \"warehouse\" meta [IsParameterQuery = true]\n",
		"pbip/M.SemanticModel/definition/tables/student.tmdl": "table student\n\n" +
			"\tmeasure 'Roe, Richard' = COUNTROWS(student)\n\n" +
			"\tcolumn 'jane.doe@example.com'\n\t\tdataType: string\n\n" +
			"\tpartition student = m\n\t\tsource =\n\t\t\t\tlet\n" +
			"\t\t\t\t\tSource = Sql.Database(\"contoso.example.cloud\", Database),\n" +
			"\t\t\t\t\tt = Source{[Schema = \"dbo\", Item = \"student\"]}[Data],\n" +
			"\t\t\t\t\tf = Table.SelectRows(t, each [full_name] = \"Doe, Jane\"),\n" +
			"\t\t\t\t\tq = Value.NativeQuery(Source, \"select * from dbo.enrollment where d = '2105-03-14'\")\n" +
			"\t\t\t\tin\n\t\t\t\t\tf\n",
		"pbip/M.SemanticModel/definition/roles/advisors.tmdl": "role advisors\n" +
			"\ttablePermission student = [advisor] = USERPRINCIPALNAME()\n" +
			"\tmember 'jane.doe@example.com'\n\tmember 'Advising Staff'\n",
		"pbip/M.SemanticModel/definition/tables/broken.tmdl": "table broken\n  column 'Doe, Jane'\n",
		"sql/roster.sql": "create view analytics.roster as select * from warehouse.dbo.student " +
			"where full_name = 'Doe, Jane' and phone = '212-555-0142'\n",
		"models/students.yml": "models:\n  - name: student\n    description: \"Doe, Jane\"\n",
	}
	for name, content := range files {
		writeFile(t, root, name, content)
	}
	if err := corpus.GitSetup(root, nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
}

func explainLeak(o outcome) string {
	all := o.stdout + o.stderr
	for _, v := range explainPlanted {
		if strings.Contains(all, v) {
			return v
		}
	}
	return ""
}

func TestExplainNeverPrintsAValue(t *testing.T) {
	newExplainRepo(t)
	for _, format := range []string{formatJSON, formatHuman} {
		o := runIn(t, "", "explain", "--format", format, "pbip", "sql", "models")
		if o.code != finding.ExitOperational {
			t.Errorf("%s: exit = %d, want %d for the file that does not parse", format, o.code, finding.ExitOperational)
		}
		if v := explainLeak(o); v != "" {
			t.Errorf("%s: output carries a planted value", format)
		}
		for _, want := range []string{"<withheld>", "warehouse.dbo.student", "dbo.enrollment", "analytics.roster", "unparseable-tmdl", "not-explained"} {
			if !strings.Contains(o.stdout, want) {
				t.Errorf("%s: output lacks %s", format, want)
			}
		}
	}
}

func TestExplainJSON(t *testing.T) {
	newExplainRepo(t)
	o := runIn(t, "", "explain", "--format", "json", "pbip/M.SemanticModel")
	var got struct {
		Files []map[string]json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal([]byte(o.stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if len(got.Files) != 4 {
		t.Fatalf("files = %d, want 4", len(got.Files))
	}
	broken := got.Files[2] // expressions, roles, broken, student
	if len(broken) != 3 || string(broken["error"]) != `"unparseable-tmdl"` {
		t.Errorf("the broken file is not path, kind and error alone")
	}
	var roles []struct {
		Members int      `json:"members"`
		Filters []string `json:"filters"`
	}
	if err := json.Unmarshal(got.Files[1]["roles"], &roles); err != nil || len(roles) != 1 || roles[0].Members != 2 {
		t.Errorf("roles = %s, want one role with 2 members", got.Files[1]["roles"])
	}
}

func TestExplainUsageErrors(t *testing.T) {
	newExplainRepo(t)
	for _, args := range [][]string{
		{"explain"},
		{"explain", "--format", "yaml", "sql"},
		{"explain", "sql", sentinel},
		{"explain", "--show-values", "sql"},
	} {
		o := runIn(t, "", args...)
		if o.code != finding.ExitUsage {
			t.Errorf("%v: exit = %d, want %d", len(args), o.code, finding.ExitUsage)
		}
		if explainLeak(o) != "" {
			t.Error("a usage error echoes an argument")
		}
	}
}
