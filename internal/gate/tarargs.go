package gate

import "strings"

// tarLong are the long options of tar the gate understands, with whether
// they take a value (1, as the next word or after "=") or not (0). An
// option in none of them is not understood: tar has options that run a
// program (--to-command, --use-compress-program, --checkpoint-action) or
// print what it extracts (--to-stdout).
var tarLong = map[string]int{
	"extract": 0, "get": 0, "create": 0, "list": 0, "append": 0, "update": 0, "catenate": 0, "concatenate": 0,
	"diff": 0, "compare": 0, "delete": 0,
	"verbose": 0, "gzip": 0, "gunzip": 0, "ungzip": 0, "bzip2": 0, "xz": 0, "lzma": 0, "zstd": 0, "compress": 0,
	"uncompress": 0, "auto-compress": 0, "one-file-system": 0, "keep-old-files": 0, "skip-old-files": 0,
	"overwrite": 0, "keep-newer-files": 0, "no-same-owner": 0, "same-owner": 0, "no-same-permissions": 0,
	"preserve-permissions": 0, "same-permissions": 0, "numeric-owner": 0, "wildcards": 0, "no-wildcards": 0,
	"anchored": 0, "no-anchored": 0, "null": 0, "totals": 0, "ignore-failed-read": 0, "dereference": 0,
	"hard-dereference": 0, "recursion": 0, "no-recursion": 0, "remove-files": 0, "show-omitted-dirs": 0,
	"file": 1, "directory": 1, "strip-components": 1, "exclude": 1, "exclude-from": 1, "files-from": 1,
}

// tarModeLong maps a long option that selects an operation to its letter.
var tarModeLong = map[string]byte{
	"extract": 'x', "get": 'x', "create": 'c', "list": 't', "append": 'r', "update": 'u', "catenate": 'A',
	"concatenate": 'A', "diff": 'd', "compare": 'd', "delete": 'D',
}

const (
	tarModes = "xctruAd"                 // the short options that select an operation
	tarShort = "xctruAdvwzjJZapPhkmSlns" // short options with no value
	tarValue = "fCbTXKNgHVL"             // short options with a value
)

// tarArgs reads the words after tar: whether it only extracts (the one
// operation is x), and the indexes in words of the words that name the
// directory it works in (-C, --directory). The first word is read as tar
// reads it, a bundle of letters with no "-" whose values follow in the
// order of the letters; with a "-", a letter that takes a value takes the
// rest of the word, or the next word. ok is false when anything is not
// understood, or a word that could be an option cannot be told: the command
// is then taken to read.
func tarArgs(words []xword) (extract bool, dirs []int, ok bool) {
	modes := map[byte]bool{}
	for i := 0; i < len(words); i++ {
		a := words[i].text
		if !words[i].known {
			return false, nil, false
		}
		switch {
		case a == "--":
			// Members only.
			i = len(words)
		case strings.HasPrefix(a, "--"):
			opt, _, hasValue := strings.Cut(a[2:], "=")
			full := expandTarLong(opt)
			kind, found := tarLong[full]
			if full == "" || !found || kind == 0 && hasValue {
				return false, nil, false
			}
			if m, isMode := tarModeLong[full]; isMode {
				modes[m] = true
			}
			switch {
			case kind == 1 && hasValue:
				if full == "directory" {
					dirs = append(dirs, i)
				}
			case kind == 1:
				if i+1 >= len(words) {
					return false, nil, false
				}
				i++
				if full == "directory" {
					dirs = append(dirs, i)
				}
			}
		case len(a) > 1 && a[0] == '-' || i == 0 && a != "" && a[0] != '-':
			old := a[0] != '-'
			start := 1
			if old {
				start = 0
			}
			for j := start; j < len(a); j++ {
				c := a[j]
				switch {
				case strings.IndexByte(tarValue, c) >= 0:
					if !old && j+1 < len(a) {
						// The rest of the word is the value.
						if c == 'C' {
							dirs = append(dirs, i)
						}
						j = len(a)
						break
					}
					if i+1 >= len(words) {
						return false, nil, false
					}
					i++
					if c == 'C' {
						dirs = append(dirs, i)
					}
				case strings.IndexByte(tarShort, c) >= 0:
					if strings.IndexByte(tarModes, c) >= 0 {
						modes[c] = true
					}
				default:
					return false, nil, false
				}
			}
		}
	}
	return len(modes) == 1 && modes['x'], dirs, true
}

// expandTarLong is the option of tarLong that opt names, exactly or as the
// one it abbreviates; "" for none or several.
func expandTarLong(opt string) string {
	if _, ok := tarLong[opt]; ok {
		return opt
	}
	match := ""
	for name := range tarLong {
		if opt != "" && strings.HasPrefix(name, opt) {
			if match != "" {
				return ""
			}
			match = name
		}
	}
	return match
}
