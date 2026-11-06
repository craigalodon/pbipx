package gate

import "strings"

// Searches that print only the names of the files that match (rg -l, rg
// --files, grep -rl), as producers of names for a runner. They are confined
// by an option table alone: an option not in it, one that follows links,
// preprocesses a file or runs anything, makes the producer not confined.

// nameSearch is the options of a search that prints names. flags are the
// single letters with no value, values those with one (attached or next
// word); long maps a long option to whether it takes a value (1, as the
// next word or after "=") or only after "=" (2), or none (0). needs lists
// the options the search requires: each entry is the letters or long names
// any one of which satisfies it. filesOpt is the long option that lists files
// with no pattern (rg --files).
type nameSearch struct {
	flags, values string
	long          map[string]int
	needs         [][]string
	filesOpt      string
	patternLong   []string
}

var nameSearches = map[string]nameSearch{
	"rg": {
		flags: "iFwSl0", values: "gte",
		long: map[string]int{
			"hidden": 0, "files": 0, "files-with-matches": 0, "ignore-case": 0, "fixed-strings": 0,
			"word-regexp": 0, "smart-case": 0, "null": 0, "glob": 1, "type": 1, "regexp": 1,
		},
		needs:       [][]string{{"l", "files-with-matches", "files"}},
		filesOpt:    "files",
		patternLong: []string{"regexp"},
	},
	"grep": {
		flags: "rliFEw", values: "e",
		long: map[string]int{
			"recursive": 0, "files-with-matches": 0, "ignore-case": 0, "fixed-strings": 0, "extended-regexp": 0,
			"word-regexp": 0, "regexp": 1, "include": 2, "exclude": 2, "exclude-dir": 2,
		},
		needs:       [][]string{{"l", "files-with-matches"}, {"r", "recursive"}},
		patternLong: []string{"regexp"},
	},
}

// searchOperands reads the words of a name search v by its table: the
// operands that are paths, and ok when every option is in the table, every
// word is known and the options it needs are there. With no path the working
// directory is searched, which the caller places.
func (s *setup) searchOperands(v view, spec nameSearch) (paths []xword, ok bool) {
	words := s.restWords(v)
	got := map[string]bool{}
	pat, files := false, false
	var operands []xword
	for i := 0; i < len(words); i++ {
		xw := words[i]
		a := xw.text
		if xw.redirect || !xw.known {
			return nil, false
		}
		switch {
		case a == "--":
			operands = append(operands, words[i+1:]...)
			i = len(words)
		case strings.HasPrefix(a, "--"):
			name, _, hasValue := strings.Cut(a[2:], "=")
			kind, found := spec.long[name]
			if !found || kind == 0 && hasValue || kind == 2 && !hasValue {
				return nil, false
			}
			if kind == 1 && !hasValue {
				i++
				if i >= len(words) || !words[i].known {
					return nil, false
				}
			}
			got[name] = true
			for _, p := range spec.patternLong {
				pat = pat || name == p
			}
			files = files || name == spec.filesOpt
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c := a[j]
				switch {
				case strings.IndexByte(spec.values, c) >= 0:
					if j+1 == len(a) {
						i++
						if i >= len(words) || !words[i].known {
							return nil, false
						}
					}
					pat = pat || c == 'e'
					j = len(a)
				case strings.IndexByte(spec.flags, c) >= 0:
					got[string(c)] = true
				default:
					return nil, false
				}
			}
		default:
			operands = append(operands, xw)
		}
	}
	for _, need := range spec.needs {
		hit := false
		for _, n := range need {
			hit = hit || got[n]
		}
		if !hit {
			return nil, false
		}
	}
	if !pat && !files {
		// The first operand is the pattern.
		if len(operands) == 0 {
			return nil, false
		}
		operands = operands[1:]
	}
	return operands, true
}
