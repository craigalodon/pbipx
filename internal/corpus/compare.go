package corpus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// CompareResults compares an implementation's normalized result with the
// expected one and describes each difference. Only what expected.json can
// say is compared:
//
//   - a failure, when one is expected; otherwise there must be none;
//   - for the gate, the decision and the reason class;
//   - accepted and suppressed;
//   - findings as a multiset: order does not matter, and a finding's locator
//     is compared only when the expected finding has one;
//   - for explain, every key of the expected object, recursively.
//
// A description names structure only: paths, contexts, columns, tiers and
// reasons, which are schema, never values.
func CompareResults(want, got *Result) []string {
	var diffs []string
	addf := func(format string, args ...any) { diffs = append(diffs, fmt.Sprintf(format, args...)) }

	if want.Decision != "" {
		if got.Decision != want.Decision || got.Reason != want.Reason {
			addf("gate: got %s, want %s", describeDecision(got), describeDecision(want))
		}
		return diffs
	}
	if want.Failure != "" || got.Failure != "" {
		if got.Failure != want.Failure {
			addf("failure: got %s, want %s", describeClass(got.Failure), describeClass(want.Failure))
		}
		return diffs
	}
	if want.OK || want.Refusal != "" || got.OK || got.Refusal != "" {
		if got.OK != want.OK || got.Refusal != want.Refusal {
			addf("got %s, want %s", describeOutcome(got), describeOutcome(want))
			return diffs
		}
	}
	if want.Explain != nil {
		for _, d := range jsonSubset(want.Explain, got.Explain, "") {
			addf("explain: %s", d)
		}
		return diffs
	}
	if got.Accepted != want.Accepted {
		addf("accepted = %d, want %d", got.Accepted, want.Accepted)
	}
	if got.Suppressed != want.Suppressed {
		addf("suppressed = %d, want %d", got.Suppressed, want.Suppressed)
	}
	missing, extra := matchFindings(want.Findings, got.Findings)
	for _, f := range missing {
		addf("missing finding: %s", describeFinding(f))
	}
	for _, f := range extra {
		addf("unexpected finding: %s", describeFinding(f))
	}
	return diffs
}

// matchFindings pairs expected findings with actual ones and returns those
// left unpaired on each side. Expected findings with a locator are paired
// first, so that one without a locator never takes the only actual finding a
// located one could match.
func matchFindings(want, got []Finding) (missing, extra []Finding) {
	order := slices.Clone(want)
	slices.SortStableFunc(order, func(a, b Finding) int {
		la, lb := a.Locator != nil, b.Locator != nil
		switch {
		case la == lb:
			return compareFindings(a, b, true)
		case la:
			return -1
		default:
			return 1
		}
	})
	used := make([]bool, len(got))
	for _, w := range order {
		found := false
		for i, g := range got {
			if used[i] || compareFindings(w, g, false) != 0 || w.Tier != g.Tier {
				continue
			}
			if w.Locator != nil && !bytes.Equal(canonicalJSON(w.Locator), canonicalJSON(g.Locator)) {
				continue
			}
			used[i], found = true, true
			break
		}
		if !found {
			missing = append(missing, w)
		}
	}
	for i, g := range got {
		if !used[i] {
			extra = append(extra, g)
		}
	}
	sortFindings := func(fs []Finding) {
		slices.SortFunc(fs, func(a, b Finding) int { return compareFindings(a, b, true) })
	}
	sortFindings(missing)
	sortFindings(extra)
	return missing, extra
}

// jsonSubset reports where got lacks or differs from a key of want. Objects
// are compared key by key; anything else must be equal.
func jsonSubset(want, got map[string]json.RawMessage, at string) []string {
	var diffs []string
	for _, k := range sortedKeys(want) {
		where := at + "." + k
		if !plainKey.MatchString(k) {
			where = at + ".(key not printed)"
		}
		g, ok := got[k]
		if !ok {
			diffs = append(diffs, where+" is missing")
			continue
		}
		var wObj, gObj map[string]json.RawMessage
		if json.Unmarshal(want[k], &wObj) == nil && wObj != nil {
			if json.Unmarshal(g, &gObj) != nil || gObj == nil {
				diffs = append(diffs, where+" is not an object")
				continue
			}
			diffs = append(diffs, jsonSubset(wObj, gObj, where)...)
			continue
		}
		if !bytes.Equal(canonicalJSON(want[k]), canonicalJSON(g)) {
			diffs = append(diffs, where+" differs")
		}
	}
	return diffs
}

func describeOutcome(r *Result) string {
	if r.OK {
		return "ok"
	}
	if r.Refusal == "" {
		return "none"
	}
	return "refusal " + describeClass(r.Refusal)
}

func describeDecision(r *Result) string {
	if r.Decision == decisionDeny {
		return "deny (" + describeClass(r.Reason) + ")"
	}
	return describeClass(r.Decision)
}

// describeClass prints a vocabulary word, or a placeholder for anything that
// does not look like one.
func describeClass(s string) string {
	switch {
	case s == "":
		return "none"
	case plainKey.MatchString(s):
		return s
	default:
		return "(not a vocabulary word)"
	}
}

func describeFinding(f Finding) string {
	null := func(s *string) string {
		if s == nil {
			return "null"
		}
		return *s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "path=%s context=%s column=%s tier=%s reasons=[%s]",
		f.Path, null(f.Context), null(f.Column), f.Tier, strings.Join(f.Reasons, ","))
	if f.Locator != nil {
		fmt.Fprintf(&b, " locator=%s", canonicalJSON(f.Locator))
	}
	return b.String()
}
