package gate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/craigalodon/pbipx/internal/gate"
)

// The deny for a Performance Analyzer export (behavior.md, "Files and the
// default deny"; principle 8). The field names in these fixtures are from
// memory of the export's general shape and need confirming against a real
// export (perfexport.go). The query text is a synthetic value: it must never
// reach a message.

// perfExport is an export of one visual's events. The name in its query text
// is inside a longer string, which no scanner judges.
const perfExport = `{
  "version": "1.2.0",
  "events": [
    {
      "name": "Execute DAX Query",
      "id": "00000000-0000-0000-0000-000000000001",
      "parentId": "00000000-0000-0000-0000-000000000000",
      "start": "2100-01-01T00:00:00.000Z",
      "end": "2100-01-01T00:00:00.250Z",
      "metrics": {
        "QueryText": "EVALUATE FILTER(student, student[full_name] = \"Doe, Jane\")"
      }
    }
  ]
}`

const (
	exportUnderRoot = "pbip/A.Report/definition/trace.json"
	exportOutside   = "analysis/trace.json"
)

func readEv(p string) gate.Event { return gate.Event{Tool: gate.Read, Path: p} }

func utf16LE(s string) string {
	b := []byte{0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u&0xff), byte(u>>8))
	}
	return string(b)
}

func TestReadPerfExport(t *testing.T) {
	files := cleanFiles()
	files[exportUnderRoot] = perfExport
	files[exportOutside] = perfExport
	files["analysis/trace.txt"] = perfExport // shape, not name
	files["analysis/TRACE.JSON"] = perfExport
	files["analysis/bom.json"] = "\xef\xbb\xbf" + perfExport
	files["analysis/crlf.json"] = strings.ReplaceAll(perfExport, "\n", "\r\n")
	files["analysis/parent.json"] = `{"version":2,"events":[{"name":"Query","parentId":"x"}]}`
	files["analysis/metrics.json"] = `{"version":"1","events":[{"name":"Query","metrics":{}}]}`
	files["analysis/one-line.json"] = `{"version":"1","events":[{"name":"Query","metrics":{}}]}` + "\n"
	files["analysis/utf16.json"] = utf16LE(perfExport)
	root := newRepo(t, files)
	run(t, root, []decideCase{
		{"under a root, in scan scope", readEv(exportUnderRoot), gate.ReasonPerfExport},
		{"outside the roots", readEv(exportOutside), gate.ReasonPerfExport},
		{"renamed", readEv("analysis/trace.txt"), gate.ReasonPerfExport},
		{"upper-case name", readEv("analysis/TRACE.JSON"), gate.ReasonPerfExport},
		{"with a byte order mark", readEv("analysis/bom.json"), gate.ReasonPerfExport},
		{"with CRLF", readEv("analysis/crlf.json"), gate.ReasonPerfExport},
		{"parentId and a numeric version", readEv("analysis/parent.json"), gate.ReasonPerfExport},
		{"metrics only", readEv("analysis/metrics.json"), gate.ReasonPerfExport},
		{"one line and a newline", readEv("analysis/one-line.json"), gate.ReasonPerfExport},
		{"UTF-16", readEv("analysis/utf16.json"), gate.ReasonPerfExport},
		{"absolute", readEv(filepath.Join(root, filepath.FromSlash(exportOutside))), gate.ReasonPerfExport},
		{"edit shows the file", gate.Event{Tool: gate.Edit, Path: exportOutside}, gate.ReasonPerfExport},
		{"relative from a subdirectory", gate.Event{Tool: gate.Read, Path: "trace.json", Cwd: "analysis"}, gate.ReasonPerfExport},
	})
}

// Principle 6: ordinary JSON is not an export, and reading it stays allowed.
func TestReadOrdinaryJSONStaysAllowed(t *testing.T) {
	big := `{"version":"1","pad":"` + strings.Repeat("a", 100<<10) + `","events":[{"name":"Query","metrics":{}}]}`
	files := cleanFiles()
	files["package.json"] = `{"name":"example","version":"1.0.0","scripts":{"test":"echo ok"},"dependencies":{"left":"^1.0.0"}}`
	files["tsconfig.json"] = `{"compilerOptions":{"target":"es2022","strict":true},"include":["src"]}`
	files["analysis/calendar.json"] = `{"version":"1","events":[{"name":"Standup","id":"a1","start":"2100-01-01T09:00:00Z","end":"2100-01-01T09:15:00Z"}]}`
	files["analysis/name-start.json"] = `{"events":[{"name":"Standup","start":"2100-01-01"}]}`
	files["analysis/name-start-version.json"] = `{"version":"1","events":[{"name":"Standup","start":"2100-01-01"}]}`
	files["analysis/name-id.json"] = `{"version":1,"events":[{"name":"boot","id":3,"level":"info"}]}`
	files["analysis/ndjson.json"] = `{"version":"1","events":[{"name":"Query","metrics":{}}]}` + "\n" + `{"version":"1","events":[{"name":"Query","metrics":{}}]}` + "\n"
	files["analysis/events-before-version.json"] = `{"events":[{"name":"Query","metrics":{}}],"version":"1"}`
	files["analysis/no-version.json"] = `{"events":[{"name":"Query","metrics":{}}]}`
	files["analysis/empty-events.json"] = `{"version":"1.2.0","events":[]}`
	files["analysis/events-of-strings.json"] = `{"version":"1","events":["a","b"]}`
	files["analysis/events-object.json"] = `{"version":"1","events":{"name":"Query","start":1}}`
	files["analysis/events-nested.json"] = `{"data":{"events":[{"name":"Query","start":1}]}}`
	files["analysis/name-only.json"] = `{"version":"1","events":[{"name":"Launch"}]}`
	files["analysis/array.json"] = `[{"name":"Query","start":1}]`
	files["analysis/empty-events-no-version.json"] = `{"events":[]}`
	files["analysis/truncated.json"] = `{"version":"1","events":[{"name":"Que`
	files["analysis/not-json.json"] = `events: [name: Query]`
	files["analysis/empty-file.json"] = ``
	files["analysis/events-past-the-head.json"] = big
	files["analysis/report-like.json"] = `{"name":"0001","visual":{"visualType":"card"}}`
	root := newRepo(t, files)
	run(t, root, []decideCase{
		{"package.json", readEv("package.json"), ""},
		{"tsconfig.json", readEv("tsconfig.json"), ""},
		{"a calendar with named, timed events", readEv("analysis/calendar.json"), ""},
		{"name and start", readEv("analysis/name-start.json"), ""},
		{"name and start, with a version", readEv("analysis/name-start-version.json"), ""},
		{"name and id, with a version", readEv("analysis/name-id.json"), ""},
		{"lines of values", readEv("analysis/ndjson.json"), ""},
		{"events before the version", readEv("analysis/events-before-version.json"), ""},
		{"no version", readEv("analysis/no-version.json"), ""},
		{"no events, with a version", readEv("analysis/empty-events.json"), ""},
		{"events of strings", readEv("analysis/events-of-strings.json"), ""},
		{"events an object", readEv("analysis/events-object.json"), ""},
		{"events nested", readEv("analysis/events-nested.json"), ""},
		{"an event with a name only", readEv("analysis/name-only.json"), ""},
		{"a top-level array", readEv("analysis/array.json"), ""},
		{"empty events and no version", readEv("analysis/empty-events-no-version.json"), ""},
		{"truncated", readEv("analysis/truncated.json"), ""},
		{"not JSON", readEv("analysis/not-json.json"), ""},
		{"empty file", readEv("analysis/empty-file.json"), ""},
		{"undecided in the head falls back", readEv("analysis/events-past-the-head.json"), ""},
		{"a report-like file outside the roots", readEv("analysis/report-like.json"), ""},
		{"the existing clean report", readEv(reportJSON), ""},
		{"the existing leak still denies as a finding", readEv(leakNote), gate.ReasonFinding},
	})
}

// An export outside the repository is not the gate's to judge.
func TestReadPerfExportOutsideTheRepositoryAllows(t *testing.T) {
	root := newRepo(t, cleanFiles())
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "trace.json"), []byte(perfExport), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, root, []decideCase{{"outside the repository", readEv(filepath.Join(outside, "trace.json")), ""}})
}

// A message names the file and the class, never the query text.
func TestPerfExportMessageHoldsNoQueryText(t *testing.T) {
	root := newRepo(t, map[string]string{exportOutside: perfExport})
	d := gate.Decide(gate.Event{Tool: gate.Read, Path: exportOutside, Cwd: root}, gate.Options{})
	if d.Allow || d.Reason != gate.ReasonPerfExport {
		t.Fatalf("decision = %+v", d)
	}
	for _, s := range []string{"EVALUATE", "full_name", "QueryText", "Doe"} {
		if strings.Contains(d.Message, s) {
			t.Errorf("message holds %q", s)
		}
	}
	if !strings.Contains(d.Message, exportOutside) {
		t.Errorf("message does not name the file: %s", d.Message)
	}
}

// Principle 6, and the budget: a command naming many files cannot cost
// unbounded time, and fails closed past the budget with scan-error.
func TestPerfExportBudget(t *testing.T) {
	files := cleanFiles()
	for i := 0; i < 600; i++ {
		files[fmt.Sprintf("many/f%03d.json", i)] = `{"a":1}`
	}
	for i := 0; i < 100; i++ {
		files[fmt.Sprintf("few/f%03d.json", i)] = `{"a":1}`
	}
	root := newRepo(t, files)
	run(t, root, []decideCase{
		bash("100 files", "cat few/*.json", ""),
		bash("600 files", "cat many/*.json", gate.ReasonScanError),
		bash("600 files by name", "cat many/f0*.json many/f1*.json many/f2*.json many/f3*.json many/f4*.json many/f5*.json", gate.ReasonScanError),
	})
}
