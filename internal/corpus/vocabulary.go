// Package corpus loads, validates and runs the behavior corpus
// (docs/design/corpus.md).
//
// A case's fixtures hold synthetic values, and its sentinels exist to prove
// that they never reach output. So nothing in this package prints a fixture's
// content: every problem names the case, the file and the rule broken, and
// at most a line number or an index.
package corpus

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

// Vocabulary is the corpus's closed vocabulary (testdata/corpus/vocabulary.toml).
type Vocabulary struct {
	Version     int                    `toml:"version"`
	Contexts    map[string]vocabEntry  `toml:"contexts"`
	Reasons     map[string]vocabReason `toml:"reasons"`
	ReasonOrder struct {
		Order []string `toml:"order"`
	} `toml:"reason-order"`
	Tiers         map[string]string `toml:"tiers"`
	Failures      map[string]string `toml:"failures"`
	GateReasons   map[string]string `toml:"gate-reasons"`
	ExitCodes     map[string]string `toml:"exit-codes"`
	GateExitCodes map[string]string `toml:"gate-exit-codes"`

	// reasonRank is each finding reason's position in ReasonOrder.
	reasonRank map[string]int
}

type vocabEntry struct {
	Area        string `toml:"area"`
	Description string `toml:"description"`
}

type vocabReason struct {
	Kind        string `toml:"kind"`
	Description string `toml:"description"`
}

// Reason kinds.
const (
	reasonFinding = "finding"
	reasonError   = "error"
)

// LoadVocabulary reads and checks a vocabulary file.
func LoadVocabulary(path string) (*Vocabulary, error) {
	data, err := readIn(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("vocabulary: %w", err)
	}
	var v Vocabulary
	dec := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("vocabulary: %s", tomlProblem(err))
	}
	if v.Version != 1 {
		return nil, errors.New("vocabulary: version is not 1")
	}
	v.reasonRank = make(map[string]int, len(v.ReasonOrder.Order))
	for i, r := range v.ReasonOrder.Order {
		if v.Reasons[r].Kind != reasonFinding {
			return nil, fmt.Errorf("vocabulary: reason-order entry %d is not a finding reason", i)
		}
		if _, dup := v.reasonRank[r]; dup {
			return nil, fmt.Errorf("vocabulary: reason-order entry %d is repeated", i)
		}
		v.reasonRank[r] = i
	}
	for name, r := range v.Reasons {
		switch r.Kind {
		case reasonFinding:
			if _, ok := v.reasonRank[name]; !ok {
				return nil, fmt.Errorf("vocabulary: finding reason %q is missing from reason-order", name)
			}
		case reasonError:
		default:
			return nil, fmt.Errorf("vocabulary: reason %q has an unknown kind", name)
		}
	}
	for _, codes := range []map[string]string{v.ExitCodes, v.GateExitCodes} {
		for k := range codes {
			if _, err := strconv.Atoi(k); err != nil {
				return nil, fmt.Errorf("vocabulary: exit code %q is not a number", k)
			}
		}
	}
	return &v, nil
}

// validExit reports whether code is an exit code of the command.
func (v *Vocabulary) validExit(command string, code int) bool {
	codes := v.ExitCodes
	if command == commandGate {
		codes = v.GateExitCodes
	}
	_, ok := codes[strconv.Itoa(code)]
	return ok
}

// tomlProblem describes a TOML decoding error by position only. The library's
// own messages can quote the document, and a case.toml holds sentinels.
func tomlProblem(err error) string {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) && len(strict.Errors) > 0 {
		e := strict.Errors[0]
		row, _ := e.Position()
		return fmt.Sprintf("line %d: unknown key %s", row, keyName(e.Key()))
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, col := de.Position()
		return fmt.Sprintf("line %d, column %d: invalid TOML or a value of the wrong type", row, col)
	}
	return "invalid TOML or a value of the wrong type"
}

// keyName prints a key only when it looks like one, so a value typed where a
// key belongs is not echoed.
func keyName(k toml.Key) string {
	if len(k) == 0 {
		return "(unnamed)"
	}
	name := k[len(k)-1]
	if !plainKey.MatchString(name) {
		return "(not printed)"
	}
	return strconv.Quote(name)
}
