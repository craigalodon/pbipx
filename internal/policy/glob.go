package policy

import (
	"errors"
	"path"
	"strings"
)

// A glob matches repository-relative paths with forward slashes (ADR-0004).
// Within one segment, `*` matches any run of characters, `?` one character,
// and `[...]` a class, with path.Match's syntax (`[^...]` negates, `\`
// escapes). A segment that is exactly `**` matches any number of segments,
// none included. Matching is case-sensitive.
type glob struct {
	segs []string
}

// Glob errors. They describe the pattern's structure, never its text.
var (
	errGlobEmpty      = errors.New("is empty")
	errGlobAbsolute   = errors.New("is absolute")
	errGlobEmptySeg   = errors.New("has an empty segment")
	errGlobDoubleStar = errors.New("uses ** inside a segment")
	errGlobSyntax     = errors.New("does not compile")
)

func compileGlob(pattern string) (glob, error) {
	switch {
	case pattern == "":
		return glob{}, errGlobEmpty
	case strings.HasPrefix(pattern, "/"):
		return glob{}, errGlobAbsolute
	}
	var segs []string
	for seg := range strings.SplitSeq(pattern, "/") {
		switch {
		case seg == "":
			return glob{}, errGlobEmptySeg
		case seg == "**":
			// Consecutive ** match what one does.
			if len(segs) > 0 && segs[len(segs)-1] == "**" {
				continue
			}
		case strings.Contains(seg, "**"):
			return glob{}, errGlobDoubleStar
		default:
			// path.Match checks the whole pattern, whatever the name.
			if _, err := path.Match(seg, ""); err != nil {
				return glob{}, errGlobSyntax
			}
		}
		segs = append(segs, seg)
	}
	return glob{segs: segs}, nil
}

// match reports whether g matches the slash-separated path p.
func (g glob) match(p string) bool {
	return matchSegs(g.segs, strings.Split(p, "/"), false)
}

// matchesChild reports whether g matches some path directly in dir: dir's
// segments and one more, whatever its name.
func (g glob) matchesChild(dir string) bool {
	return matchSegs(g.segs, append(strings.Split(dir, "/"), ""), true)
}

// matchSegs matches glob segments against path segments. ok[j] holds whether
// the glob segments seen so far match the first j path segments; one pass
// per glob segment keeps it linear in each, whatever the number of **. With
// anyLast, the last path segment matches any glob segment.
func matchSegs(globSegs, pathSegs []string, anyLast bool) bool {
	ok := make([]bool, len(pathSegs)+1)
	ok[0] = true
	for _, gs := range globSegs {
		next := make([]bool, len(pathSegs)+1)
		if gs == "**" {
			reached := false
			for j := range ok {
				reached = reached || ok[j]
				next[j] = reached
			}
		} else {
			for j := 1; j <= len(pathSegs); j++ {
				last := anyLast && j == len(pathSegs)
				next[j] = ok[j-1] && (last || segMatch(gs, pathSegs[j-1]))
			}
		}
		ok = next
	}
	return ok[len(pathSegs)]
}

func segMatch(pattern, name string) bool {
	matched, err := path.Match(pattern, name)
	return err == nil && matched
}

// coversUnder reports whether g can match some file below dir: a path that
// starts with dir's segments and has at least one more.
func (g glob) coversUnder(dir string) bool {
	return coversSegs(g.segs, strings.Split(dir, "/"))
}

func coversSegs(globSegs, dirSegs []string) bool {
	if len(dirSegs) == 0 {
		// Any remaining glob can match some deeper path; ** alone matches
		// any.
		return len(globSegs) > 0
	}
	if len(globSegs) == 0 {
		return false
	}
	if globSegs[0] == "**" {
		return coversSegs(globSegs[1:], dirSegs) || coversSegs(globSegs, dirSegs[1:])
	}
	return segMatch(globSegs[0], dirSegs[0]) && coversSegs(globSegs[1:], dirSegs[1:])
}

// globs is a list of globs; an empty list matches nothing.
type globs []glob

func (gs globs) match(p string) bool {
	for _, g := range gs {
		if g.match(p) {
			return true
		}
	}
	return false
}

func (gs globs) matchChild(dir string) bool {
	for _, g := range gs {
		if g.matchesChild(dir) {
			return true
		}
	}
	return false
}
