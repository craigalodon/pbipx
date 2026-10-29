package scan

import (
	"fmt"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/shape"
)

// Prose is the scanner for prose files (notes): one block finding per line
// with a shape, in context text (docs/design/behavior.md, "Prose").
func Prose(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	return proseFindings(path, data, p, false), nil
}

// CommitMessage scans a commit message: prose, with lines starting with "#"
// skipped as git skips them.
func CommitMessage(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	return proseFindings(path, data, p, true), nil
}

func proseFindings(path string, data []byte, p *policy.Policy, skipComments bool) []finding.Finding {
	hits := shape.Prose(string(data), shape.ProseOptions{
		Allow:        p.AllowedValues(),
		Switches:     switches(p.Shapes()),
		SkipComments: skipComments,
	})
	out := make([]finding.Finding, 0, len(hits))
	for _, h := range hits {
		out = append(out, finding.Finding{
			Path:    path,
			Context: finding.Text,
			Locator: fmt.Sprintf("line:%d", h.Line),
			Tier:    finding.Block,
			Reasons: h.Reasons,
			Value:   h.Text,
		})
	}
	return out
}

// switches maps the policy's shape switches, all on by default, to shape's,
// all off by default.
func switches(s policy.Shapes) shape.Switches {
	return shape.Switches{
		PersonNameOff: !s.PersonName,
		EmailOff:      !s.Email,
		SSNOff:        !s.SSN,
		DateOff:       !s.Date,
		PhoneOff:      !s.Phone,
	}
}
