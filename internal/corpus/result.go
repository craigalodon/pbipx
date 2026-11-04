package corpus

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

// Finding is one normalized finding (docs/design/corpus.md, "expected.json").
type Finding struct {
	Path    string
	Context *string
	Column  *string
	Tier    string
	Reasons []string
	// Locator is the raw JSON locator, nil when the finding has none.
	Locator json.RawMessage
	// Fingerprint is an implementation's baseline key for the finding. It is
	// read only from output, never from expected.json.
	Fingerprint string
}

// Result is a normalized result: findings, a failure, a gate decision, or
// explain's structure.
type Result struct {
	Findings   []Finding
	Accepted   int
	Suppressed int
	Failure    string

	Decision string
	Reason   string

	// OK and Refusal are the result of stage and collect: it succeeded, or
	// it refused with a class of the vocabulary's refusals. A refusal of
	// findings also has Findings.
	OK      bool
	Refusal string

	// Explain is explain's structure, whose schema is not settled yet.
	Explain map[string]json.RawMessage
}

// Gate decisions.
const (
	decisionAllow = "allow"
	decisionDeny  = "deny"
)

// Finding tiers.
const (
	tierBlock = "block"
	tierWarn  = "warn"
	tierError = "error"
)

// ParseResult decodes a normalized result for a command.
//
// Strict is for expected.json: every key must be known, and every context,
// reason, tier, failure and gate reason must be in the vocabulary. Without it,
// for an implementation's output, unknown keys are ignored and only the types
// are checked.
//
// Problems name the rule and the position, never the text.
func ParseResult(data []byte, command string, vocab *Vocabulary, strict bool) (*Result, []string) {
	p := &resultParser{vocab: vocab, strict: strict}
	top, ok := p.object(data, "the result")
	if !ok {
		return nil, p.problems
	}
	res := &Result{}
	switch {
	case command == commandGate:
		p.gate(top, res)
	case present(top, "failure"):
		p.failure(top, res)
	case command == commandExplain:
		res.Explain = top
	case command == commandStage || command == commandCollect:
		p.workflow(top, res)
	default:
		p.findings(top, res)
	}
	if len(p.problems) > 0 {
		return nil, p.problems
	}
	return res, nil
}

type resultParser struct {
	vocab    *Vocabulary
	strict   bool
	problems []string
}

func (p *resultParser) addf(format string, args ...any) {
	p.problems = append(p.problems, fmt.Sprintf(format, args...))
}

// present reports whether key is set to something other than null.
func present(obj map[string]json.RawMessage, key string) bool {
	raw, ok := obj[key]
	return ok && !isNull(raw)
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// object decodes a JSON object. what names it in a problem.
func (p *resultParser) object(data []byte, what string) (map[string]json.RawMessage, bool) {
	if len(bytes.TrimSpace(data)) == 0 {
		p.addf("%s is empty", what)
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			p.addf("%s is not valid JSON (line %d)", what, lineAt(data, syn.Offset))
		} else {
			p.addf("%s is not a JSON object", what)
		}
		return nil, false
	}
	return obj, true
}

// allowKeys reports, in strict mode, every key of obj not in allowed.
func (p *resultParser) allowKeys(obj map[string]json.RawMessage, what string, allowed ...string) {
	if !p.strict {
		return
	}
	var unknown []string
	for k := range obj {
		if !slices.Contains(allowed, k) {
			unknown = append(unknown, k)
		}
	}
	slices.Sort(unknown)
	for _, k := range unknown {
		if plainKey.MatchString(k) {
			p.addf("%s has an unknown key %q", what, k)
		} else {
			p.addf("%s has an unknown key", what)
		}
	}
}

// str decodes a string, or reports that the key is not one.
func (p *resultParser) str(raw json.RawMessage, what string) (string, bool) {
	var s string
	if raw == nil || isNull(raw) || json.Unmarshal(raw, &s) != nil {
		p.addf("%s is not a string", what)
		return "", false
	}
	return s, true
}

// nullableStr decodes a string or null.
func (p *resultParser) nullableStr(obj map[string]json.RawMessage, key, what string) *string {
	raw, ok := obj[key]
	if !ok {
		p.addf("%s is missing", what)
		return nil
	}
	if isNull(raw) {
		return nil
	}
	s, ok := p.str(raw, what)
	if !ok {
		return nil
	}
	return &s
}

func (p *resultParser) count(obj map[string]json.RawMessage, key string) int {
	raw, ok := obj[key]
	if !ok || (!p.strict && isNull(raw)) {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) != nil || n < 0 {
		p.addf("%s is not a non-negative integer", key)
		return 0
	}
	return n
}

func (p *resultParser) gate(top map[string]json.RawMessage, res *Result) {
	p.allowKeys(top, "the gate result", "decision", "reason")
	res.Decision, _ = p.str(top["decision"], "decision")
	switch res.Decision {
	case decisionAllow:
		if _, ok := top["reason"]; ok {
			p.addf("an allow has no reason")
		}
	case decisionDeny:
		res.Reason, _ = p.str(top["reason"], "reason")
		if _, ok := p.vocab.GateReasons[res.Reason]; !ok && res.Reason != "" {
			p.addf("reason is not one of the vocabulary's gate-reasons")
		}
	default:
		p.addf("decision is neither %q nor %q", decisionAllow, decisionDeny)
	}
}

func (p *resultParser) failure(top map[string]json.RawMessage, res *Result) {
	if p.strict && present(top, "findings") {
		p.addf("failure and findings are both present")
	}
	p.allowKeys(top, "the failure result", "failure")
	res.Failure, _ = p.str(top["failure"], "failure")
	if _, ok := p.vocab.Failures[res.Failure]; p.strict && !ok && res.Failure != "" {
		p.addf("failure is not one of the vocabulary's failures")
	}
}

func (p *resultParser) findings(top map[string]json.RawMessage, res *Result) {
	p.allowKeys(top, "the result", "findings", "accepted", "suppressed")
	res.Accepted = p.count(top, "accepted")
	res.Suppressed = p.count(top, "suppressed")
	raw, ok := top["findings"]
	if !ok {
		p.addf("findings is missing (an empty result is \"findings\": [])")
		return
	}
	p.findingItems(raw, res)
}

// workflow decodes the result of stage or collect: exactly one of "ok" and
// "refusal", and, for the refusal "findings", the findings that caused it.
func (p *resultParser) workflow(top map[string]json.RawMessage, res *Result) {
	p.allowKeys(top, "the result", "ok", "refusal", "findings")
	if present(top, "ok") {
		var ok bool
		if json.Unmarshal(top["ok"], &ok) != nil || !ok {
			p.addf("ok is not true")
		}
		res.OK = ok
	}
	if present(top, "refusal") {
		res.Refusal, _ = p.str(top["refusal"], "refusal")
		if _, known := p.vocab.Refusals[res.Refusal]; p.strict && !known && res.Refusal != "" {
			p.addf("refusal is not one of the vocabulary's refusals")
		}
	}
	if res.OK == (res.Refusal != "") {
		p.addf("the result needs exactly one of ok and refusal")
	}
	hasFindings := present(top, "findings")
	switch {
	case p.strict && hasFindings && res.Refusal != refusalFindings:
		p.addf("findings only go with the refusal %s", refusalFindings)
	case p.strict && !hasFindings && res.Refusal == refusalFindings:
		p.addf("the refusal %s needs findings", refusalFindings)
	}
	if hasFindings {
		p.findingItems(top["findings"], res)
	}
}

// refusalFindings is the refusal of a collect that found a protected value.
const refusalFindings = "findings"

// findingItems decodes the findings array.
func (p *resultParser) findingItems(raw json.RawMessage, res *Result) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		p.addf("findings is not an array")
		return
	}
	res.Findings = make([]Finding, 0, len(items))
	for i, item := range items {
		f, ok := p.finding(item, i)
		if !ok {
			continue
		}
		if p.strict && len(res.Findings) > 0 && compareFindings(res.Findings[len(res.Findings)-1], f, false) > 0 {
			p.addf("findings[%d] is out of order (sort by path, context, column, then reasons)", i)
		}
		res.Findings = append(res.Findings, f)
	}
}

func (p *resultParser) finding(item json.RawMessage, i int) (Finding, bool) {
	var f Finding
	what := fmt.Sprintf("findings[%d]", i)
	var obj map[string]json.RawMessage
	if json.Unmarshal(item, &obj) != nil || obj == nil {
		p.addf("%s is not an object", what)
		return f, false
	}
	before := len(p.problems)
	p.allowKeys(obj, what, "path", "context", "column", "tier", "reasons", "locator")

	f.Path, _ = p.str(obj["path"], what+".path")
	f.Context = p.nullableStr(obj, "context", what+".context")
	f.Column = p.nullableStr(obj, "column", what+".column")
	f.Tier, _ = p.str(obj["tier"], what+".tier")
	if raw, ok := obj["reasons"]; !ok || json.Unmarshal(raw, &f.Reasons) != nil || f.Reasons == nil {
		p.addf("%s.reasons is not an array of strings", what)
	}
	if raw, ok := obj["locator"]; ok {
		f.Locator = raw
	}
	if raw, ok := obj["fingerprint"]; ok && !p.strict {
		f.Fingerprint, _ = p.str(raw, what+".fingerprint")
	}
	if p.strict && len(p.problems) == before {
		p.checkFinding(f, what)
	}
	return f, len(p.problems) == before
}

// checkFinding applies expected.json's rules to a well-typed finding.
func (p *resultParser) checkFinding(f Finding, what string) {
	if !validRelPath(f.Path) {
		p.addf("%s.path is not a clean relative path with forward slashes", what)
	}
	if f.Column != nil && *f.Column == "" {
		p.addf("%s.column is empty (use null when no column decided the verdict)", what)
	}
	if _, ok := p.vocab.Tiers[f.Tier]; !ok {
		p.addf("%s.tier is not one of the vocabulary's tiers", what)
		return
	}
	if len(f.Reasons) == 0 {
		p.addf("%s.reasons is empty", what)
		return
	}
	if f.Tier == tierError {
		if f.Context != nil || f.Column != nil {
			p.addf("%s is an error finding: context and column must be null", what)
		}
		if len(f.Reasons) != 1 || p.vocab.Reasons[f.Reasons[0]].Kind != reasonError {
			p.addf("%s is an error finding: it needs exactly one error reason", what)
		}
		return
	}
	if f.Context == nil {
		p.addf("%s.context is null on a %s finding", what, f.Tier)
	} else if _, ok := p.vocab.Contexts[*f.Context]; !ok {
		p.addf("%s.context is not one of the vocabulary's contexts", what)
	}
	last := -1
	for j, r := range f.Reasons {
		rank, ok := p.vocab.reasonRank[r]
		if !ok {
			p.addf("%s.reasons[%d] is not one of the vocabulary's finding reasons", what, j)
			return
		}
		if rank <= last {
			p.addf("%s.reasons is not in the vocabulary's reason-order, or repeats one", what)
			return
		}
		last = rank
	}
}

// validRelPath reports whether p is relative, slash-separated and clean.
func validRelPath(p string) bool {
	return p != "" && p != "." && !strings.Contains(p, `\`) && !path.IsAbs(p) &&
		path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}

// compareFindings orders findings by path, context (null first), column (null
// first), then reasons element by element. With full, it then orders by tier
// and locator, so that equal findings are adjacent and the order is total.
func compareFindings(a, b Finding, full bool) int {
	if c := cmp.Compare(a.Path, b.Path); c != 0 {
		return c
	}
	if c := compareNullable(a.Context, b.Context); c != 0 {
		return c
	}
	if c := compareNullable(a.Column, b.Column); c != 0 {
		return c
	}
	if c := slices.Compare(a.Reasons, b.Reasons); c != 0 {
		return c
	}
	if !full {
		return 0
	}
	if c := cmp.Compare(a.Tier, b.Tier); c != 0 {
		return c
	}
	return bytes.Compare(canonicalJSON(a.Locator), canonicalJSON(b.Locator))
}

func compareNullable(a, b *string) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return cmp.Compare(*a, *b)
}

// canonicalJSON is raw re-encoded with sorted keys and no insignificant
// whitespace, so that equal JSON values compare equal.
func canonicalJSON(raw json.RawMessage) []byte {
	if raw == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// lineAt is the 1-based line of a byte offset.
func lineAt(data []byte, offset int64) int {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	return bytes.Count(data[:offset], []byte("\n")) + 1
}
