package scan

import (
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/pbir"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/sqlfile"
	"github.com/craigalodon/pbipx/internal/yamlfile"
)

// Scanners is the scanner for each file kind. A kind that is missing here is
// still classified as scanned, and each of its files is an unvettable error
// until its scanner is added (principle 8).
//
// Missing: model.
func Scanners() map[policy.Kind]judge.Scanner {
	return map[policy.Kind]judge.Scanner{
		policy.Report: pbir.Scan,
		policy.SQL:    sqlfile.Scan,
		policy.YAML:   yamlfile.Scan,
		policy.Prose:  Prose,
	}
}
