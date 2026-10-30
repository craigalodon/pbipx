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
						return deny(ReasonLocalState, "%s is Power BI Desktop local state, which is never read. %s", displayPath(rel), hintLocal)
					case policy.Unvettable:
						return deny(ReasonUnvettable, "%s is under a protected root and no file class covers it, so it cannot be vetted. %s",
							displayPath(rel), vetHint(rel))
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
		return deny(ReasonScanError, "%s cannot be read, so it was not vetted. %s", displayPath(scanned[0]), hintUnread)
	case !info.Mode().IsRegular():
		return deny(ReasonScanError, "%s is not a regular file, so it was not vetted. Read a regular file instead.", displayPath(scanned[0]))
	}
	data, err := os.ReadFile(a) //nolint:gosec // G304: the path is the one the agent is about to read
	if err != nil {
		return deny(ReasonScanError, "%s cannot be read, so it was not vetted. %s", displayPath(scanned[0]), hintUnread)
	}
	for _, rel := range scanned {
		if d := verdict(s.scanFrames([]string{rel}, content(data)), trigger{}); d != nil {
			return *d
		}
	}
	return allow()
}

// vetHint is what to do about an unvettable file.
func vetHint(rel string) string {
	if strings.HasSuffix(strings.ToLower(rel), ".bim") {
		return "A model saved as TMSL (.bim) cannot be scanned: save it as TMDL."
	}
	return hintVet
}

// findingHint is what to do about a block finding: a selection a report
// keeps is cleared in Power BI Desktop; any other value is removed from
// the file.
func findingHint(f finding.Finding) string {
	switch f.Context {
	case "filter", "slicer-selection", "expansion-state", "insight-selector", "bookmark":
		return hintSelect
	}
	return hintValue
}

// search decides a search of p, the working directory when empty
// (behavior.md, "The gate", Grep).
func (s *setup) search(p string) Decision {
	if p == "" {
		p = s.cwd
	}
	for _, a := range spellings(s.cwd, expandHome(p, s.home)) {
		if s.touchesAbs(a) {
			return s.tree(trigger{"the search covers a path that " + s.whereAbs(a), hintSearch})
		}
	}
	return allow()
}

// tree scans every file on disk under the protected roots, and under each
// nested work tree's roots, local state skipped and symlinks not followed,
// for a call that reaches them because of t: any block or error denies,
// and the message names t's trigger and ends with its hint.
func (s *setup) tree(t trigger) Decision {
	set, err := s.repo.Under(s.treeDirs())
	if err != nil {
		return deny(ReasonScanError, "%s; the protected roots could not be listed, so they were not vetted. %s", t.why, t.hint)
	}
	defer func() { _ = set.Close() }()
	if d := verdict(s.scanFrames(set.Paths, set), t); d != nil {
		return *d
	}
	return allow()
}

// verdict is the deny a scan result calls for, or nil when it has neither a
// block nor an error. A block outranks an error (behavior.md,
// Clarifications, "Gate"). An error is unvettable when the file could not
// be classified and scan-error otherwise. The message names the first such
// file and counts the rest. For a tree scan, t is what made the call reach
// the tree, and its hint ends the message; for a Read, t is zero and the
// file's own hint does.
func verdict(res finding.Result, t trigger) *Decision {
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
	// A tree scan says what reached the file; a Read names the file alone.
	lead, which, hint := "", " ", ""
	if t.set() {
		lead, which, hint = t.why+", so the call could reach ", ", which ", t.hint
	}
	switch {
	case len(blocks) > 0:
		if hint == "" {
			hint = findingHint(blocks[0])
		}
		d := deny(ReasonFinding, "%s%s%shas a block finding (%s)%s. %s",
			lead, displayPath(blocks[0].Path), which, describeFinding(blocks[0]), more(blocks, errs), hint)
		return &d
	case len(errs) > 0:
		reason := ReasonScanError
		if slices.Contains(errs[0].Reasons, finding.Unvettable) {
			reason = ReasonUnvettable
		}
		if hint == "" {
			hint = hintUnread
			if reason == ReasonUnvettable {
				hint = vetHint(errs[0].Path)
			}
		}
		d := deny(reason, "%s%s%scould not be vetted (%s)%s. %s",
			lead, displayPath(errs[0].Path), which, describeFinding(errs[0]), more(blocks, errs), hint)
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
