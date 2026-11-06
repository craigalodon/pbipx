package gate

import "strings"

// Option tables of the programs that write into a destination: cp, mv, ln,
// install, rsync and tee. The gate reads a command's words with these to tell
// a destination from a source by position. An option in none of the tables
// is not understood, and then the command's words are not placed: each
// is taken to be both (copyArgs).
//
// Left out on purpose, so a command that has one is not understood: mv
// --exchange (it swaps the two operands, so every one is a source), cp -b,
// --backup and --remove-destination (they rename or remove what is at the
// destination), and rsync's --link-dest, --compare-dest, --copy-dest and
// --backup-dir (a third path read or linked). mv --backup and rsync --backup
// are in: they set a name aside only for a file the command replaces.

// optSpec lists a program's options. short are the single letters that take
// no value, value the letters that take one (attached or as the next
// word), and long the long options with whether they take a value: 0 none,
// 1 one, as the next word or after "=", 2 one only after "=". target is the
// letter and the long name of the option that names the destination
// directory, dirs the letter after which every operand is a directory to
// make (install -d).
type optSpec struct {
	short, value string
	long         map[string]int
	target       byte
	targetLong   string
	dirs         byte
	// allDest is set for a program that writes every operand (tee).
	allDest bool
}

// copyWord is a word of a command, by its index among the words after the
// program name, of which the first skip bytes are an option's name (-t, or
// --target-directory=) and the rest the path.
type copyWord struct{ idx, skip int }

var copyOptSpecs = map[string]optSpec{
	"cp": {
		short: "adfHiLlnPpRrsTuvxZcg",
		value: "St",
		long: map[string]int{
			"archive": 0, "attributes-only": 0, "copy-contents": 0, "debug": 0, "dereference": 0,
			"force": 0, "interactive": 0, "link": 0, "no-clobber": 0, "no-dereference": 0, "no-preserve": 1,
			"no-target-directory": 0, "one-file-system": 0, "parents": 0, "preserve": 2, "recursive": 0,
			"reflink": 2, "sparse": 1, "strip-trailing-slashes": 0, "suffix": 1,
			"symbolic-link": 0, "target-directory": 1, "update": 2, "verbose": 0, "context": 2, "help": 0,
			"version": 0, "keep-directory-symlink": 0,
		},
		target: 't', targetLong: "target-directory",
	},
	"mv": {
		short: "bfinTuvZ",
		value: "St",
		long: map[string]int{
			"backup": 2, "debug": 0, "force": 0, "interactive": 0, "no-clobber": 0, "no-copy": 0,
			"no-target-directory": 0, "strip-trailing-slashes": 0, "suffix": 1, "target-directory": 1,
			"update": 2, "verbose": 0, "context": 0, "help": 0, "version": 0,
		},
		target: 't', targetLong: "target-directory",
	},
	"ln": {
		short: "bfiLnPrsTvFw",
		value: "St",
		long: map[string]int{
			"backup": 2, "directory": 0, "force": 0, "interactive": 0, "logical": 0, "no-dereference": 0,
			"physical": 0, "relative": 0, "symbolic": 0, "suffix": 1, "target-directory": 1,
			"no-target-directory": 0, "verbose": 0, "help": 0, "version": 0,
		},
		target: 't', targetLong: "target-directory",
	},
	"install": {
		short: "bcCDpsTvd",
		value: "gmoSt",
		long: map[string]int{
			"backup": 2, "compare": 0, "directory": 0, "group": 1, "mode": 1, "owner": 1, "preserve-timestamps": 0,
			"strip": 0, "strip-program": 1, "suffix": 1, "target-directory": 1, "no-target-directory": 0,
			"verbose": 0, "context": 2, "help": 0, "version": 0, "debug": 0, "unprivileged": 0,
		},
		target: 't', targetLong: "target-directory", dirs: 'd',
	},
	"rsync": {
		short: "avrlptgoDzunchiqPRxSWLkKbdHyOJ",
		value: "e",
		long: map[string]int{
			"archive": 0, "verbose": 0, "recursive": 0, "links": 0, "perms": 0, "times": 0, "group": 0, "owner": 0,
			"devices": 0, "specials": 0, "compress": 0, "update": 0, "dry-run": 0, "checksum": 0,
			"human-readable": 0, "itemize-changes": 0, "quiet": 0, "progress": 0, "partial": 0, "relative": 0,
			"one-file-system": 0, "delete": 0, "delete-after": 0, "delete-before": 0, "delete-during": 0,
			"delete-excluded": 0, "exclude": 1, "include": 1, "rsh": 1, "inplace": 0, "whole-file": 0,
			"numeric-ids": 0, "stats": 0, "copy-links": 0, "hard-links": 0, "safe-links": 0, "no-perms": 0,
			"size-only": 0, "ignore-existing": 0, "backup": 0, "mkpath": 0,
		},
	},
	"tee": {
		short: "aip",
		long:  map[string]int{"append": 0, "ignore-interrupts": 0, "output-error": 2, "help": 0, "version": 0},

		allDest: true,
	},
}

// copyArgs reads the words after the program name of a cp, mv, ln,
// install, rsync or tee command, by their positions in words: the
// destinations (the target directory option's value, the last operand, or
// every operand of tee and install -d) and the sources. ok is false when an
// option is not understood, or a word cannot be told and might be an option: the
// caller then takes no word of the command as a destination.
func copyArgs(name string, words []xword) (dests []copyWord, sources []int, ok bool) {
	spec, found := copyOptSpecs[name]
	if !found {
		return nil, nil, false
	}
	var pos []int
	dirsMode := false
	for i := 0; i < len(words); i++ {
		a := words[i].text
		if !words[i].known {
			return nil, nil, false
		}
		switch {
		case a == "--":
			for j := i + 1; j < len(words); j++ {
				if !words[j].known {
					return nil, nil, false
				}
				pos = append(pos, j)
			}
			i = len(words)
		case strings.HasPrefix(a, "--"):
			opt, _, hasValue := strings.Cut(a[2:], "=")
			full, found := expandCopyLong(spec, opt)
			if !found {
				return nil, nil, false
			}
			kind := spec.long[full]
			switch {
			case kind == 0 && hasValue:
				return nil, nil, false
			case full == spec.targetLong && spec.targetLong != "":
				switch {
				case hasValue:
					dests = append(dests, copyWord{i, strings.IndexByte(a, '=') + 1})
				case i+1 < len(words):
					dests = append(dests, copyWord{i + 1, 0})
					i++
				default:
					return nil, nil, false
				}
			case kind == 1 && !hasValue:
				if i+1 >= len(words) {
					return nil, nil, false
				}
				i++
			case full == "directory" && name == "install":
				dirsMode = true
			}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c := a[j]
				switch {
				case c == spec.target && c != 0:
					switch {
					case j+1 < len(a):
						dests = append(dests, copyWord{i, j + 1}) // attached
					case i+1 < len(words):
						dests = append(dests, copyWord{i + 1, 0})
						i++
					default:
						return nil, nil, false
					}
					j = len(a)
				case strings.IndexByte(spec.value, c) >= 0:
					if j+1 == len(a) {
						if i+1 >= len(words) {
							return nil, nil, false
						}
						i++
					}
					j = len(a)
				case strings.IndexByte(spec.short, c) >= 0:
					if c == spec.dirs && c != 0 {
						dirsMode = true
					}
				default:
					return nil, nil, false
				}
			}
		default:
			pos = append(pos, i)
		}
	}
	switch {
	case spec.allDest || dirsMode:
		for _, p := range pos {
			dests = append(dests, copyWord{p, 0})
		}
	case len(dests) > 0:
		sources = pos
	case len(pos) >= 2:
		dests = append(dests, copyWord{pos[len(pos)-1], 0})
		sources = pos[:len(pos)-1]
	default:
		sources = pos
	}
	return dests, sources, true
}

// expandCopyLong is the long option opt names in spec: exactly, or by the
// one option it abbreviates. found is false for none or for several.
func expandCopyLong(spec optSpec, opt string) (string, bool) {
	if _, ok := spec.long[opt]; ok {
		return opt, true
	}
	match := ""
	for name := range spec.long {
		if strings.HasPrefix(name, opt) {
			if match != "" {
				return "", false
			}
			match = name
		}
	}
	return match, match != "" && opt != ""
}
