package finding

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/secret"
)

// The value planted in these tests. It must never be written.
const sentinel = "Doe, Jane"

func TestVocabularyInStep(t *testing.T) {
	v, err := corpus.LoadVocabulary("../../testdata/corpus/vocabulary.toml")
	if err != nil {
		t.Fatal(err)
	}
	sameSet(t, "contexts", mapKeys(v.Contexts), Contexts)
	var findingReasons, errorReasons []string
	for name, r := range v.Reasons {
		if r.Kind == "finding" {
			findingReasons = append(findingReasons, name)
		} else {
			errorReasons = append(errorReasons, name)
		}
	}
	sameSet(t, "finding reasons", findingReasons, ReasonOrder)
	sameSet(t, "error reasons", errorReasons, ErrorReasons)
	sameSet(t, "failures", mapKeys(v.Failures), Failures)
	sameSet(t, "tiers", mapKeys(v.Tiers), []Tier{Block, Warn, Error})
	order := make([]string, len(ReasonOrder))
	for i, r := range ReasonOrder {
		order[i] = string(r)
	}
	if !slices.Equal(order, v.ReasonOrder.Order) {
		t.Errorf("ReasonOrder differs from the vocabulary's reason-order")
	}
	codes := map[string]int{"0": ExitClean, "1": ExitBlock, "2": ExitWarnStrict, "3": ExitOperational, "4": ExitUsage}
	sameSet(t, "exit codes", mapKeys(v.ExitCodes), mapKeys(codes))
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func sameSet[T ~string](t *testing.T, what string, vocab []string, ours []T) {
	t.Helper()
	got := make([]string, len(ours))
	for i, o := range ours {
		got[i] = string(o)
	}
	slices.Sort(got)
	want := slices.Clone(vocab)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s: package has %v, vocabulary has %v", what, got, want)
	}
}

func block(path string) Finding {
	return Finding{
		Path:    path,
		Context: SlicerSelection,
		Locator: "filter.Where[0]",
		Column:  &Column{Table: "student", Name: "full_name"},
		Tier:    Block,
		Reasons: []Reason{BlockColumn, PersonNameShape},
		Value:   secret.New(sentinel),
	}
}

func TestFingerprintIgnoresTheValue(t *testing.T) {
	a := block("a.json")
	b := a
	b.Value = secret.New("Roe, Richard")
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("a changed value changed the fingerprint")
	}
	c := a
	c.Reasons = []Reason{BlockColumn, PersonNameShape, EmailShape}
	if a.Fingerprint() == c.Fingerprint() {
		t.Error("an added reason kept the fingerprint")
	}
	d := a
	d.Reasons = []Reason{PersonNameShape, BlockColumn}
	if a.Fingerprint() != d.Fingerprint() {
		t.Error("reason order changed the fingerprint")
	}
	if !strings.HasPrefix(a.Fingerprint(), "fp1:") {
		t.Error("fingerprint has no version prefix")
	}
}

func TestExitCode(t *testing.T) {
	warn := Finding{Path: "w.json", Context: Filter, Tier: Warn, Reasons: []Reason{WarnColumn}}
	errf := Finding{Path: "e.json", Tier: Error, Reasons: []Reason{UnparseableJSON}}
	tests := []struct {
		name   string
		result Result
		strict bool
		want   int
	}{
		{"clean", Result{}, false, ExitClean},
		{"warning", Result{Findings: []Finding{warn}}, false, ExitClean},
		{"warning strict", Result{Findings: []Finding{warn}}, true, ExitWarnStrict},
		{"block", Result{Findings: []Finding{warn, block("b.json")}}, true, ExitBlock},
		{"error outranks block", Result{Findings: []Finding{block("b.json"), errf}}, false, ExitOperational},
		{"failure", Result{Failure: PolicyError}, false, ExitOperational},
	}
	for _, tt := range tests {
		if got := tt.result.ExitCode(tt.strict); got != tt.want {
			t.Errorf("%s: exit %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	errf := Finding{Path: "a.json", Tier: Error, Reasons: []Reason{UnparseableJSON}}
	r := Result{Findings: []Finding{block("b.json"), errf}, Suppressed: 1}
	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Doe") || strings.Contains(buf.String(), "Jane") {
		t.Fatal("JSON output carries the value")
	}
	var got struct {
		Findings []struct {
			Path        string   `json:"path"`
			Context     *string  `json:"context"`
			Column      *string  `json:"column"`
			Tier        string   `json:"tier"`
			Reasons     []string `json:"reasons"`
			Fingerprint string   `json:"fingerprint"`
		} `json:"findings"`
		Accepted   int `json:"accepted"`
		Suppressed int `json:"suppressed"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Findings) != 2 || got.Suppressed != 1 {
		t.Fatalf("got %d findings, %d suppressed", len(got.Findings), got.Suppressed)
	}
	first, second := got.Findings[0], got.Findings[1]
	if first.Path != "a.json" || first.Context != nil || first.Column != nil || first.Tier != "error" {
		t.Errorf("error finding not first, or has a context or column: %+v", first)
	}
	if second.Column == nil || *second.Column != "student.full_name" || second.Fingerprint == "" {
		t.Errorf("block finding lost its column or fingerprint: %+v", second)
	}
}

func TestWriteJSONFailure(t *testing.T) {
	var buf bytes.Buffer
	if err := (Result{Failure: BaselineError}).WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != `{
  "failure": "baseline-error"
}` {
		t.Errorf("failure output is %q", buf.String())
	}
}

func TestSortReasons(t *testing.T) {
	r := []Reason{PhoneShape, PersonNameShape, BlockColumn}
	SortReasons(r)
	if !slices.Equal(r, []Reason{BlockColumn, PersonNameShape, PhoneShape}) {
		t.Errorf("sorted to %v", r)
	}
}

func TestColumnString(t *testing.T) {
	if (Column{Name: "full_name"}).String() != "full_name" {
		t.Error("bare column")
	}
	if (Column{Table: "student", Name: "full_name"}).String() != "student.full_name" {
		t.Error("qualified column")
	}
}
