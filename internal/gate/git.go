package gate

import (
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Git subcommands that always print file content (behavior.md, "The gate",
// Bash rule 2).
var gitAlwaysContent = map[string]bool{
	"blame": true, "annotate": true, "grep": true, "archive": true,
	"format-patch": true, "difftool": true, "range-diff": true,
}

// gitSummaryFlags turn diff and show into a summary with no content.
var gitSummaryFlags = []string{
	"--stat", "--numstat", "--shortstat", "--name-only", "--name-status",
	"--dirstat", "--compact-summary", "--summary", "--raw", "--no-patch",
	"-s", "--quiet",
}

// gitPatchFlags make git print a patch: content. Each also matches with a
// value attached ("-U3", "--unified=3", "-L1,2:file").
var gitPatchFlags = []string{
	"-p", "-u", "--patch", "--patch-with-stat", "--patch-with-raw", "-U", "--unified",
	"--cc", "-c", "--dd", "--full-diff", "-L", "--word-diff", "--color-words",
	"--function-context", "-W", "--binary", "--ext-diff", "--textconv",
}

// gitValueFlags are patch flags whose value can be attached without "=".
var gitValueFlags = []string{"-U", "-L"}

// gitCatFileSafe are the cat-file modes that print no content.
var gitCatFileSafe = []string{"-t", "-s", "-e", "--batch-check"}

// gitGlobalWithValue are git's global options that take the next word.
var gitGlobalWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true,
	"--config-env": true, "--super-prefix": true, "--list-cmds": true,
}

// gitInvocation is one git command line, read far enough to judge it.
type gitInvocation struct {
	sub  string   // the subcommand
	args []string // its arguments, before any "--"
	// specs are the words after "--", and dashdash whether there was one.
	specs    []string
	dashdash bool
	// dir is the directory relative paths start from (-C moves it).
	dir string
	// elsewhere is true when --git-dir or --work-tree point git at a tree
	// these paths cannot be checked against.
	elsewhere bool
}

// parseGit reads a git command's words after "git". ok is false when there
// is no subcommand.
func parseGit(words []string, cwd string) (inv gitInvocation, ok bool) {
	inv.dir = cwd
	i := 0
	for i < len(words) {
		w := words[i]
		name, _, hasValue := strings.Cut(w, "=")
		switch {
		case w == "-C" && i+1 < len(words):
			if words[i+1] != "" {
				inv.dir = abs(inv.dir, words[i+1])
			}
			i += 2
			continue
		case name == "--git-dir" || name == "--work-tree":
			inv.elsewhere = true
		}
		if !strings.HasPrefix(w, "-") {
			break
		}
		i++
		if gitGlobalWithValue[name] && !hasValue {
			i++
		}
	}
	if i >= len(words) {
		return inv, false
	}
	inv.sub = words[i]
	rest := words[i+1:]
	if k := slices.Index(rest, "--"); k >= 0 {
		inv.args, inv.specs, inv.dashdash = rest[:k], rest[k+1:], true
	} else {
		inv.args = rest
	}
	return inv, true
}

// hasFlag reports whether args hold one of flags, alone, with "=value", or,
// for those in gitValueFlags, with the value attached.
func hasFlag(args, flags []string) bool {
	for _, a := range args {
		for _, f := range flags {
			switch {
			case a == f, strings.HasPrefix(a, f+"="):
				return true
			case slices.Contains(gitValueFlags, f) && strings.HasPrefix(a, f) && len(a) > len(f):
				return true
			}
		}
	}
	return false
}

// positional is the arguments that are not options.
func positional(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// pathspec is one path a git command is limited to, or excludes.
type pathspec struct {
	rel     string // repository-relative, clean; "" when it cannot be placed
	exclude bool
	// unprovable is true when what it matches cannot be told from the
	// text: a glob, case folding, attributes, or a path outside the
	// repository's tree.
	unprovable bool
}

// gitContent decides rule 2 for one simple command: whether it is git
// printing content that could come from a protected root. It returns the
// subcommand when the command is denied, and "" otherwise.
func (s *setup) gitContent(args []string) string {
	name, rest := program(args)
	if name != "git" {
		return ""
	}
	inv, ok := parseGit(rest, s.cwd)
	if !ok {
		return ""
	}
	specs, content := s.gitSpecs(inv)
	if !content {
		return ""
	}
	if !inv.elsewhere && s.avoidsRoots(specs) {
		return ""
	}
	return inv.sub
}

// gitSpecs says whether inv prints content and, if it does, the paths that
// content can come from. Nil specs with content true is the whole tree.
func (s *setup) gitSpecs(inv gitInvocation) (specs []pathspec, content bool) {
	patch := hasFlag(inv.args, gitPatchFlags)
	after := func() []pathspec { return s.parseSpecs(inv.specs, inv.dir) }
	switch inv.sub {
	case "blame", "annotate":
		// The file is the last argument, or follows "--".
		if inv.dashdash {
			return s.literalSpecs(inv.specs, inv.dir), true
		}
		if n := len(inv.args); n > 0 && !strings.HasPrefix(inv.args[n-1], "-") {
			return s.literalSpecs(inv.args[n-1:], inv.dir), true
		}
		return nil, true
	case "diff":
		if hasFlag(inv.args, gitSummaryFlags) && !patch {
			return nil, false
		}
		return after(), true
	case "show":
		diff := !hasFlag(inv.args, gitSummaryFlags) || patch
		var blobs []string
		commit := false
		pos := positional(inv.args)
		for _, a := range pos {
			if p, ok := revPath(a); ok {
				blobs = append(blobs, p)
			} else {
				commit = true
			}
		}
		if len(pos) == 0 {
			commit = true // HEAD
		}
		specs = s.blobSpecs(blobs, inv.dir)
		switch {
		case diff && commit && len(inv.specs) == 0:
			return nil, true
		case diff && commit:
			specs = append(specs, after()...)
		}
		return specs, len(specs) > 0
	case "log", "reflog", "whatchanged":
		if !patch {
			return nil, false
		}
		return after(), true
	case "stash":
		if len(inv.args) == 0 || inv.args[0] != "show" || !patch {
			return nil, false
		}
		return after(), true
	case "cat-file":
		if hasFlag(inv.args, gitCatFileSafe) {
			return nil, false
		}
		var blobs []string
		for _, a := range positional(inv.args) {
			switch a {
			case "blob", "tree", "commit", "tag":
				continue
			}
			p, ok := revPath(a)
			if !ok {
				// An object name: what it holds cannot be told.
				return []pathspec{{unprovable: true}}, true
			}
			blobs = append(blobs, p)
		}
		if len(blobs) == 0 {
			// --batch reads object names from standard input.
			return []pathspec{{unprovable: true}}, true
		}
		return s.blobSpecs(blobs, inv.dir), true
	}
	if gitAlwaysContent[inv.sub] {
		return after(), true
	}
	return nil, false
}

// revPath splits REV:path, as show and cat-file take it, and returns the
// path. ":path" and ":N:path" name the index.
func revPath(a string) (string, bool) {
	if strings.HasPrefix(a, ":") {
		rest := a[1:]
		if len(rest) >= 2 && isDigit(rune(rest[0])) && rest[1] == ':' {
			rest = rest[2:]
		}
		return rest, true
	}
	_, p, ok := strings.Cut(a, ":")
	return p, ok
}

// blobSpecs places REV:path paths: relative to the repository root, unless
// they start with ./ or ../, which are relative to dir.
func (s *setup) blobSpecs(paths []string, dir string) []pathspec {
	out := make([]pathspec, 0, len(paths))
	for _, p := range paths {
		if p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
			out = append(out, s.place(abs(dir, p), false))
			continue
		}
		out = append(out, s.place(filepath.Join(s.repo.Root, filepath.FromSlash(p)), false))
	}
	return out
}

// literalSpecs places file paths that git takes literally, relative to dir.
func (s *setup) literalSpecs(paths []string, dir string) []pathspec {
	out := make([]pathspec, 0, len(paths))
	for _, p := range paths {
		out = append(out, s.place(abs(dir, p), false))
	}
	return out
}

// parseSpecs reads pathspecs with their magic (gitglossary, "pathspec"):
// ":!x", ":^x" and ":(exclude)x" exclude; ":/x" and ":(top)x" start from
// the repository root; ":(literal)" turns globs off. Other magic, and a
// glob, make a pathspec unprovable.
func (s *setup) parseSpecs(specs []string, dir string) []pathspec {
	out := make([]pathspec, 0, len(specs))
	for _, raw := range specs {
		exclude, top, literal, odd := false, false, false, false
		p := raw
		switch {
		case strings.HasPrefix(p, ":("):
			end := strings.Index(p, ")")
			if end < 0 {
				out = append(out, pathspec{unprovable: true})
				continue
			}
			for m := range strings.SplitSeq(p[2:end], ",") {
				switch m {
				case "exclude":
					exclude = true
				case "top":
					top = true
				case "literal":
					literal = true
				default: // glob, icase, attr:...
					odd = true
				}
			}
			p = p[end+1:]
		case strings.HasPrefix(p, ":"):
			i := 1
			for i < len(p) && strings.ContainsRune("/!^", rune(p[i])) {
				switch p[i] {
				case '/':
					top = true
				default:
					exclude = true
				}
				i++
			}
			if i < len(p) && p[i] == ':' {
				i++
			}
			p = p[i:]
		}
		if odd || (!literal && hasGlob(p)) {
			out = append(out, pathspec{exclude: exclude, unprovable: true})
			continue
		}
		a := abs(dir, p)
		if top {
			a = filepath.Join(s.repo.Root, filepath.FromSlash(p))
		}
		out = append(out, s.place(a, exclude))
	}
	return out
}

// place makes a pathspec of the absolute path a.
func (s *setup) place(a string, exclude bool) pathspec {
	r, ok := s.rel(a)
	if !ok {
		if r2, ok2 := s.rel(resolve(a)); ok2 {
			r, ok = r2, true
		}
	}
	if !ok {
		return pathspec{exclude: exclude, unprovable: true}
	}
	return pathspec{rel: path.Clean(r), exclude: exclude}
}

// avoidsRoots reports whether pathspecs provably keep a git command away
// from every protected root: each root is excluded, or no positive pathspec
// touches it. No positive pathspec means the whole tree. An exclusion
// counts only when it can be placed and covers the whole root.
func (s *setup) avoidsRoots(specs []pathspec) bool {
	var positives []pathspec
	for _, sp := range specs {
		if !sp.exclude {
			positives = append(positives, sp)
		}
	}
	if len(positives) == 0 {
		positives = []pathspec{{rel: "."}}
	}
	for _, root := range s.policy.ProtectedRoots() {
		excluded := false
		for _, sp := range specs {
			if sp.exclude && !sp.unprovable && (sp.rel == "." || root == sp.rel || strings.HasPrefix(root, sp.rel+"/")) {
				excluded = true
			}
		}
		if excluded {
			continue
		}
		for _, sp := range positives {
			if sp.unprovable || touchesRoot(sp.rel, root) {
				return false
			}
		}
	}
	return true
}

// touchesRoot reports whether the repository-relative path rel is root, is
// inside it, or contains it.
func touchesRoot(rel, root string) bool {
	return rel == "." || rel == root || strings.HasPrefix(rel, root+"/") || strings.HasPrefix(root, rel+"/")
}
