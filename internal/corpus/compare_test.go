package corpus

import (
	"strings"
	"testing"
)

func parseActual(t *testing.T, c *Corpus, command, out string) *Result {
	t.Helper()
	res, problems := ParseResult([]byte(out), command, c.Vocab, false)
	if len(problems) > 0 {
		t.Fatalf("output does not parse: %q", problems)
	}
	return res
}

func TestCompareFindings(t *testing.T) {
	c := openTest(t, testCorpusDir)
	want := findCase(t, c, slicer).Expected
	const path = `"path": "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json"`
	const located = `{` + path + `, "context": "slicer-selection", "column": "student.full_name",
		"tier": "block", "reasons": ["block-column", "person-name-shape"],
		"locator": {"filter": 0}, "fingerprint": "0000"}`
	tests := []struct {
		name string
		out  string
		want []string
	}{
		{"extra fields are ignored", `{"version": 1, "findings": [` + located + `], "accepted": 0}`, nil},
		{"counts default to 0", `{"findings": [` + located + `]}`, nil},
		{"missing finding", `{"findings": []}`, []string{"missing finding: path=pbip/"}},
		{"unexpected finding", `{"findings": [` + located + `, ` + strings.Replace(located, `"block"`, `"warn"`, 1) + `]}`,
			[]string{"unexpected finding: path=pbip/"}},
		{"different reasons", `{"findings": [` + strings.Replace(located, `"block-column", `, "", 1) + `]}`,
			[]string{"missing finding", "unexpected finding"}},
		{"null column differs", `{"findings": [` + strings.Replace(located, `"student.full_name"`, "null", 1) + `]}`,
			[]string{"missing finding", "unexpected finding: path=pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json context=slicer-selection column=null"}},
		{"accepted differs", `{"findings": [` + located + `], "accepted": 2}`, []string{"accepted = 2, want 0"}},
		{"unexpected failure", `{"failure": "policy-error"}`, []string{"failure: got policy-error, want none"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := CompareResults(want, parseActual(t, c, commandScan, tt.out))
			if len(diffs) != len(tt.want) {
				t.Fatalf("got %d differences, want %d: %q", len(diffs), len(tt.want), diffs)
			}
			for i, w := range tt.want {
				if !strings.Contains(diffs[i], w) {
					t.Errorf("difference %d = %q, want it to contain %q", i, diffs[i], w)
				}
			}
		})
	}
}

func TestCompareLocators(t *testing.T) {
	c := openTest(t, testCorpusDir)
	finding := func(column, locator string) string {
		f := `{"path": "a.json", "context": "filter", "column": "` + column + `", "tier": "warn", "reasons": ["warn-column"]`
		if locator != "" {
			f += `, "locator": ` + locator
		}
		return f + `}`
	}
	tests := []struct {
		name      string
		want, got []string
		diffs     int
	}{
		{"unlocated expectation ignores the locator", []string{finding("t.c", "")}, []string{finding("t.c", `{"i": 1}`)}, 0},
		{"located expectation matches key order freely",
			[]string{finding("t.c", `{"i": 1, "j": 2}`)}, []string{finding("t.c", `{"j": 2, "i": 1}`)}, 0},
		{"located expectation differs", []string{finding("t.c", `{"i": 1}`)}, []string{finding("t.c", `{"i": 2}`)}, 2},
		{"located expectation is paired first",
			[]string{finding("t.c", ""), finding("t.c", `{"i": 2}`)},
			[]string{finding("t.c", `{"i": 2}`), finding("t.c", `{"i": 1}`)}, 0},
		{"duplicates count", []string{finding("t.c", ""), finding("t.c", "")}, []string{finding("t.c", "")}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := parseActual(t, c, commandScan, `{"findings": [`+strings.Join(tt.want, ",")+`]}`)
			got := parseActual(t, c, commandScan, `{"findings": [`+strings.Join(tt.got, ",")+`]}`)
			if diffs := CompareResults(want, got); len(diffs) != tt.diffs {
				t.Errorf("got %d differences, want %d: %q", len(diffs), tt.diffs, diffs)
			}
		})
	}
}

func TestCompareFailure(t *testing.T) {
	c := openTest(t, testCorpusDir)
	want := &Result{Failure: "policy-error"}
	if d := CompareResults(want, parseActual(t, c, commandScan, `{"failure": "policy-error", "detail": "line 3"}`)); len(d) != 0 {
		t.Errorf("same failure: %q", d)
	}
	if d := CompareResults(want, parseActual(t, c, commandScan, `{"findings": []}`)); len(d) != 1 {
		t.Errorf("no failure: %q", d)
	}
}

func TestCompareExplain(t *testing.T) {
	c := openTest(t, testCorpusDir)
	want := parseActual(t, c, commandExplain, `{"tables": {"student": {"columns": ["full_name"]}}}`)
	same := parseActual(t, c, commandExplain, `{"tables": {"student": {"columns": ["full_name"], "measures": []}}, "extra": 1}`)
	if d := CompareResults(want, same); len(d) != 0 {
		t.Errorf("superset: %q", d)
	}
	other := parseActual(t, c, commandExplain, `{"tables": {"student": {"columns": []}}}`)
	if d := CompareResults(want, other); len(d) != 1 || d[0] != "explain: .tables.student.columns differs" {
		t.Errorf("different: %q", d)
	}
}

func TestGateResult(t *testing.T) {
	hook := func(decision, reason string) string {
		return `{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "` +
			decision + `", "permissionDecisionReason": "` + reason + `"}}`
	}
	tests := []struct {
		name           string
		exit           int
		stdout, stderr string
		want           string // "allow", "deny/<class>", or "error"
	}{
		{"allow", 0, "", "", "allow"},
		{"deny from hook JSON", 2, hook("deny", "pbipx: finding: a.json student.full_name"), "", "deny/finding"},
		{"stderr wins over hook JSON", 2, hook("deny", "pbipx: unvettable: a.bin"), "pbipx: finding: x\n", "deny/finding"},
		{"hook JSON when stderr has no class", 2, hook("deny", "pbipx: unvettable: a.bin"), "denied\n", "deny/unvettable"},
		{"deny from stderr", 2, "", "pbipx: policy-error: line 3\nmore\n", "deny/policy-error"},
		{"deny without a class", 2, hook("deny", "denied"), "", "error"},
		{"hook JSON that allows", 2, hook("allow", "pbipx: finding: a"), "", "error"},
		{"stdout that is not JSON", 2, "denied", "", "error"},
		{"other exit", 1, "", "", "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := GateResult(tt.exit, []byte(tt.stdout), []byte(tt.stderr))
			got := "error"
			if err == nil {
				got = res.Decision
				if res.Reason != "" {
					got += "/" + res.Reason
				}
			}
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestContainsSentinel(t *testing.T) {
	const backslash = `\`
	for _, tt := range []struct {
		data, sentinel string
		want           bool
	}{
		{"x Doe, Jane y", "Doe, Jane", true},
		{"x Doe y", "Doe, Jane", false},
		{`{"v": "O\"Doe"}`, `O"Doe`, true},
		{`{"v": "a` + backslash + `u003cb"}`, "a<b", true},
		{`{"v": "a<b"}`, "a<b", true},
		{`{"v": "a` + backslash + `u0062"}`, "ab", false}, // an escape json.Marshal never writes
	} {
		if got := containsSentinel([]byte(tt.data), tt.sentinel); got != tt.want {
			t.Errorf("containsSentinel(%q, %q) = %v, want %v", tt.data, tt.sentinel, got, tt.want)
		}
	}
}

func TestCheck(t *testing.T) {
	c := openTest(t, testCorpusDir)
	located := `{"findings": [{"path": "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json",
		"context": "slicer-selection", "column": "student.full_name", "tier": "block",
		"reasons": ["block-column", "person-name-shape"]}]}`
	tests := []struct {
		name string
		id   string
		out  Outcome
		want []string
	}{
		{"pass", slicer, Outcome{Exit: 1, Stdout: []byte(located)}, nil},
		{"wrong exit", slicer, Outcome{Exit: 0, Stdout: []byte(located)}, []string{"exit = 0, want 1"}},
		{"sentinel in stderr", slicer, Outcome{Exit: 1, Stdout: []byte(located), Stderr: []byte("Doe, Jane")},
			[]string{"sentinels[0] appears in stderr"}},
		{"stdout not JSON", slicer, Outcome{Exit: 1, Stdout: []byte("1 finding")}, []string{"stdout is not a JSON result"}},
		{"usage error checks only exit", usage, Outcome{Exit: 4, Stderr: []byte("pbipx: usage error")}, nil},
		{"gate deny", gate, Outcome{Exit: 2, Stderr: []byte("pbipx: finding: a.json\n")}, nil},
		{"gate allow", gate, Outcome{Exit: 0}, []string{"exit = 0, want 2", "gate: got allow, want deny (finding)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := c.Check(findCase(t, c, tt.id), tt.out)
			if len(errs) != len(tt.want) {
				t.Fatalf("got %d problems, want %d: %v", len(errs), len(tt.want), errs)
			}
			for i, w := range tt.want {
				if !strings.Contains(errs[i].Error(), w) {
					t.Errorf("problem %d = %q, want it to contain %q", i, errs[i], w)
				}
				if strings.Contains(errs[i].Error(), testSentinel) {
					t.Errorf("problem %d prints a sentinel", i)
				}
			}
		})
	}
}
