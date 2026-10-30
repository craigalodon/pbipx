package gate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
)

// content is one file's bytes, read once, as a scan.Reader.
type content []byte

func (c content) Read(string) ([]byte, error) { return c, nil }

// read decides a Read of p (behavior.md, "The gate", Read).
func (s *setup) read(p string) Decision {
	if p == "" {
		return allow()
	}
	p = expandHome(p, s.home)
	// The file the operating system opens: ".." after a symlink is
	// applied to where the link points.
	a := rawJoin(s.cwd, p)
	info, statErr := os.Stat(a)
	if s.fold && errors.Is(statErr, fs.ErrNotExist) {
		// A file system that folds case opens the file as spelled on disk.
		for _, rel := range s.rels(p) {
			d := filepath.Join(s.repo.Root, filepath.FromSlash(s.diskCase(rel)))
			if i, err := os.Stat(d); err == nil {
				a, info, statErr = d, i, nil
				break
			}
		}
	}
	if statErr == nil && info.IsDir() {
		return allow()
	}
	// Each spelling is judged as the repository sees it and as every
	// nested work tree it is in sees it, under each policy there.
	var scanned []string
	for _, rel := range s.rels(p) {
		for _, f := range s.frames(rel) {
			for _, pol := range f.policies {
				for _, sub := range s.subSpellings(pol, f.sub) {
					class, _ := pol.Classify(sub)
					switch class {
					case policy.LocalState:
						return deny(ReasonLocalState, "%s is Power BI Desktop local state, which is never read", displayPath(rel))
					case policy.Unvettable:
						return deny(ReasonUnvettable, "%s is under a protected root and no file class covers it, so it cannot be vetted%s",
							displayPath(rel), hint(rel))
					case policy.Scanned:
						if !slices.Contains(scanned, rel) {
							scanned = append(scanned, rel)
						}
					default: // outside, or allowed unscanned
					}
				}
			}
		}
	}
	if len(scanned) == 0 {
		return allow()
	}
	switch {
	case errors.Is(statErr, fs.ErrNotExist):
		return allow()
	case statErr != nil:
		return deny(ReasonScanError, "%s cannot be read, so it was not vetted", displayPath(scanned[0]))
	case !info.Mode().IsRegular():
		return deny(ReasonScanError, "%s is not a regular file, so it was not vetted", displayPath(scanned[0]))
	}
	data, err := os.ReadFile(a) //nolint:gosec // G304: the path is the one the agent is about to read
	if err != nil {
		return deny(ReasonScanError, "%s cannot be read, so it was not vetted", displayPath(scanned[0]))
	}
	for _, rel := range scanned {
		if d := verdict(s.scanFrames([]string{rel}, content(data)), "reading "); d != nil {
			return *d
		}
	}
	return allow()
}

// hint is what to do about an unvettable file, when there is something.
func hint(rel string) string {
	if strings.HasSuffix(strings.ToLower(rel), ".bim") {
		return "; a model saved as TMSL (.bim) cannot be scanned: save it as TMDL"
	}
	return ""
}

// search decides a search of p, the working directory when empty
// (behavior.md, "The gate", Grep).
func (s *setup) search(p string) Decision {
	if p == "" {
		p = s.cwd
	}
	if !s.touches(expandHome(p, s.home)) {
		return allow()
	}
	return s.tree("a search over a protected root")
}

// tree scans every file on disk under the protected roots, and under each
// nested work tree's roots, local state skipped and symlinks not followed,
// for a call described by what: any block or error denies.
func (s *setup) tree(what string) Decision {
	set, err := s.repo.Under(s.treeDirs())
	if err != nil {
		return deny(ReasonScanError, "%s: the protected roots could not be listed, so they were not vetted", what)
	}
	defer func() { _ = set.Close() }()
	if d := verdict(s.scanFrames(set.Paths, set), what+" could reach "); d != nil {
		return *d
	}
	return allow()
}

// verdict is the deny a scan result calls for, or nil when it has neither a
// block nor an error. A block outranks an error (behavior.md,
// Clarifications, "Gate"). An error is unvettable when the file could not
// be classified and scan-error otherwise. The message names the first such
// file, prefixed with lead, and counts the rest.
func verdict(res finding.Result, lead string) *Decision {
	var blocks, errs []finding.Finding
	for _, f := range res.Findings {
		switch f.Tier {
		case finding.Block:
			blocks = append(blocks, f)
		case finding.Error:
			errs = append(errs, f)
		default: // a warning does not deny
		}
	}
	switch {
	case len(blocks) > 0:
		d := deny(ReasonFinding, "%s%s, which has a block finding (%s)%s; pbipx scan lists every finding, without values",
			lead, displayPath(blocks[0].Path), describeFinding(blocks[0]), more(blocks, errs))
		return &d
	case len(errs) > 0:
		reason := ReasonScanError
		if slices.Contains(errs[0].Reasons, finding.Unvettable) {
			reason = ReasonUnvettable
		}
		d := deny(reason, "%s%s, which could not be vetted (%s)%s; pbipx scan lists every finding",
			lead, displayPath(errs[0].Path), describeFinding(errs[0]), more(blocks, errs))
		return &d
	}
	return nil
}

// describeFinding is a finding's structure: its context, column, reasons
// and, for an error, where the scanner stopped. Never its value.
func describeFinding(f finding.Finding) string {
	var parts []string
	if f.Context != "" {
		parts = append(parts, "context "+string(f.Context))
	}
	if f.Column != nil {
		parts = append(parts, "column "+f.Column.String())
	}
	reasons := make([]string, len(f.Reasons))
	for i, r := range f.Reasons {
		reasons[i] = string(r)
	}
	parts = append(parts, "reasons "+strings.Join(reasons, ", "))
	if f.Tier == finding.Error && f.Locator != "" {
		parts = append(parts, "at "+f.Locator)
	}
	return strings.Join(parts, "; ")
}

// more counts the findings a message does not name.
func more(blocks, errs []finding.Finding) string {
	n := len(blocks) + len(errs) - 1
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return ", and 1 more finding"
	}
	return fmt.Sprintf(", and %d more findings", n)
}
