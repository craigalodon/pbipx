package finding

import (
	"encoding/json"
	"io"
	"slices"
)

// Failure is why a command produced no scan result.
type Failure string

// Failures.
const (
	PolicyError    Failure = "policy-error"
	BaselineError  Failure = "baseline-error"
	NotARepository Failure = "not-a-repository"
	NoFailure      Failure = ""
)

// Failures lists every failure.
var Failures = []Failure{PolicyError, BaselineError, NotARepository}

// Exit codes for every command but the gate.
const (
	ExitClean       = 0 // clean, or warnings without --strict
	ExitBlock       = 1 // a block finding
	ExitWarnStrict  = 2 // warnings under --strict
	ExitOperational = 3 // an error finding, or a failure
	ExitUsage       = 4 // a usage error
)

// Result is the outcome of a scan.
type Result struct {
	Findings   []Finding
	Accepted   int // warnings accepted by policy
	Suppressed int // warnings accepted by the baseline
	Failure    Failure
}

// ExitCode is the process exit code for r. An error finding outranks a
// block, so a file that was not vetted is never reported as merely dirty.
func (r Result) ExitCode(strict bool) int {
	if r.Failure != NoFailure {
		return ExitOperational
	}
	code := ExitClean
	for _, f := range r.Findings {
		switch f.Tier {
		case Error:
			return ExitOperational
		case Block:
			code = ExitBlock
		case Warn:
			if strict && code == ExitClean {
				code = ExitWarnStrict
			}
		}
	}
	return code
}

// jsonFinding is a finding as --format json writes it. It has no field for
// the value.
type jsonFinding struct {
	Path        string   `json:"path"`
	Context     *string  `json:"context"`
	Column      *string  `json:"column"`
	Tier        Tier     `json:"tier"`
	Reasons     []Reason `json:"reasons"`
	Locator     string   `json:"locator,omitempty"`
	Fingerprint string   `json:"fingerprint"`
}

type jsonResult struct {
	Findings   []jsonFinding `json:"findings"`
	Accepted   int           `json:"accepted"`
	Suppressed int           `json:"suppressed"`
}

type jsonFailure struct {
	Failure Failure `json:"failure"`
}

// WriteJSON writes r in the corpus's normalized shape plus each finding's
// locator and fingerprint (docs/design/corpus.md). Findings are sorted first.
// No value is written.
func (r Result) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if r.Failure != NoFailure {
		return enc.Encode(jsonFailure{Failure: r.Failure})
	}
	findings := slices.Clone(r.Findings)
	Sort(findings)
	out := jsonResult{Findings: make([]jsonFinding, 0, len(findings)), Accepted: r.Accepted, Suppressed: r.Suppressed}
	for _, f := range findings {
		jf := jsonFinding{
			Path:        f.Path,
			Tier:        f.Tier,
			Reasons:     f.Reasons,
			Locator:     f.Locator,
			Fingerprint: f.Fingerprint(),
		}
		if f.Context != "" {
			c := string(f.Context)
			jf.Context = &c
		}
		if f.Column != nil {
			c := f.Column.String()
			jf.Column = &c
		}
		out.Findings = append(out.Findings, jf)
	}
	return enc.Encode(out)
}
