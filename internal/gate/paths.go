package gate

import (
	"path/filepath"
	"slices"
	"strings"
)

// abs is p made absolute against dir, and clean.
func abs(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}

// spellings are the absolute paths p can stand for: as written, against
// the working directory as given and with its symlinks resolved, and with
// every symlink in p resolved. A decision about a path is made for each
// spelling, and the strictest wins: a protected root reached through a
// link, or a link inside a root that points elsewhere, is still judged as
// what it is under the policy.
func (s *setup) spellings(p string) []string {
	out := []string{abs(s.cwd, p)}
	if !filepath.IsAbs(p) {
		if cwd, err := filepath.EvalSymlinks(s.cwd); err == nil {
			out = append(out, filepath.Join(cwd, p))
		}
	}
	out = append(out, resolve(out[0]))
	slices.Sort(out)
	return slices.Compact(out)
}

// resolve is a with its symlinks resolved: all of them when a exists, or
// those of its directory when only that does.
func resolve(a string) string {
	if r, err := filepath.EvalSymlinks(a); err == nil {
		return r
	}
	if r, err := filepath.EvalSymlinks(filepath.Dir(a)); err == nil {
		return filepath.Join(r, filepath.Base(a))
	}
	return a
}

// rel is the absolute path a relative to the repository root, with forward
// slashes ("." for the root itself); ok is false when a is outside the
// repository.
func (s *setup) rel(a string) (string, bool) {
	r, err := filepath.Rel(s.repo.Root, a)
	if err != nil {
		return "", false
	}
	r = filepath.ToSlash(r)
	if r == ".." || strings.HasPrefix(r, "../") || filepath.IsAbs(r) {
		return "", false
	}
	return r, true
}

// rels are the repository-relative paths p stands for (spellings), without
// those outside the repository.
func (s *setup) rels(p string) []string {
	var out []string
	for _, a := range s.spellings(p) {
		if r, ok := s.rel(a); ok && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// touchesRel reports whether the repository-relative path rel is a
// protected root, is inside one, or contains one.
func (s *setup) touchesRel(rel string) bool {
	if rel == "." || s.policy.UnderRoot(rel) {
		return true
	}
	for _, root := range s.policy.ProtectedRoots() {
		if strings.HasPrefix(root, rel+"/") {
			return true
		}
	}
	return false
}

// touches reports whether p, relative to the working directory or
// absolute, is a protected root, is inside one, or contains one, under any
// of its spellings. A directory above the repository contains every root.
func (s *setup) touches(p string) bool {
	for _, a := range s.spellings(p) {
		if r, ok := s.rel(a); ok {
			if s.touchesRel(r) {
				return true
			}
			continue
		}
		if up, err := filepath.Rel(a, s.repo.Root); err == nil && !strings.HasPrefix(filepath.ToSlash(up), "../") && up != ".." {
			return true
		}
	}
	return false
}

// hasGlob reports whether w has a shell glob character.
func hasGlob(w string) bool { return strings.ContainsAny(w, "*?[") }

// expand is what an unquoted shell glob in w matches, relative to the
// working directory; nothing for a word without one or a quoted word.
func (s *setup) expand(w word) []string {
	if w.quoted || !hasGlob(w.text) {
		return nil
	}
	matches, err := filepath.Glob(abs(s.cwd, w.text))
	if err != nil {
		return nil
	}
	return matches
}
