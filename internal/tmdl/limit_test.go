package tmdl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/exprlex"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/tmdl"
)

// TestScanAssociationLimit checks that code past the association limits is
// an unparseable-tmdl error at the expression's line, in DAX, M and native
// SQL, and not a literal judged on some of its columns.
func TestScanAssociationLimit(t *testing.T) {
	cols := func(format string) string {
		out := make([]string, exprlex.MaxColumnsPerLiteral+1)
		for i := range out {
			out[i] = strings.ReplaceAll(format, "%", "c"+strings.Repeat("x", i))
		}
		return strings.Join(out, ", ")
	}
	cases := []struct {
		name string
		src  string
		line int
	}{
		{"DAX", "table student\n\tmeasure m = CONTAINSSTRING(\"" + sentinel + "\", " + cols("[%]") + ")\n", 2},
		{"M", "expression E =\n\t\tList.Contains({" + cols("[%]") + "}, \"" + sentinel + "\")\n", 2},
		{"native SQL", "expression E =\n\t\tlet\n\t\t\tQ = Value.NativeQuery(S, \"select a from t where '" + sentinel + "' = coalesce(" + cols("%") + ")\")\n\t\tin Q\n", 3},
	}
	for _, tc := range cases {
		fs, err := tmdl.Scan(scanPath, []byte(tc.src), loadScanPolicy(t))
		var pe *judge.ParseError
		switch {
		case !errors.As(err, &pe):
			t.Errorf("%s: err = %v, want a parse error", tc.name, err)
			continue
		case len(fs) != 0:
			t.Errorf("%s: findings returned with an error", tc.name)
		case pe.Reason != finding.UnparseableTMDL || pe.Line != tc.line || pe.Msg != exprlex.MsgTooLarge:
			t.Errorf("%s: reason %s line %d %q, want line %d", tc.name, pe.Reason, pe.Line, pe.Msg, tc.line)
		}
		if strings.Contains(err.Error(), "Doe") {
			t.Errorf("%s: the error carries source text", tc.name)
		}
	}
}
