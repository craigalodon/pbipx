package gate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A search program (the grep family, ripgrep, ag, ack and kin) is taken to
// read every file under its search paths, recursively, unless it is
// provably confined to the files it names (behavior.md, Bash, rule 4).
// Recursion is not told by the shape of a flag: ugrep, which can be
// installed as grep, recurses on -NUM, --depth, -g, --include, --index and
// a configuration file, and other tools have their own ways.

// searcher describes one search program's options: how its words parse,
// which options leave it confined to the files it names, and what
// configuration can change it. A short option the gate takes to have no
// value, when it has one, could hide the pattern's place; so an option of
// uncertain arity is listed with withValue, and one not listed at all is
// unknown and widens the search to the working directory.
type searcher struct {
	// withValue are the short options that take a value, flags those known
	// to take none, safe those of either that neither widen the files
	// searched nor read another file.
	withValue, flags, safe string
	// digits is set when -NUM is an option (GNU grep's context, ugrep's
	// depth).
	digits bool
	// pattern are the short options whose value is the pattern, noPattern
	// those after which no pattern is given, file those whose value is a
	// file the program reads.
	pattern, noPattern, file string
	// everywhere are the short options that read files the command line
	// does not name, or run a command on what is read.
	everywhere string
	// The same, for long options (without the leading "--"). longFlags are
	// those known to take no value.
	longWithValue, longFlags, longSafe, longPattern, longNoPattern, longFile, longEverywhere []string
	// noConfig are long options that turn the program's configuration off.
	noConfig []string
	// pathsOnly is set for a program that takes no pattern.
	pathsOnly bool
	// stdin is set when the program, given no file operand and no option
	// outside safe, reads its input (GNU grep). Any other search given no
	// file operand searches its working directory.
	stdin bool
	// configFiles are names that, in the working directory or the home
	// directory, configure the program; configEnv are variables that do.
	configFiles, configEnv []string
}

// grepSearcher is the grep family's options. grep can be ugrep under
// another name, so ugrep's options (and its .ugrep file) count for every
// name; only GNU grep reads its input when given no file.
var grepSearcher = searcher{
	withValue:  "efmABCdDNOMgtKJ",
	flags:      "rRiyncClLhHoqsvwxEFGPbTaIUZzV",
	safe:       "inclLhHoqsvwxEFGPbTaIefmABC",
	digits:     true,
	pattern:    "ef",
	file:       "f",
	everywhere: "Q",
	longWithValue: []string{
		"regexp", "file", "max-count", "after-context", "before-context", "context",
		"directories", "devices", "include", "exclude", "exclude-dir", "exclude-from",
		"include-from", "include-dir", "label", "binary-files", "group-separator",
		"file-type", "glob", "iglob", "max-depth", "jobs", "fuzzy", "depth", "file-extension",
		"file-magic", "neg-regexp", "range", "index",
	},
	longFlags: []string{"recursive", "dereference-recursive"},
	longSafe: []string{
		"regexp", "file", "max-count", "after-context", "before-context", "context",
		"ignore-case", "no-ignore-case", "line-number", "count", "files-with-matches",
		"files-without-match", "no-filename", "with-filename", "only-matching", "quiet",
		"silent", "no-messages", "invert-match", "word-regexp", "line-regexp",
		"extended-regexp", "fixed-strings", "basic-regexp", "perl-regexp", "color", "colour",
		"byte-offset", "initial-tab", "text", "label", "null", "line-buffered",
		"group-separator", "no-group-separator",
	},
	longPattern:    []string{"regexp", "file"},
	longFile:       []string{"file", "exclude-from", "include-from"},
	longEverywhere: []string{"from", "config", "filter", "query", "save-config"},
	configFiles:    []string{".ugrep"},
	configEnv:      []string{"GREP_OPTIONS"},
}

var rgSearcher = searcher{
	withValue: "efgjmrtTABCEM",
	flags:     "inclLoqsvwxFUPHNSIb0uzpa.",
	safe:      "incloqsvwxFUPHNSIb0paefmrABCEM",
	pattern:   "ef",
	file:      "f",
	longWithValue: []string{
		"regexp", "file", "glob", "iglob", "type", "type-not", "type-add", "type-clear",
		"threads", "max-count", "replace", "after-context", "before-context", "context",
		"encoding", "max-columns", "max-depth", "max-filesize", "path-separator", "pre",
		"pre-glob", "color", "colors", "sort", "sortr", "field-match-separator",
		"field-context-separator", "dfa-size-limit", "regex-size-limit", "engine",
		"ignore-file", "context-separator", "hostname-bin", "hyperlink-format", "generate",
	},
	longSafe: []string{
		"regexp", "file", "ignore-case", "case-sensitive", "smart-case", "line-number",
		"no-line-number", "count", "count-matches", "files-with-matches",
		"files-without-match", "only-matching", "quiet", "invert-match", "word-regexp",
		"line-regexp", "fixed-strings", "multiline", "pcre2", "with-filename", "no-filename",
		"heading", "no-heading", "color", "colors", "column", "no-column", "vimgrep", "json",
		"byte-offset", "null", "trim", "replace", "max-count", "after-context",
		"before-context", "context", "encoding", "max-columns", "context-separator", "pretty",
		"text",
	},
	longPattern:    []string{"regexp", "file"},
	longFlags:      []string{"files", "type-list", "hidden", "no-ignore", "follow", "search-zip"},
	longNoPattern:  []string{"files", "type-list"},
	longFile:       []string{"file", "ignore-file"},
	longEverywhere: []string{"pre"},
	noConfig:       []string{"no-config"},
	configEnv:      []string{"RIPGREP_CONFIG_PATH"},
}

// searchers are the search programs, by command name. git grep is git's
// own rule.
var searchers = map[string]searcher{
	"grep":          withStdin(grepSearcher),
	"egrep":         withStdin(grepSearcher),
	"fgrep":         withStdin(grepSearcher),
	"rgrep":         grepSearcher,
	"ugrep":         grepSearcher,
	"ug":            grepSearcher,
	"ugrep-indexer": {pathsOnly: true},
	"rg":            rgSearcher,
	"rga":           rgSearcher,
	"ripgrep-all":   rgSearcher,
	"ag": {
		withValue: "ABCGgmpW",
		flags:     "isSQwvclLofuUtaDz",
		safe:      "isSQwvclLoABCm",
		pattern:   "g",
		file:      "p",
		longWithValue: []string{
			"ignore", "ignore-dir", "file-search-regex", "max-count", "after", "before",
			"context", "depth", "pager", "path-to-ignore", "workers", "width", "color-match",
			"color-path", "color-line-number",
		},
		longSafe: []string{
			"ignore-case", "case-sensitive", "smart-case", "literal", "word-regexp",
			"invert-match", "count", "files-with-matches", "files-without-matches",
			"only-matching", "numbers", "nonumbers", "color", "nocolor", "column", "heading",
			"noheading", "break", "nobreak", "filename", "nofilename", "after", "before",
			"context", "max-count",
		},
		longFile: []string{"path-to-ignore"},
	},
	"ack":      ackSearcher,
	"ack-grep": ackSearcher,
	"sift": {
		withValue: "efABCmx",
		flags:     "ilnvwc",
		pattern:   "ef",
		file:      "f",
		longWithValue: []string{
			"regexp", "file", "context", "after-context", "before-context", "exclude-dirs",
			"ext", "files",
		},
		longPattern: []string{"regexp", "file"},
		longFile:    []string{"file"},
		configFiles: []string{".sift.conf"},
	},
	"pt": {
		withValue:     "eGgABC",
		flags:         "iSlv",
		pattern:       "e",
		longWithValue: []string{"file-search-regexp", "ignore", "after", "before", "context", "depth"},
		configFiles:   []string{".ptconfig.toml"},
	},
}

var ackSearcher = searcher{
	withValue:  "ABCm",
	flags:      "iISwvclLohHQn1f",
	safe:       "iISwvclLohHQn1ABCm",
	noPattern:  "f",
	everywhere: "x",
	longWithValue: []string{
		"type", "ignore-dir", "noignore-dir", "ignore-file", "match", "max-count",
		"output", "pager", "type-set", "type-add", "type-del",
	},
	longSafe: []string{
		"ignore-case", "smart-case", "word-regexp", "invert-match", "count",
		"files-with-matches", "files-without-matches", "literal", "no-filename",
		"with-filename", "color", "nocolor", "column", "nocolumn", "heading", "noheading",
		"break", "nobreak", "group", "nogroup", "match", "after-context", "before-context",
		"context", "max-count", "no-recurse",
	},
	longPattern:    []string{"match"},
	longEverywhere: []string{"files-from", "ackrc"},
	noConfig:       []string{"noenv"},
	configFiles:    []string{".ackrc"},
	configEnv:      []string{"ACKRC", "ACK_OPTIONS"},
}

func withStdin(s searcher) searcher {
	s.stdin = true
	return s
}

// searchArgs is a search command's arguments, sorted.
type searchArgs struct {
	// operands are the indexes of the arguments after the pattern.
	operands []int
	// reads are the indexes of the arguments holding a file an option
	// names for reading (-f FILE), with the file.
	reads []searchRead
	// unsafe names the first option outside the allowlist, everywhere the
	// first that reads files the command line does not name, each as the
	// searcher's table spells it ("" for none); unknown is set by an
	// option the gate does not know, which is never named; noConfig by one
	// that turns the configuration off.
	unsafe, everywhere string
	unknown, noConfig  bool
}

type searchRead struct {
	index int
	file  string
}

// tableLong is "--" and the entry of list that opt is: an option's name
// as the gate's own table spells it, so a message never echoes a word of
// the command.
func tableLong(list []string, opt string) string {
	if i := slices.Index(list, opt); i >= 0 {
		return "--" + list[i]
	}
	return ""
}

// tableShort is "-" and the letter c as set holds it ("-NUM" for a digit).
func tableShort(set string, c byte) string {
	if i := strings.IndexByte(set, c); i >= 0 {
		return "-" + set[i:i+1]
	}
	if '0' <= c && c <= '9' {
		return "-NUM"
	}
	return ""
}

// parseSearch sorts rest, the arguments of a search program described by
// sr.
func parseSearch(sr searcher, rest []string) searchArgs {
	var a searchArgs
	patternGiven := sr.pathsOnly
	var pos []int
	read := func(i int, file string) {
		if file != "" && file != "-" {
			a.reads = append(a.reads, searchRead{i, file})
		}
	}
	unsafe := func(name string) {
		if a.unsafe == "" {
			a.unsafe = name
		}
	}
	everywhere := func(name string) {
		if a.everywhere == "" {
			a.everywhere = name
		}
	}
	for i := 0; i < len(rest); i++ {
		w := rest[i]
		switch {
		case w == "--":
			for j := i + 1; j < len(rest); j++ {
				pos = append(pos, j)
			}
			i = len(rest)
		case strings.HasPrefix(w, "--"):
			opt, value, hasValue := strings.Cut(w[2:], "=")
			at := i
			if slices.Contains(sr.longWithValue, opt) && !hasValue && i+1 < len(rest) {
				i++
				at, value, hasValue = i, rest[i], true
			}
			switch {
			case slices.Contains(sr.longEverywhere, opt):
				everywhere(tableLong(sr.longEverywhere, opt))
			case slices.Contains(sr.noConfig, opt):
				a.noConfig = true
			case slices.Contains(sr.longSafe, opt):
			case slices.Contains(sr.longWithValue, opt):
				name := tableLong(sr.longWithValue, opt)
				if name == "--directories" && value == "recurse" {
					name = "--directories=recurse"
				}
				unsafe(name)
			case slices.Contains(sr.longFlags, opt):
				unsafe(tableLong(sr.longFlags, opt))
			default:
				a.unknown = true
				// An abbreviation that getopt would take for an option
				// that reads anywhere.
				for _, e := range sr.longEverywhere {
					if opt != "" && strings.HasPrefix(e, opt) {
						everywhere("an abbreviation of " + tableLong(sr.longEverywhere, e))
					}
				}
			}
			if slices.Contains(sr.longFile, opt) && hasValue {
				read(at, value)
			}
			if slices.Contains(sr.longPattern, opt) || slices.Contains(sr.longNoPattern, opt) {
				patternGiven = true
			}
		case strings.HasPrefix(w, "-") && len(w) > 1:
		cluster:
			for j := 1; j < len(w); j++ {
				c := w[j]
				safe := strings.IndexByte(sr.safe, c) >= 0
				switch {
				case strings.IndexByte(sr.everywhere, c) >= 0:
					everywhere(tableShort(sr.everywhere, c))
					break cluster
				case strings.IndexByte(sr.withValue, c) >= 0:
					value, at := w[j+1:], i
					if value == "" && i+1 < len(rest) {
						i++
						value, at = rest[i], i
					}
					if !safe {
						name := tableShort(sr.withValue, c)
						if name == "-d" && value == "recurse" {
							name = "-d recurse"
						}
						unsafe(name)
					}
					if strings.IndexByte(sr.file, c) >= 0 {
						read(at, value)
					}
					patternGiven = patternGiven || strings.IndexByte(sr.pattern, c) >= 0
					break cluster
				case strings.IndexByte(sr.flags, c) >= 0 || sr.digits && '0' <= c && c <= '9':
					if !safe {
						unsafe(tableShort(sr.flags, c))
					}
					patternGiven = patternGiven || strings.IndexByte(sr.noPattern, c) >= 0
				default:
					a.unknown = true
					break cluster
				}
			}
		default:
			pos = append(pos, i)
		}
	}
	if !patternGiven && len(pos) > 0 {
		pos = pos[1:]
	}
	a.operands = pos
	return a
}

// searchScope is what one search reads, and why.
type searchScope struct {
	// operands are the paths it searches, reads the files its options
	// name, each read recursively from the command's working directory.
	operands, reads []string
	// widen says why it searches its working directory as well, ""
	// when it does not; everywhere why it can read files the command line
	// does not name, "" when it cannot. Both are fixed words and option
	// names from the searcher's table.
	widen, everywhere string
}

// paths are the operands, the working directory (".") when the search
// widens to it, then the files its options name.
func (sc searchScope) paths() []string {
	out := slices.Clone(sc.operands)
	if sc.widen != "" {
		out = append(out, ".")
	}
	return append(out, sc.reads...)
}

// searchReads returns what the search v reads: the paths it reads under,
// each read recursively from the command's working directory, and
// everywhere when it can read files the command line does not name;
// nothing when v is not a search.
func (w *walker) searchReads(v view) (paths []string, everywhere bool) {
	sc := w.searchScope(v)
	if sc.everywhere != "" {
		return nil, true
	}
	return sc.paths(), false
}

// searchScope is searchReads with the reasons.
//
// A search is confined to the files it names only when every option is in
// its allowlist and no configuration applies; GNU grep given no file then
// reads its input. Otherwise it reads under each operand, and, given none,
// an option the gate does not know, a configuration, or an operand that is
// a glob, under its working directory as well. A file an option names
// (-f FILE) is read as well.
func (w *walker) searchScope(v view) searchScope {
	var sc searchScope
	sr, ok := searchers[v.name]
	if !ok {
		return sc
	}
	// An argument that cannot be told could be any option or any path.
	if slices.Contains(v.restKnown, false) {
		sc.everywhere = "an argument the gate cannot expand"
		return sc
	}
	a := parseSearch(sr, v.rest)
	if a.everywhere != "" {
		sc.everywhere = "the option " + a.everywhere + ", which reads files the command does not name"
		if strings.HasPrefix(a.everywhere, "an ") {
			sc.everywhere = a.everywhere + ", which reads files the command does not name"
		}
		return sc
	}
	config := ""
	if !a.noConfig {
		config = w.searchConfig(sr, v)
	}
	switch {
	case a.unknown:
		sc.widen = "an option the gate does not know"
	case config != "":
		sc.widen = config
	case len(a.operands) == 0 && a.unsafe != "":
		sc.widen = "the option " + a.unsafe + ", with no file operands"
	case len(a.operands) == 0 && !sr.stdin:
		sc.widen = "no file operands"
	}
	for _, i := range a.operands {
		op := v.rest[i]
		if op == "-" {
			continue
		}
		sc.operands = append(sc.operands, op)
		if strings.ContainsAny(op, "*?[{") && sc.widen == "" {
			sc.widen = "a file operand that is a pattern"
		}
	}
	for _, r := range a.reads {
		sc.reads = append(sc.reads, r.file)
	}
	return sc
}

// searchConfig says what configuration could change what the search sr
// reads, "" when none: one of its variables is set, in the gate's
// environment or anywhere in the command line, or one of its files exists
// in the command's working directory or the home directory (which counts
// as existing when it cannot be told, or when the line sets HOME). It
// names the variable or file from sr's table.
func (w *walker) searchConfig(sr searcher, v view) string {
	for _, name := range sr.configEnv {
		if os.Getenv(name) != "" || w.linesMention(name) {
			return name + " is set"
		}
	}
	if len(sr.configFiles) == 0 {
		return ""
	}
	switch {
	case w.s.home == "":
		return "a " + sr.configFiles[0] + " configuration file may exist (the home directory is unknown)"
	case w.linesMention("HOME="):
		return "a " + sr.configFiles[0] + " configuration file may exist (the command sets HOME)"
	}
	for _, d := range append(slices.Clone(v.cwd.dirs), w.s.home) {
		for _, f := range sr.configFiles {
			if _, err := os.Lstat(filepath.Join(hostPath(d), f)); !errors.Is(err, fs.ErrNotExist) {
				return "a " + f + " configuration file"
			}
		}
	}
	return ""
}

// linesMention reports whether any line read holds s.
func (w *walker) linesMention(s string) bool {
	return slices.ContainsFunc(w.lines, func(line string) bool { return strings.Contains(line, s) })
}
