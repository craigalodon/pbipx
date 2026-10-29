package corpus

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const (
	accepted = "baseline/accepted-warning-after-change"
	record   = "baseline/record-names-a-finding"
	stdinTxt = "text/stdin-line-blocks"
	noPolicy = "policy/missing-policy-fails"

	warnVisual = "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json"
)

func TestValidateBaselineAndStdinRules(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		mutate func(t *testing.T, dir string)
		want   string
	}{
		{"severity missing", accepted, replace("case.toml", "severity = \"warn\"\n", ""), "baseline[0].severity is missing"},
		{"expires missing", accepted, replace("case.toml", "expires  = \"2199-12-31\"\n", ""), "baseline[0].expires is missing"},
		{"unknown entry key", accepted, appendTo("case.toml", "note = \"x\"\n"), `unknown key "note"`},
		{"context not in vocabulary", accepted, replace("case.toml", `"slicer-selection"`, `"slicer"`), "baseline[0].context is not one of"},
		{"path not in tree", accepted, replace("case.toml", "visual.json\"", "page.json\""), "baseline[0].path names no path in tree/"},
		{"reasons out of order", accepted, replace("case.toml", `["warn-column"]`, `["date-shape", "warn-column"]`), "baseline[0].reasons is not in"},
		{"empty column", record, replace("case.toml", `"enrollment.cohort_year"`, `""`), "fingerprint.column is empty"},
		{"placeholder without fingerprint", accepted, replace("case.toml", `["--all", "--strict"]`, `["--all", "{fingerprint}"]`), "no [fingerprint]"},
		{"fingerprint unused", record, replace("case.toml", `"{fingerprint}"`, `"0000"`), "no argument uses {fingerprint}"},
		{"after without a baseline", slicer, write("after/tree/sql/a.sql", "select 1;\n"), "applied only in a case with"},
		{"after with a stray entry", accepted, write("after/notes.txt", "x\n"), "after/ holds only tree/"},
		{"after report with a final newline", accepted, appendTo("after/tree/"+warnVisual, "\n"), "after/tree/pbip/Example.Report"},
		{"record with a result", record, write("expected.json", `{"findings": []}`), "present, but a usage error, baseline record"},
		{"sentinel nowhere", accepted, replace("case.toml", `"Fall 2151"]`, `"Fall 2152"]`), "sentinels[1] appears nowhere"},
		{"stdin path outside scan-text", stdinTxt, replace("case.toml", `"scan-text"`, `"scan"`), "findings[0].path names no path"},
		{"stdin path without stdin", stdinTxt, remove("stdin"), "findings[0].path names no path"},
		{"tree policy missing without a policy error", noPolicy, write("expected.json", `{"findings": []}`), "tree/.pbipx.toml does not exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestCorpus(t)
			tt.mutate(t, filepath.Join(dir, filepath.FromSlash(tt.id)))
			c := openTest(t, dir)
			var all []string
			for _, cs := range c.Cases {
				for _, e := range c.Validate(cs) {
					all = append(all, e.Error())
				}
			}
			found := false
			for _, e := range all {
				if strings.Contains(e, "Fall 215") || strings.Contains(e, "Roe") {
					t.Errorf("a problem prints a sentinel")
				}
				found = found || strings.Contains(e, tt.want)
			}
			if !found {
				t.Errorf("no problem contains %q; got %q", tt.want, all)
			}
		})
	}
}

func deflated(t *testing.T, s string) string {
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

func TestPlanted(t *testing.T) {
	rows := `[["Doe, Jane","2150"]]`
	blob := func(b64 string) string {
		return "\t\t\tSource = Table.FromRows(Json.Document(Binary.Decompress(Binary.FromText(\"" + b64 +
			"\", BinaryEncoding.Base64), Compression.Deflate)))\n"
	}
	tests := []struct {
		name, data, rel, sentinel string
		want                      bool
	}{
		{"as written", "x Doe, Jane", "a.md", testSentinel, true},
		{"absent", "x Doe", "a.md", testSentinel, false},
		{"apostrophe doubled", "'O''Doe, Jane'", "a.sql", "O'Doe, Jane", true},
		{"deflated blob", blob(deflated(t, rows)), "t.tmdl", testSentinel, true},
		{"plain blob", blob(base64.StdEncoding.EncodeToString([]byte(rows))), "t.tmdl", testSentinel, true},
		{"blob outside TMDL", blob(deflated(t, rows)), "t.json", testSentinel, false},
		{"blob without it", blob(deflated(t, `[["Roe"]]`)), "t.tmdl", testSentinel, false},
	}
	for _, tt := range tests {
		if got := planted([]byte(tt.data), tt.rel, tt.sentinel); got != tt.want {
			t.Errorf("%s: planted = %v, want %v", tt.name, got, tt.want)
		}
	}
	if !containsSentinel([]byte("O''Doe, Jane"), "O'Doe, Jane") {
		t.Error("output with the apostrophe doubled does not count as the sentinel")
	}
}

func strp(s string) *string { return &s }

func TestBuildBaseline(t *testing.T) {
	findings := []Finding{
		{Path: warnVisual, Context: strp("slicer-selection"), Column: strp("enrollment.cohort_year"), Tier: tierWarn, Reasons: []string{"warn-column"}, Fingerprint: "fp-1"},
		{Path: warnVisual, Context: strp("filter"), Tier: tierBlock, Reasons: []string{"person-name-shape"}, Fingerprint: "fp-2"},
	}
	entries := []BaselineEntry{
		{Path: warnVisual, Context: "slicer-selection", Column: strp("enrollment.cohort_year"), Reasons: []string{"warn-column"}, Severity: "warn", Expires: "2199-12-31"},
		{Path: warnVisual, Context: "filter", Reasons: []string{"person-name-shape"}, Severity: "block", Expires: "not a date"},
	}
	data, err := buildBaseline(entries, findings)
	if err != nil {
		t.Fatal(err)
	}
	var got pbipxBaseline
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Entries) != 2 {
		t.Fatalf("version %d with %d entries", got.Version, len(got.Entries))
	}
	if e := got.Entries["fp-1"]; e.Severity != "warn" || e.Expires != "2199-12-31" || e.Column == nil || e.Context != "slicer-selection" {
		t.Errorf("entry fp-1 = %+v", e)
	}
	if e := got.Entries["fp-2"]; e.Severity != "block" || e.Expires != "not a date" || e.Column != nil {
		t.Errorf("entry fp-2 = %+v", e)
	}
	if bytes.Contains(data, []byte("column = ''")) || bytes.Contains(data, []byte(`column = ""`)) {
		t.Error("a null column is written as an empty one")
	}

	for name, f := range map[string][]Finding{
		"no match":       findings[1:],
		"two matches":    {findings[0], findings[0], findings[1]},
		"no fingerprint": {{Path: warnVisual, Context: strp("slicer-selection"), Column: strp("enrollment.cohort_year"), Reasons: []string{"warn-column"}}, findings[1]},
	} {
		if _, err := buildBaseline(entries, f); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := buildBaseline([]BaselineEntry{entries[0], entries[0]}, findings); err == nil {
		t.Error("two entries for one finding: no error")
	}
}

// fakeScanner writes a shell script that stands in for pbipx's setup scan:
// it records its arguments beside itself and prints one warning with a
// fingerprint.
func fakeScanner(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake scanner is a shell script")
	}
	bin := filepath.Join(t.TempDir(), "fake-pbipx")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$0.args"
cat <<'EOF'
{"findings": [{"path": "` + warnVisual + `", "context": "slicer-selection",
 "column": "enrollment.cohort_year", "tier": "warn", "reasons": ["warn-column"],
 "locator": {"filter": 0}, "fingerprint": "fp-0001"}]}
EOF
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { //nolint:gosec // G306: the script must be executable
		t.Fatal(err)
	}
	return bin
}

func TestPrepareResolvesBaseline(t *testing.T) {
	needGit(t)
	bin := fakeScanner(t)
	c := openTest(t, testCorpusDir)
	cs := findCase(t, c, accepted)
	root := t.TempDir()
	p, err := c.Prepare(cs, root, bin)
	if err != nil {
		t.Fatal(err)
	}
	args, err := readIn(filepath.Dir(bin), filepath.Base(bin)+".args")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(args)); !slices.Equal(got, resolveArgs) {
		t.Errorf("setup scan args = %q, want %q", got, resolveArgs)
	}
	data, err := readIn(root, baselineFile)
	if err != nil {
		t.Fatal(err)
	}
	var b pbipxBaseline
	if err := toml.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Entries["fp-0001"]; !ok || len(b.Entries) != 1 {
		t.Errorf("baseline entries = %d, want fp-0001 only", len(b.Entries))
	}
	got, err := readIn(root, filepath.FromSlash(warnVisual))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := readIn(cs.AfterTreeDir(), filepath.FromSlash(warnVisual))
	if !bytes.Equal(got, want) {
		t.Error("after/tree/ was not applied")
	}
	if tracked := gitOut(t, root, "ls-tree", "-r", "--name-only", "HEAD"); !strings.Contains(tracked, baselineFile) {
		t.Error("the baseline is not committed")
	}
	if status := gitOut(t, root, "status", "--porcelain"); status != "" {
		t.Error("after/tree/ is not committed")
	}
	if p.Fingerprint != "" {
		t.Error("a case without [fingerprint] resolved one")
	}
}

func TestPrepareResolvesFingerprint(t *testing.T) {
	needGit(t)
	bin := fakeScanner(t)
	c := openTest(t, testCorpusDir)
	cs := findCase(t, c, record)
	p, err := c.Prepare(cs, t.TempDir(), bin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, baselineFile)); !os.IsNotExist(err) {
		t.Error("a case with no [[baseline]] has a baseline file")
	}
	want := []string{"baseline", "record", "fp-0001", "--note", "a reviewed selection"}
	if got := CommandArgs(cs, p); !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
}

func TestPrepareResolveFailsWithoutAMatch(t *testing.T) {
	needGit(t)
	bin := fakeScanner(t)
	dir := copyTestCorpus(t)
	replace("case.toml", `["warn-column"]`, `["warn-column", "date-shape"]`)(t, filepath.Join(dir, filepath.FromSlash(record)))
	c := openTest(t, dir)
	_, err := c.Prepare(findCase(t, c, record), t.TempDir(), bin)
	if err == nil || !strings.Contains(err.Error(), "matches 0 findings") {
		t.Errorf("err = %v, want a failed match", err)
	}
}
