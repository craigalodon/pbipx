package gate

import (
	"slices"
	"strings"
)

// A lister prints the names of files and their metadata, never what they
// hold: names are schema (principle 1), so a listing over a protected root
// is allowed whatever the tree holds. ls, stat and du (but not du reading
// names from a file), tree (but not tree reading a listing from a file or
// writing one), and find whose expression only tests names and metadata
// and prints names. file is not one: it reads content to tell a file's
// type. A listing piped into a reader is judged by the reader.

// lister reports whether the program name, run with the arguments rest,
// only lists names.
func lister(name string, rest []string) bool {
	switch name {
	case "ls", "stat", "dir", "vdir":
		return true
	case "du":
		return !anyArg(rest, func(a string) bool {
			return strings.HasPrefix(a, "--files0-from") || strings.HasPrefix(a, "--exclude-from") ||
				shortCluster(a, "X")
		})
	case "tree":
		// --fromfile prints a listing read from a file; -o writes the
		// output to a file, and -R (with -H) writes a page in each
		// directory.
		return !anyArg(rest, func(a string) bool {
			return strings.HasPrefix(a, "--fromfile") || shortCluster(a, "oR")
		})
	case "find":
		return findLists(rest)
	}
	return false
}

// namesRootOutsideListers reports whether line names root as a whole word
// (namesWord) anywhere but in the words of a command that only lists names:
// in another command's words, in a command substitution or in a
// here-document. A line that did not tokenize is read whole.
func namesRootOutsideListers(line, root string) bool {
	if !namesWord(line, root) {
		return false
	}
	for _, c := range splitCommands(line) {
		if c.fallback {
			return true
		}
		for _, sub := range c.subs {
			if namesRootOutsideListers(sub, root) {
				return true
			}
		}
		for _, doc := range c.docs {
			if namesWord(doc.body, root) {
				return true
			}
		}
		if name, rest := program(c.args()); lister(name, rest) {
			continue
		}
		for _, wd := range c.words {
			if namesWord(withoutSubs(wd), root) {
				return true
			}
		}
	}
	return false
}

// withoutSubs is a word's text with its command substitutions, which are
// read as lines of their own, left out.
func withoutSubs(wd word) string {
	var b strings.Builder
	for _, p := range wd.parts {
		switch {
		case p.kind == opaquePart && (strings.HasPrefix(p.raw, "$(") || strings.HasPrefix(p.raw, "`")):
			b.WriteByte(' ')
		case p.kind == litPart:
			b.WriteString(p.text)
		default:
			b.WriteString(p.raw)
		}
	}
	return b.String()
}

// anyArg reports whether f holds for any argument before "--".
func anyArg(rest []string, f func(string) bool) bool {
	for _, a := range rest {
		if a == "--" {
			return false
		}
		if f(a) {
			return true
		}
	}
	return false
}

// shortCluster reports whether a is a cluster of short options holding one
// of chars.
func shortCluster(a, chars string) bool {
	return len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a[1:], chars)
}

// findPrimaries are find's options, tests and actions that only look at
// names and metadata and print names, with the number of arguments each
// takes.
var findPrimaries = map[string]int{
	"-name": 1, "-iname": 1, "-path": 1, "-ipath": 1, "-wholename": 1, "-iwholename": 1,
	"-lname": 1, "-ilname": 1, "-regex": 1, "-iregex": 1, "-regextype": 1, "-type": 1,
	"-xtype": 1, "-maxdepth": 1, "-mindepth": 1, "-mtime": 1, "-mmin": 1, "-atime": 1,
	"-amin": 1, "-ctime": 1, "-cmin": 1, "-newer": 1, "-anewer": 1, "-cnewer": 1,
	"-size": 1, "-perm": 1, "-user": 1, "-group": 1, "-uid": 1, "-gid": 1, "-links": 1,
	"-inum": 1, "-samefile": 1, "-fstype": 1, "-used": 1,
	"-empty": 0, "-readable": 0, "-writable": 0, "-executable": 0, "-depth": 0, "-d": 0,
	"-prune": 0, "-print": 0, "-print0": 0, "-ls": 0, "-not": 0, "-a": 0, "-o": 0,
	"-and": 0, "-or": 0, "-true": 0, "-false": 0, "-follow": 0, "-xdev": 0, "-mount": 0,
	"-noleaf": 0, "-daystart": 0, "-nouser": 0, "-nogroup": 0, "-quit": 0,
	"!": 0, "(": 0, ")": 0, ",": 0,
	"-printf": 1,
}

// findLists reports whether find, run with rest, only lists names: its
// paths are followed only by primaries in findPrimaries (or -newerXY), and
// each -printf format holds only directives for names and metadata.
func findLists(rest []string) bool {
	i := 0
	for i < len(rest) {
		a := rest[i]
		if a == "-H" || a == "-L" || a == "-P" || strings.HasPrefix(a, "-O") {
			i++
			continue
		}
		if a == "-D" {
			i += 2
			continue
		}
		break
	}
	for i < len(rest) && !strings.HasPrefix(rest[i], "-") && !slices.Contains([]string{"(", ")", "!", ","}, rest[i]) {
		i++
	}
	for ; i < len(rest); i++ {
		a := rest[i]
		n, ok := findPrimaries[a]
		if !ok && strings.HasPrefix(a, "-newer") && len(a) == len("-newerXY") {
			n, ok = 1, true
		}
		if !ok || i+n >= len(rest) {
			return false
		}
		if a == "-printf" && !printfLists(rest[i+1]) {
			return false
		}
		i += n
	}
	return true
}

// printfLists reports whether a find -printf format holds only directives
// for names and metadata, and escapes.
func printfLists(format string) bool {
	for i := 0; i < len(format); i++ {
		switch format[i] {
		case '%':
			i++
			for i < len(format) && strings.IndexByte("-+# .0123456789", format[i]) >= 0 {
				i++
			}
			if i >= len(format) {
				return false
			}
			c := format[i]
			if strings.IndexByte("aAcCtT", c) >= 0 {
				// A time, with its format letter after A, C and T.
				if c == 'A' || c == 'C' || c == 'T' {
					i++
					if i >= len(format) {
						return false
					}
				}
				continue
			}
			if strings.IndexByte("%pfhPHsSkbmMuUgGyYdDinlF", c) < 0 {
				return false
			}
		case '\\':
			i++
			if i >= len(format) || strings.IndexByte(`ntr0\abfv`, format[i]) < 0 {
				return false
			}
		}
	}
	return true
}
