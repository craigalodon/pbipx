package corpus

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// baselineFile is where pbipx reads its baseline (ADR-0004).
const baselineFile = ".pbipx-baseline.toml"

// pbipxBaseline is pbipx's baseline format (corpus.md, "Baseline cases").
type pbipxBaseline struct {
	Version int                           `toml:"version"`
	Entries map[string]pbipxBaselineEntry `toml:"entries"`
}

type pbipxBaselineEntry struct {
	Severity string   `toml:"severity"`
	Expires  string   `toml:"expires"`
	Path     string   `toml:"path"`
	Context  string   `toml:"context"`
	Column   *string  `toml:"column,omitempty"`
	Reasons  []string `toml:"reasons"`
}

// resolveArgs is the setup scan: every file, with no baseline.
var resolveArgs = []string{commandScan, "--all", "--no-baseline", "--format", "json"}

// resolve runs the setup of a case with [[baseline]] or [fingerprint]: it
// scans the root with binary and no baseline, matches each description to
// exactly one finding, writes the baseline, and records the fingerprint
// [fingerprint] resolves to.
//
// The scan runs on a committed copy of the root, so that it needs no change
// to the root itself, which is committed (or not) only after after/tree/ is
// applied.
func (c *Corpus) resolve(cs *Case, binary string, p *Prepared) error {
	if binary == "" {
		return errors.New("resolving findings needs the binary")
	}
	scratch, err := os.MkdirTemp("", "pbipx-corpus-resolve-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	if err := copyTree(p.Root, scratch); err != nil {
		return fmt.Errorf("copying the root for the setup scan: %w", err)
	}
	if err := GitSetup(scratch, nil, nil); err != nil {
		return fmt.Errorf("setup scan: %w", err)
	}
	out, err := runBinary(binary, scratch, resolveArgs, CommandEnv(os.Environ(), &Spec{}), nil)
	if err != nil {
		return fmt.Errorf("setup scan: %w", err)
	}
	res, problems := ParseResult(out.Stdout, commandScan, c.Vocab, false)
	if len(problems) > 0 {
		return fmt.Errorf("setup scan (exit %d): stdout is not a JSON result: %s", out.Exit, strings.Join(problems, "; "))
	}
	if res.Failure != "" {
		return fmt.Errorf("setup scan failed: %s", describeClass(res.Failure))
	}

	if len(cs.Spec.Baseline) > 0 {
		data, err := buildBaseline(cs.Spec.Baseline, res.Findings)
		if err != nil {
			return err
		}
		if err := writeIn(p.Root, baselineFile, data); err != nil {
			return fmt.Errorf("writing the baseline: %w", err)
		}
	}
	if cs.Spec.Fingerprint != nil {
		fp, err := matchRef(res.Findings, *cs.Spec.Fingerprint, "fingerprint")
		if err != nil {
			return err
		}
		p.Fingerprint = fp
	}
	return nil
}

// buildBaseline resolves each entry to a finding and encodes the baseline
// in pbipx's format.
func buildBaseline(entries []BaselineEntry, findings []Finding) ([]byte, error) {
	b := pbipxBaseline{Version: 1, Entries: map[string]pbipxBaselineEntry{}}
	for i := range entries {
		e := &entries[i]
		fp, err := matchRef(findings, e.Ref(), fmt.Sprintf("baseline[%d]", i))
		if err != nil {
			return nil, err
		}
		if _, dup := b.Entries[fp]; dup {
			return nil, fmt.Errorf("baseline[%d] resolves to the same finding as an earlier entry", i)
		}
		b.Entries[fp] = pbipxBaselineEntry{
			Severity: e.Severity, Expires: e.Expires,
			Path: e.Path, Context: e.Context, Column: e.Column, Reasons: e.Reasons,
		}
	}
	data, err := toml.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("encoding the baseline: %w", err)
	}
	return data, nil
}

// matchRef returns the fingerprint of the one finding that ref describes.
func matchRef(findings []Finding, ref FindingRef, what string) (string, error) {
	var matched []Finding
	for _, f := range findings {
		if refMatches(ref, f) {
			matched = append(matched, f)
		}
	}
	if len(matched) != 1 {
		return "", fmt.Errorf("%s matches %d findings of the setup scan, want exactly 1 (an error in the case, or a scan that misses the finding)", what, len(matched))
	}
	if matched[0].Fingerprint == "" {
		return "", fmt.Errorf("%s: the setup scan's finding has no fingerprint", what)
	}
	return matched[0].Fingerprint, nil
}

// refMatches reports whether a finding is the one ref describes, by path,
// context, column and reasons.
func refMatches(ref FindingRef, f Finding) bool {
	return f.Path == ref.Path &&
		f.Context != nil && *f.Context == ref.Context &&
		compareNullable(f.Column, ref.Column) == 0 &&
		slices.Equal(f.Reasons, ref.Reasons)
}
