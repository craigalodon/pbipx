package gate

import (
	"slices"
	"strings"
)

// nameListLong are the long options, by program, that read the names of files
// from a file or the input (--files0-from, tar -T, rsync --files-from): the
// gate does not read the list, so the names it holds are words it cannot
// see.
var nameListLong = map[string][]string{
	"tar": {"files-from", "verbatim-files-from"}, "bsdtar": {"files-from"}, "gtar": {"files-from", "verbatim-files-from"},
	"rsync": {"files-from"}, "wc": {"files0-from"}, "sort": {"files0-from"}, "du": {"files0-from"},
}

// readsNameList reports whether v reads file names from a list it is given:
// tar -T and --files-from, --files0-from, rsync --files-from, cpio, zip -@
// and the @file of 7z. It then touches every working copy, the same as a
// runner's input does. grep -f reads patterns, not names, and is not here.
func readsNameList(v view) bool {
	switch v.name {
	case "cpio", "pax":
		return true
	case "zip":
		return slices.Contains(v.rest, "-@") || slices.ContainsFunc(v.rest, func(a string) bool {
			return len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.Contains(a[1:], "@")
		})
	case "7z", "7za", "7zr", "rar":
		return slices.ContainsFunc(v.rest, func(a string) bool { return strings.HasPrefix(a, "@") })
	}
	longs, ok := nameListLong[v.name]
	if !ok {
		return false
	}
	for k, a := range v.rest {
		switch {
		case strings.HasPrefix(a, "--"):
			name, _, _ := strings.Cut(a[2:], "=")
			for _, l := range longs {
				if len(name) >= 3 && name != "file" && strings.HasPrefix(l, name) {
					return true
				}
			}
		case v.name == "tar" || v.name == "bsdtar" || v.name == "gtar":
			// -T, in a cluster, or in the bundle that starts tar's words.
			switch {
			case strings.HasPrefix(a, "-"):
				// The letters to the first that takes a value: the rest of
				// the word is that value.
				for _, c := range a[1:] {
					if c == 'T' {
						return true
					}
					if strings.ContainsRune(tarValue, c) {
						break
					}
				}
			case k == 0 && strings.Contains(a, "T"):
				return true
			}
		}
	}
	return false
}
