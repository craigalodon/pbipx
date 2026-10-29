package scan

import (
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/policy"
)

// Scanners is the scanner for each file kind. A kind that is missing here is
// still classified as scanned, and each of its files is an unvettable error
// until its scanner is added (principle 8).
//
// Missing: report (internal/pbir), model, sql, yaml.
func Scanners() map[policy.Kind]judge.Scanner {
	return map[policy.Kind]judge.Scanner{
		policy.Prose: Prose,
	}
}
