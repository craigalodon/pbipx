package baseline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/secret"
)

// The value planted in these tests. It must never be written or quoted.
const sentinel = "Doe, Jane"

var day = time.Date(2100, 6, 15, 12, 0, 0, 0, time.UTC)

func warning() finding.Finding {
	return finding.Finding{
		Path:    "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json",
		Context: finding.SlicerSelection,
		Locator: "visual.objects.general[0]",
		Column:  &finding.Column{Table: "enrollment", Name: "cohort_year"},
		Tier:    finding.Warn,
		Reasons: []finding.Reason{finding.WarnColumn},
		Value:   secret.New(sentinel),
	}
}

func blockFinding() finding.Finding {
	f := warning()
	f.Tier = finding.Block
	f.Column = &finding.Column{Table: "student", Name: "full_name"}
	f.Reasons = []finding.Reason{finding.BlockColumn, finding.PersonNameShape}
	return f
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingIsEmpty(t *testing.T) {
	b, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil || len(b.Entries) != 0 {
		t.Errorf("a missing baseline: %v, %d entries", err, len(b.Entries))
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct{ name, doc, key string }{
		{"invalid TOML", "version = 1\n[entries.\"" + sentinel + "\"\n", ""},
		{"no version", "[entries]\n", "version"},
		{"wrong version", "version = 2\n", "version"},
		{"unknown key", "version = 1\nowner = \"" + sentinel + "\"\n", "owner"},
		{"non-warn severity", "version = 1\n[entries.\"fp1:00\"]\nseverity = \"block\"\nexpires = \"2199-12-31\"\n", "severity"},
		{"non-warn severity under a free-text key", "version = 1\n[entries.\"" + sentinel + "\"]\nseverity = \"block\"\n", "severity"},
		{"wrong type", "version = 1\n[entries.\"fp1:00\"]\nseverity = \"warn\"\nreasons = \"" + sentinel + "\"\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeFile(t, tt.doc))
			var be *Error
			if !errors.As(err, &be) {
				t.Fatalf("got %v, want an *Error", err)
			}
			if tt.key != "" && be.Key != tt.key {
				t.Errorf("key = %q, want %q", be.Key, tt.key)
			}
			if strings.Contains(err.Error(), "Doe") || strings.Contains(err.Error(), "Jane") {
				t.Errorf("the error quotes the file")
			}
		})
	}
}

func TestAccepts(t *testing.T) {
	w := warning()
	fp := w.Fingerprint()
	entry := func(expires string) *Baseline {
		return &Baseline{Entries: map[string]Entry{fp: {Severity: "warn", Expires: expires}}}
	}
	tests := []struct {
		name    string
		b       *Baseline
		f       finding.Finding
		accepts bool
	}{
		{"listed and current", entry("2100-06-15"), w, true},
		{"expired yesterday", entry("2100-06-14"), w, false},
		{"unparseable expiry", entry("someday"), w, false},
		{"not listed", &Baseline{}, w, false},
		{"nil baseline", nil, w, false},
		{"a block is never accepted", &Baseline{Entries: map[string]Entry{blockFinding().Fingerprint(): {Severity: "warn", Expires: "2199-12-31"}}}, blockFinding(), false},
	}
	for _, tt := range tests {
		if got := tt.b.Accepts(tt.f, day); got != tt.accepts {
			t.Errorf("%s: Accepts = %v, want %v", tt.name, got, tt.accepts)
		}
	}
}

func TestExpired(t *testing.T) {
	b := &Baseline{Entries: map[string]Entry{
		"fp1:01": {Severity: "warn", Expires: "2100-06-15"},
		"fp1:02": {Severity: "warn", Expires: "2100-06-14"},
		"fp1:03": {Severity: "warn", Expires: "not a date"},
	}}
	got := b.Expired(day)
	if strings.Join(got, ",") != "fp1:02,fp1:03" {
		t.Errorf("expired = %v", got)
	}
}

func TestRecordRefusals(t *testing.T) {
	findings := []finding.Finding{warning(), blockFinding()}
	tests := []struct {
		name string
		fp   string
		note string
		days int
		want Refusal
	}{
		{"block", blockFinding().Fingerprint(), "reviewed", 180, NotAWarning},
		{"unknown", "fp1:000000000000000000000000", "reviewed", 180, UnknownFinding},
		{"no note", warning().Fingerprint(), " ", 180, NoNote},
		{"no days", warning().Fingerprint(), "reviewed", 0, BadExpiry},
	}
	for _, tt := range tests {
		b := &Baseline{}
		_, err := b.Record(tt.fp, findings, tt.note, tt.days, day)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, err, tt.want)
		}
		if len(b.Entries) != 0 {
			t.Errorf("%s: an entry was recorded", tt.name)
		}
	}
}

func TestRecordSaveLoad(t *testing.T) {
	w := warning()
	b, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := b.Record(w.Fingerprint(), []finding.Finding{w}, "a reviewed persisted selection", DefaultExpiryDays, day)
	if err != nil {
		t.Fatal(err)
	}
	if e.Expires != "2100-12-12" {
		t.Errorf("expires = %s, want 180 days on", e.Expires)
	}
	path := filepath.Join(t.TempDir(), FileName)
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"Doe", "Jane"} {
		if strings.Contains(string(data), part) {
			t.Fatal("the baseline holds the value")
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Accepts(w, day) {
		t.Error("the saved entry does not accept its finding")
	}
	ge := got.Entries[w.Fingerprint()]
	if ge.Path != w.Path || ge.Context != "slicer-selection" || ge.Column == nil || *ge.Column != "enrollment.cohort_year" ||
		strings.Join(ge.Reasons, ",") != "warn-column" || ge.Note == "" {
		t.Errorf("entry = %+v", ge)
	}
}

// The corpus runner writes entries without a note; they load.
func TestLoadCorpusFormat(t *testing.T) {
	doc := `version = 1

[entries."fp1:0123456789abcdef01234567"]
severity = "warn"
expires = "2199-12-31"
path = "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json"
context = "slicer-selection"
column = "enrollment.cohort_year"
reasons = ["warn-column"]
`
	b, err := Load(writeFile(t, doc))
	if err != nil || len(b.Entries) != 1 {
		t.Fatalf("%v, %d entries", err, len(b.Entries))
	}
}

func TestPrintableKey(t *testing.T) {
	if PrintableKey("fp1:0a") != "fp1:0a" {
		t.Error("a fingerprint is printable")
	}
	if strings.Contains(PrintableKey(sentinel), "Doe") {
		t.Error("a free-text key is printed")
	}
}
