// Package scan runs the scanners over a set of files and applies policy and
// the baseline (docs/design/behavior.md, "Files and the default deny" and
// "Accepting a warning").
//
// Every path is classified by policy first. Under a protected root a file no
// class covers is an error finding, and so is a scanned file whose kind has
// no scanner yet: the deny comes before the capability (principle 8). A file
// that cannot be read, is not UTF-8 or does not parse is one error finding
// and nothing else (principle 10).
package scan

import (
	"bytes"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/craigalodon/pbipx/internal/baseline"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
)

// Reader reads one file of a scan by its repository-relative path.
type Reader interface {
	Read(path string) ([]byte, error)
}

// Options configures Run.
type Options struct {
	Policy *policy.Policy
	// Baseline accepts warnings; nil means none.
	Baseline *baseline.Baseline
	// Scanners maps a kind to its scanner; nil means Scanners().
	Scanners map[policy.Kind]judge.Scanner
	// Now is when baseline expiry is judged; zero means time.Now().
	Now time.Time
	// AgentSettings checks every Claude Code settings file among the paths
	// (IsAgentSettings), whatever the policy's scan scope: one that can
	// turn hooks off is a hooks-disabled error. pbipx scan sets it; the
	// gate's own scans do not.
	AgentSettings bool
}

// Run scans paths, read through r, and returns the result: every finding
// not accepted, with warnings accepted by policy counted in Accepted and
// those accepted by the baseline in Suppressed.
func Run(paths []string, r Reader, opts Options) finding.Result {
	scanners := opts.Scanners
	if scanners == nil {
		scanners = Scanners()
	}
	var findings []finding.Finding
	for _, path := range paths {
		if opts.AgentSettings && IsAgentSettings(path) {
			if f, bad := agentSettings(path, r); bad {
				findings = append(findings, f)
			}
		}
		class, kind := opts.Policy.Classify(path)
		switch class {
		case policy.Unvettable:
			findings = append(findings, errorFinding(path, finding.Unvettable, 0))
			continue
		case policy.Scanned:
		default: // local state, unscanned-allowed, outside
			continue
		}
		scanner, ok := scanners[kind]
		if !ok {
			// A kind with no scanner has not been vetted.
			findings = append(findings, errorFinding(path, finding.Unvettable, 0))
			continue
		}
		data, err := r.Read(path)
		if err != nil {
			findings = append(findings, errorFinding(path, finding.Unreadable, 0))
			continue
		}
		findings = append(findings, File(path, data, scanner, opts.Policy)...)
	}
	return Accept(findings, opts)
}

// Accept applies policy acceptances and then the baseline to findings.
// Only warnings can be accepted.
func Accept(findings []finding.Finding, opts Options) finding.Result {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	var res finding.Result
	for _, f := range findings {
		switch {
		case f.Tier != finding.Warn:
		case opts.Policy.Accepts(f):
			res.Accepted++
			continue
		case opts.Baseline.Accepts(f, now):
			res.Suppressed++
			continue
		}
		res.Findings = append(res.Findings, f)
	}
	finding.Sort(res.Findings)
	return res
}

// bom is the UTF-8 byte order mark, which a file may start with.
var bom = []byte("\xef\xbb\xbf")

// File scans one file's content with scanner. Content that is not UTF-8 is
// a not-utf8 error; a leading byte order mark is accepted and removed. A
// *judge.ParseError is one error finding with its reason. Any other failure
// of the scanner, a panic included, is an unvettable error: it is never
// taken as a clean file, and its message, which could quote the file, is
// not kept.
func File(path string, data []byte, scanner judge.Scanner, p *policy.Policy) (out []finding.Finding) {
	if !utf8.Valid(data) {
		return []finding.Finding{errorFinding(path, finding.NotUTF8, 0)}
	}
	data = bytes.TrimPrefix(data, bom)
	defer func() {
		if recover() != nil {
			out = []finding.Finding{errorFinding(path, finding.Unvettable, 0)}
		}
	}()
	found, err := scanner(path, data, p)
	if err != nil {
		var pe *judge.ParseError
		if errors.As(err, &pe) && isErrorReason(pe.Reason) {
			return []finding.Finding{errorFinding(path, pe.Reason, pe.Line)}
		}
		return []finding.Finding{errorFinding(path, finding.Unvettable, 0)}
	}
	return found
}

func isErrorReason(r finding.Reason) bool {
	for _, e := range finding.ErrorReasons {
		if r == e {
			return true
		}
	}
	return false
}

// agentSettings checks one Claude Code settings file. A file that cannot be
// read is unreadable; one that can turn hooks off is hooks-disabled.
func agentSettings(path string, r Reader) (finding.Finding, bool) {
	data, err := r.Read(path)
	switch {
	case err != nil:
		return errorFinding(path, finding.Unreadable, 0), true
	case HooksDisabled(data):
		return errorFinding(path, finding.HooksDisabled, 0), true
	}
	return finding.Finding{}, false
}

// errorFinding is a file that was not vetted. Its locator is the line the
// scanner stopped at, when it said.
func errorFinding(path string, reason finding.Reason, line int) finding.Finding {
	f := finding.Finding{Path: path, Tier: finding.Error, Reasons: []finding.Reason{reason}}
	if line > 0 {
		f.Locator = fmt.Sprintf("line:%d", line)
	}
	return f
}
