package pbir

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/shape"
)

const describedVisual = `{
  "filterConfig": {
    "filters": [
      {
        "field": {"Column": {"Expression": {"SourceRef": {"Entity": "enrollment"}}, "Property": "cohort_year"}},
        "filter": {
          "From": [{"Name": "e", "Entity": "enrollment"}],
          "Where": [{"Condition": {"In": {
            "Expressions": [{"Column": {"Expression": {"SourceRef": {"Source": "e"}}, "Property": "cohort_year"}}],
            "Values": [[{"Literal": {"Value": "'2105'"}}], [{"Literal": {"Value": "2106L"}}]]
          }}}]
        }
      }
    ]
  },
  "visual": {
    "objects": {
      "general": [{"properties": {"filter": {"filter": {
        "From": [{"Name": "s", "Entity": "Doe, Jane"}],
        "Where": [{"Condition": {"In": {
          "Expressions": [
            {"Column": {"Expression": {"SourceRef": {"Source": "s"}}, "Property": "full_name"}},
            {"Column": {"Expression": {"SourceRef": {"Source": "s"}}, "Property": "jane.doe@example.com"}},
            {"Column": {"Property": "status"}}
          ],
          "Values": [[{"Literal": {"Value": "'Doe, Jane'"}}, {"Literal": {"Value": "datetime'2105-03-14T00:00:00'"}}]]
        }}}]
      }}}}]
    },
    "visualContainerObjects": {
      "title": [{"properties": {"text": {"expr": {"Literal": {"Value": "'Roster for Roe, Richard'"}}}}}]
    }
  }
}`

func TestDescribeReport(t *testing.T) {
	d, err := Describe([]byte(describedVisual))
	if err != nil {
		t.Fatal(err)
	}
	w := shape.Withheld
	want := []Context{
		{Context: "filter", Strings: 1, Columns: []string{"enrollment.cohort_year"}},
		{Context: "slicer-selection", Strings: 1, Columns: []string{w + "." + w, w + ".full_name", "status"}},
		{Context: "title-text", Strings: 1, Columns: []string{}},
	}
	if len(d.Contexts) != len(want) {
		t.Fatalf("contexts = %+v", d.Contexts)
	}
	for i := range want {
		g := d.Contexts[i]
		if g.Context != want[i].Context || g.Strings != want[i].Strings || !slices.Equal(g.Columns, want[i].Columns) {
			t.Errorf("context %d = %+v, want %+v", i, g, want[i])
		}
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"Doe", "Jane", "2105", "Roe", "Richard"} {
		if strings.Contains(string(out), v) {
			t.Errorf("the description carries a planted value")
		}
	}
}

func TestDescribeReportErrors(t *testing.T) {
	for _, src := range []string{`{"visual": `, `["Doe, Jane"]`} {
		_, err := Describe([]byte(src))
		var pe *judge.ParseError
		if !errors.As(err, &pe) || pe.Reason != finding.UnparseableJSON {
			t.Errorf("err = %v, want unparseable-json", err)
		}
	}
}
