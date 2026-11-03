package gate

import (
	"path/filepath"
)

// expandShellGlobs is v with the unquoted glob words of its command line
// replaced by the words bash would produce from them in its working
// directory, for the rules that read git's arguments: p?ip is pbip where
// that is there, and the pathspec git sees. A pattern that matches nothing
// stays as written (bash passes it on), a word whose expansion cannot be
// done (the working directory or the word cannot be told, or the glob
// budget runs out) becomes an unknown word, and a quoted glob is left to
// git, which reads it as a pathspec glob. Only git's words are expanded;
// the other rules place globs themselves.
func (s *setup) expandShellGlobs(v view) view {
	if v.name != "git" {
		return v
	}
	if inv, ok := parseGit(v.rest, ""); !ok || gitMessageSubs[inv.sub] {
		// Their words are messages and names, judged as written: a glob in
		// a message word is its own rule.
		return v
	}
	hasGlob := false
	for _, w := range v.words {
		if !w.redirect && w.glob {
			hasGlob = true
		}
	}
	if !hasGlob {
		return v
	}
	var nonRedirect []int
	for wi, w := range v.words {
		if !w.redirect {
			nonRedirect = append(nonRedirect, wi)
		}
	}
	first := len(nonRedirect) - len(v.argv)
	cache := v.cache
	if cache == nil {
		cache = newSpellCache()
	}
	var argv []string
	var known []bool
	var shapes []argShape
	for p, text := range v.argv {
		xw := v.words[nonRedirect[first+p]]
		shape := v.argvShapes[p]
		if !xw.glob || !xw.known {
			argv, known, shapes = append(argv, text), append(known, v.argvKnown[p]), append(shapes, shape)
			continue
		}
		if !v.cwd.known() {
			argv, known, shapes = append(argv, text), append(known, false), append(shapes, shape)
			continue
		}
		dir := v.cwd.dirs[0]
		pattern := xw.pattern
		if !fullPath(pattern) {
			pattern = rawJoin(dir, pattern)
		}
		matches, over := globMatches(pattern, cache)
		switch {
		case over:
			argv, known, shapes = append(argv, text), append(known, false), append(shapes, shape)
		case len(matches) == 0:
			argv, known, shapes = append(argv, text), append(known, true), append(shapes, shape)
		default:
			for _, m := range matches {
				if !fullPath(xw.pattern) {
					if rel, err := filepath.Rel(dir, m); err == nil {
						m = filepath.ToSlash(rel)
					}
				}
				argv, known, shapes = append(argv, m), append(known, true), append(shapes, argShape{})
			}
		}
	}
	v.argv, v.argvKnown, v.argvShapes = argv, known, shapes
	v.name, v.rest, v.under = command(v.argv)
	v.restKnown = v.argvKnown[len(v.argvKnown)-len(v.rest):]
	return v
}
