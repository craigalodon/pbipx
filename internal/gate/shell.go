package gate

import (
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/policy"
)

// toolName is the command that runs pbipx itself.
const toolName = "pbipx"

// shell decides a shell command line (behavior.md, "The gate", Bash, and
// the local-state divergence in Clarifications). The line is never quoted
// in a message: it can carry anything.
func (s *setup) shell(line string) Decision {
	cmds := splitCommands(line)
	// 1. The tool alone redacts on its own.
	if toolAlone(line, cmds) {
		return allow()
	}
	// 2. git printing content git history holds.
	for _, c := range cmds {
		if sub := s.gitContent(c.args()); sub != "" {
			return deny(ReasonGitContent,
				"git %s prints file content, and its pathspecs do not provably avoid every protected root; "+
					"git history cannot be vetted. A summary (--stat, --name-only) or pathspecs that exclude the roots are allowed",
				sub)
		}
	}
	// Divergence: naming local state denies, as a Read of it does.
	for _, c := range cmds {
		for _, w := range c {
			if rel, ok := s.namesLocalState(w); ok {
				return deny(ReasonLocalState, "the command names %s, Power BI Desktop local state, which is never read", rel)
			}
		}
	}
	// 3 and 4: a command that can reach a protected root scans the tree.
	if s.reachesRoot(line, cmds) {
		return s.tree("a shell command over a protected root")
	}
	return allow()
}

// toolAlone reports whether line is one simple command running the tool,
// optionally after environment assignments, with no command substitution.
func toolAlone(line string, cmds []simple) bool {
	if len(cmds) != 1 || strings.Contains(line, "$(") || strings.Contains(line, "`") {
		return false
	}
	for _, w := range cmds[0].args() {
		if !isAssignment(w) {
			return w == toolName
		}
	}
	return false
}

// namesLocalState reports whether w names a local-state file or a
// directory local state is kept in, as written, as the value of a
// --flag=value, or as what an unquoted glob in it matches. It returns the
// path, repository-relative.
func (s *setup) namesLocalState(w word) (string, bool) {
	candidates := []string{w.text}
	if _, v, ok := strings.Cut(w.text, "="); ok && v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, s.expand(w)...)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		for _, rel := range s.rels(c) {
			if class, _ := s.policy.Classify(rel); class == policy.LocalState || s.policy.LocalStateDir(rel) {
				return rel, true
			}
		}
	}
	return "", false
}

// reachesRoot reports whether a command line can reach a protected root:
// its working directory is inside one; it names one as a whole word, or
// through an unquoted glob that matches one; or it searches recursively
// over a path that touches one.
func (s *setup) reachesRoot(line string, cmds []simple) bool {
	for _, rel := range s.rels(s.cwd) {
		if s.policy.UnderRoot(rel) {
			return true
		}
	}
	for _, root := range s.policy.ProtectedRoots() {
		if namesWord(line, root) {
			return true
		}
	}
	for _, c := range cmds {
		for _, w := range c {
			for _, m := range s.expand(w) {
				if s.touches(m) {
					return true
				}
			}
		}
		if paths, ok := searchPaths(c.args()); ok {
			for _, p := range paths {
				if s.touches(p) {
					return true
				}
			}
		}
	}
	return false
}

// namesWord reports whether name occurs in line as a whole word: neither
// side touches a letter, a digit, "_", "-" or ".".
func namesWord(line, name string) bool {
	for i := 0; ; {
		j := strings.Index(line[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		if (start == 0 || !wordByte(line[start-1])) && (end == len(line) || !wordByte(line[end])) {
			return true
		}
		i = start + 1
	}
}

func wordByte(b byte) bool {
	return b == '_' || b == '-' || b == '.' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || b >= 0x80
}

// searcher describes a recursive search tool's options.
type searcher struct {
	always    bool   // recursive without a flag
	recursive string // short flags that make it recursive
	withValue string // short options that take a value
	pattern   string // short options whose value is the pattern
	noPattern string // short options after which no pattern is given
	// The same, for long options (without the leading "--").
	longRecursive, longWithValue, longPattern, longNoPattern []string
}

var grepSearcher = searcher{
	recursive: "rR", withValue: "efmABCdD", pattern: "ef",
	longRecursive: []string{"recursive", "dereference-recursive"},
	longWithValue: []string{
		"regexp", "file", "max-count", "after-context", "before-context", "context",
		"directories", "devices", "include", "exclude", "exclude-dir", "exclude-from",
		"label", "binary-files", "group-separator",
	},
	longPattern: []string{"regexp", "file"},
}

// searchers are the recursive searches of rule 4, by command name.
var searchers = map[string]searcher{
	"grep":  grepSearcher,
	"egrep": grepSearcher,
	"fgrep": grepSearcher,
	"rg": {
		always: true, withValue: "efgjmrtTABCEM", pattern: "ef",
		longWithValue: []string{
			"regexp", "file", "glob", "iglob", "type", "type-not", "type-add", "type-clear",
			"threads", "max-count", "replace", "after-context", "before-context", "context",
			"encoding", "max-columns", "max-depth", "max-filesize", "path-separator", "pre",
			"pre-glob", "colors", "sort", "sortr", "field-match-separator",
			"field-context-separator", "dfa-size-limit", "regex-size-limit", "engine",
			"ignore-file", "context-separator", "hostname-bin", "hyperlink-format", "generate",
		},
		longPattern:   []string{"regexp", "file"},
		longNoPattern: []string{"files", "type-list"},
	},
	"ag": {
		always: true, withValue: "ABCGgmpW", pattern: "g",
		longWithValue: []string{
			"ignore", "ignore-dir", "file-search-regex", "max-count", "after", "before",
			"context", "depth", "pager", "path-to-ignore", "workers", "width",
		},
	},
	"ack": {
		always: true, withValue: "ABCm", noPattern: "f",
		longWithValue: []string{
			"type", "ignore-dir", "noignore-dir", "ignore-file", "match", "max-count",
			"output", "pager", "type-set", "type-add", "type-del",
		},
		longPattern: []string{"match"},
	},
}

// searchPaths returns the paths a recursive search covers: the positional
// arguments after the pattern (unless an option gave it), "." when there
// are none. ok is false when the command is not a recursive search.
func searchPaths(args []string) (paths []string, ok bool) {
	name, rest := program(args)
	sr, known := searchers[name]
	if !known {
		return nil, false
	}
	recursive, patternGiven := sr.always, false
	var pos []string
	for i := 0; i < len(rest); i++ {
		w := rest[i]
		switch {
		case w == "--":
			pos = append(pos, rest[i+1:]...)
			i = len(rest)
		case strings.HasPrefix(w, "--"):
			opt, value, hasValue := strings.Cut(w[2:], "=")
			if slices.Contains(sr.longWithValue, opt) && !hasValue && i+1 < len(rest) {
				i++
				value = rest[i]
			}
			recursive = recursive || slices.Contains(sr.longRecursive, opt) || (opt == "directories" && value == "recurse")
			patternGiven = patternGiven || slices.Contains(sr.longPattern, opt) || slices.Contains(sr.longNoPattern, opt)
		case strings.HasPrefix(w, "-") && len(w) > 1:
			for j := 1; j < len(w); j++ {
				c := w[j]
				recursive = recursive || strings.IndexByte(sr.recursive, c) >= 0
				patternGiven = patternGiven || strings.IndexByte(sr.noPattern, c) >= 0
				if strings.IndexByte(sr.withValue, c) < 0 {
					continue
				}
				value := w[j+1:]
				if value == "" && i+1 < len(rest) {
					i++
					value = rest[i]
				}
				patternGiven = patternGiven || strings.IndexByte(sr.pattern, c) >= 0
				recursive = recursive || (c == 'd' && value == "recurse")
				break
			}
		default:
			pos = append(pos, w)
		}
	}
	if !recursive {
		return nil, false
	}
	if !patternGiven && len(pos) > 0 {
		pos = pos[1:]
	}
	if len(pos) == 0 {
		pos = []string{"."}
	}
	return pos, true
}
