package gate

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/shape"
)

// foldCase is whether the file system compares names without regard to
// case: on macOS and Windows by default. Tests set it to run either way.
var foldCase = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// maxLinks is how many symlinks resolving one path follows.
const maxLinks = 255

// abs is p made absolute against dir, and clean.
func abs(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}

// rawJoin is p made absolute against dir and not cleaned: the path the
// operating system walks, where ".." follows a symlink before it.
func rawJoin(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return dir + string(filepath.Separator) + p
}

// expandHome replaces a leading "~" or "~/" in p with home; p is returned
// unchanged when home is empty or p has no such prefix.
func expandHome(p, home string) string {
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || (filepath.Separator == '\\' && strings.HasPrefix(p, `~\`)) {
		return filepath.Join(home, p[2:])
	}
	return p
}

// spellings are the absolute paths p can stand for, against the working
// directory dir: as written and cleaned, against dir with its symlinks
// resolved, and with every symlink resolved component by component before
// any ".." after it is applied, as the operating system walks it. A
// decision about a path is made for each spelling, and the strictest wins:
// a protected root reached through a link, or a link inside a root that
// points elsewhere, is still judged as what it is under the policy.
func spellings(dir, p string) []string {
	out := []string{abs(dir, p)}
	if !filepath.IsAbs(p) {
		if d, err := filepath.EvalSymlinks(dir); err == nil {
			out = append(out, filepath.Join(d, p))
		}
	}
	raw := rawJoin(dir, p)
	out = append(out, resolvePath(raw))
	if r, err := filepath.EvalSymlinks(raw); err == nil {
		out = append(out, r)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// spellings are the spellings of p against the gate's working directory.
func (s *setup) spellings(p string) []string { return spellings(s.cwd, p) }

// resolvePath is the absolute path p with its symlinks resolved one
// component at a time, a ".." applied to what is resolved so far, as the
// operating system walks it. From the first component that does not exist,
// the rest is applied lexically. Past maxLinks links, p is returned clean.
func resolvePath(p string) string {
	vol := filepath.VolumeName(p)
	sep := string(filepath.Separator)
	resolved := vol + sep
	todo := segments(p[len(vol):])
	for links := 0; len(todo) > 0; {
		seg := todo[0]
		todo = todo[1:]
		switch seg {
		case ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, seg)
		info, err := os.Lstat(next)
		if err != nil {
			return filepath.Join(append([]string{next}, todo...)...)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			resolved = next
			continue
		}
		links++
		target, err := os.Readlink(next)
		if err != nil || links > maxLinks {
			return filepath.Clean(p)
		}
		if filepath.IsAbs(target) {
			v := filepath.VolumeName(target)
			resolved = v + sep
			target = target[len(v):]
		}
		todo = append(segments(target), todo...)
	}
	return resolved
}

// resolve is a with its symlinks resolved (resolvePath).
func resolve(a string) string { return resolvePath(a) }

// segments splits a path at its separators, without empty segments.
func segments(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
}

// relTo is the absolute path a relative to root, with forward slashes ("."
// for root itself); ok is false when a is outside root. With fold, names
// are compared without regard to case.
func relTo(root, a string, fold bool) (string, bool) {
	if fold {
		root, a = filepath.Clean(root), filepath.Clean(a)
		switch {
		case strings.EqualFold(a, root):
			return ".", true
		case len(a) > len(root) && strings.EqualFold(a[:len(root)], root) &&
			(os.IsPathSeparator(a[len(root)]) || os.IsPathSeparator(root[len(root)-1])):
			return filepath.ToSlash(strings.TrimLeft(a[len(root):], `/\`)), true
		}
		return "", false
	}
	r, err := filepath.Rel(root, a)
	if err != nil {
		return "", false
	}
	r = filepath.ToSlash(r)
	if r == ".." || strings.HasPrefix(r, "../") || filepath.IsAbs(r) {
		return "", false
	}
	return r, true
}

// rel is the absolute path a relative to the repository root, with forward
// slashes ("." for the root itself); ok is false when a is outside the
// repository.
func (s *setup) rel(a string) (string, bool) { return relTo(s.repo.Root, a, s.fold) }

// relsOf are the repository-relative paths the absolute spellings stand
// for, without those outside the repository. On a file system that folds
// case, each is also spelled as on disk and with the protected roots as
// the policy writes them, so the policy's case-sensitive globs see it.
func (s *setup) relsOf(abss []string) []string {
	var out []string
	add := func(r string) {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	for _, a := range abss {
		r, ok := s.rel(a)
		if !ok {
			continue
		}
		add(r)
		if s.fold {
			add(s.rootCase(s.diskCase(r)))
			add(s.rootCase(r))
		}
	}
	return out
}

// rels are the repository-relative paths p stands for (spellings), without
// those outside the repository.
func (s *setup) rels(p string) []string { return s.relsOf(s.spellings(p)) }

// diskCase is the repository-relative path rel with each existing
// component spelled as it is on disk.
func (s *setup) diskCase(rel string) string {
	if rel == "." {
		return rel
	}
	dir := s.repo.Root
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			break
		}
		found := ""
		for _, e := range entries {
			if e.Name() == seg {
				found = seg
				break
			}
			if found == "" && strings.EqualFold(e.Name(), seg) {
				found = e.Name()
			}
		}
		if found == "" {
			break
		}
		segs[i] = found
		dir = filepath.Join(dir, found)
	}
	return strings.Join(segs, "/")
}

// rootCase is rel with a leading protected root, matched without regard to
// case, spelled as the policy writes it.
func (s *setup) rootCase(rel string) string { return rootCaseIn(s.policy.ProtectedRoots(), rel) }

// rootCaseIn is rootCase for the roots given.
func rootCaseIn(roots []string, rel string) string {
	for _, root := range roots {
		if strings.EqualFold(rel, root) {
			return root
		}
		if len(rel) > len(root) && rel[len(root)] == '/' && strings.EqualFold(rel[:len(root)], root) {
			return root + rel[len(root):]
		}
	}
	return rel
}

// hasPrefixFold reports whether path is prefix or below it, segment by
// segment and without regard to case.
func hasPrefixFold(path, prefix string) bool {
	return strings.EqualFold(path, prefix) ||
		len(path) > len(prefix) && path[len(prefix)] == '/' && strings.EqualFold(path[:len(prefix)], prefix)
}

// underRoot reports whether the repository-relative path rel is a
// protected root or inside one, the roots of a nested work tree it is in
// included (frames). Names are compared without regard to case on every
// file system: what it decides is only whether the protected tree is
// scanned, and a scan of the real roots costs nothing in safety.
func (s *setup) underRoot(rel string) bool {
	for _, f := range s.frames(rel) {
		for _, root := range f.roots() {
			if hasPrefixFold(f.sub, root) {
				return true
			}
		}
	}
	return false
}

// touchesRel reports whether the repository-relative path rel is a
// protected root, is inside one, or contains one, compared as underRoot
// compares, the roots of a nested work tree it is in included; or whether
// it is or contains a nested work tree, whose roots it then contains.
func (s *setup) touchesRel(rel string) bool {
	if rel == "." || s.underRoot(rel) {
		return true
	}
	for _, f := range s.frames(rel) {
		if f.sub == "." {
			return true
		}
		for _, root := range f.roots() {
			if hasPrefixFold(root, f.sub) {
				return true
			}
		}
	}
	return len(s.nestedBelow(rel)) > 0
}

// touchesAbs reports whether one absolute spelling is a protected root, is
// inside one, or contains one. A directory above the repository contains
// every root.
func (s *setup) touchesAbs(a string) bool {
	if r, ok := relTo(s.repo.Root, a, true); ok {
		return s.touchesRel(r)
	}
	_, above := relTo(a, s.repo.Root, true)
	return above
}

// touchesIn reports whether p, relative to dir or absolute, touches a
// protected root under any of its spellings.
func (s *setup) touchesIn(dir, p string) bool {
	return slices.ContainsFunc(spellings(dir, p), s.touchesAbs)
}

// touches is touchesIn against the gate's working directory.
func (s *setup) touches(p string) bool { return s.touchesIn(s.cwd, p) }

// hasGlob reports whether w has a shell glob character.
func hasGlob(w string) bool { return strings.ContainsAny(w, "*?[") }

// displayPath is a repository-relative path as a message prints it: each
// segment as a finding prints a name, so one with a personal-data shape or
// a control character is {?}. Paths come from events and commands, and a
// name can carry a value.
func displayPath(rel string) string {
	if rel == "" || rel == "." {
		return rel
	}
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		segs[i] = shape.DisplayName(seg)
	}
	return strings.Join(segs, "/")
}
