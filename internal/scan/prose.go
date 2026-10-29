package scan

import (
	"fmt"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/shape"
)

// Prose is the scanner for prose files (notes): one block finding per line
// with a shape, in context text (docs/design/behavior.md, "Prose").
func Prose(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
	return proseFindings(path, data, p, ""), nil
}

// CommitMessage is the scanner for a commit message whose comment
// character (or string) is comment. Every line is scanned, comment lines
// included, because a hook cannot tell which of them git's cleanup will
// keep (`git commit -m` keeps them). Scanning stops at the scissors line,
// which git discards with everything below it.
func CommitMessage(comment string) judge.Scanner {
	scissors := shape.ScissorsLine(comment)
	return func(path string, data []byte, p *policy.Policy) ([]finding.Finding, error) {
		return proseFindings(path, data, p, scissors), nil
	}
}

func proseFindings(path string, data []byte, p *policy.Policy, scissors string) []finding.Finding {
	hits := shape.Prose(string(data), shape.ProseOptions{
		Allow:    p.AllowedValues(),
		Switches: switches(p.Shapes()),
		Scissors: scissors,
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
