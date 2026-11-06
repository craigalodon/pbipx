package gate

import (
	"regexp"
	"slices"
	"strings"
)

// Fed programs (behavior.md, "Working copies").
//
// A program a runner feeds (xargs, parallel) gets words the gate cannot
// see: they come from the runner's input. With a working copy registered,
// such a call touches every copy, unless the pipeline's producer is provably
// confined: it prints only names the gate can place, and none of them is,
// holds or lies inside a copy. Everything else is not confined: a file on
// the input (xargs < L, -a L, parallel ::::), a producer the gate does not
// know, a filter that could make a path of its own.

// pipeChain locates a command among the commands of its line, so the
// commands before it in its pipeline can be read.
type pipeChain struct {
	cmds []simple
	at   int
	line string
}

// groupOpen matches a { that opens a command group ("{ " or "{" at the end)
// and not a brace expansion or a placeholder ({}, {1}, ${HOME}).
var groupOpen = regexp.MustCompile(`\{(\s|$)`)

// hasGroup reports whether line may group commands: a group or a subshell
// (or a substitution), whose stages the flat list of commands does not
// tell apart from a pipeline's.
func hasGroup(line string) bool {
	return strings.Contains(line, "(") || groupOpen.MatchString(line)
}

// judgeFeed records, for a command c run by a runner that feeds it from its
// input, the spots the words it will get can touch: those of the producer
// when it is confined, every copy when it is not.
func (w *walker) judgeFeed(c simple, v view, feed runnerFeed, ch *pipeChain) {
	if printsOnly(v, feed) {
		return
	}
	spots, ok := w.feedSource(c, v, feed, ch)
	if !ok {
		w.feedSpots = append(w.feedSpots, spot{only: true, all: true})
		return
	}
	w.feedSpots = append(w.feedSpots, spots...)
}

// printsOnly reports whether the program a runner feeds only prints its
// arguments: echo or printf, or xargs with no program (which runs echo). It
// reads no name it is given, so the input is not a reach whatever it holds. A
// runner whose options the gate could not read, or parallel with no program
// (its input is the commands), is not one.
func printsOnly(v view, feed runnerFeed) bool {
	if slices.Contains(feed.placeholders, fedAll) {
		return false
	}
	switch v.name {
	case "echo", "printf":
		return true
	case "":
		return feed.runner == "xargs" && len(v.rest) == 0
	}
	return false
}

// feedSource is the spots of the names the pipeline into the runner command
// c carries, ok false when they are not confined.
func (w *walker) feedSource(c simple, v view, feed runnerFeed, ch *pipeChain) ([]spot, bool) {
	if feed.file || feed.splits || ch == nil || !c.pipeIn || len(c.docs) > 0 || !v.cwd.known() || hasGroup(ch.line) {
		return nil, false
	}
	for _, wd := range c.words {
		if wd.redirect && wd.input {
			return nil, false
		}
	}
	// A placeholder only stands for a whole word, and for the name as it is
	// read: {}/.. makes a path of its own from a name, and parallel's {//},
	// {.} and {/} a folder above it, a stem or a name against another folder.
	if slices.Contains(feed.placeholders, fedAll) {
		return nil, false
	}
	for _, a := range v.rest {
		if containsAny(a, feed.placeholders) && !slices.Contains(feed.whole, a) {
			return nil, false
		}
	}
	j := ch.at
	for ch.cmds[j].pipeIn {
		if j == 0 {
			return nil, false
		}
		j--
	}
	for k := j + 1; k < ch.at; k++ {
		fv := w.s.viewOf(ch.cmds[k], v.cwd, w.ph)
		if !stageOK(ch.cmds[k]) || !filterConfined(fv) {
			return nil, false
		}
	}
	pc := ch.cmds[j]
	if !stageOK(pc) {
		return nil, false
	}
	return w.producerSpots(w.s.viewOf(pc, v.cwd, w.ph), v, feed)
}

// stageOK reports whether the command c of a pipeline is a plain command the
// gate read as it stands: no redirection that reads, no here-document, no
// process substitution, a line that tokenized.
func stageOK(c simple) bool {
	if len(c.docs) > 0 || c.procSub || c.procArg || c.fallback || c.uncertain != "" || c.funcDef || c.overflow {
		return false
	}
	for _, wd := range c.words {
		if wd.redirect && wd.input {
			return false
		}
	}
	return true
}

// producerSpots reads the first command of the pipeline, pv, for the names
// it prints. consumer is the runner's view and feed its feed.
func (w *walker) producerSpots(pv view, consumer view, feed runnerFeed) ([]spot, bool) {
	s := w.s
	if !pv.cwd.known() || !allKnown(pv.restKnown, len(pv.rest)) {
		return nil, false
	}
	var out []spot
	place := func(dir, p string) {
		for _, a := range s.spellingsAt(dir, p) {
			out = append(out, spot{path: a, container: true, only: true})
		}
	}
	// The names a lister prints are relative to the folder it lists, and
	// the runner's program takes them relative to its own: both are placed.
	here := func() {
		for _, d := range consumer.cwd.dirs {
			out = append(out, spot{path: d, container: true, only: true})
		}
	}
	if len(pv.argvShapes) < len(pv.rest) {
		return nil, false
	}
	for _, shape := range pv.argvShapes[len(pv.argvShapes)-len(pv.rest):] {
		if shape.glob || shape.brace {
			return nil, false
		}
	}
	switch pv.name {
	case "echo", "printf":
		text, ok := printed(pv.name, pv.rest, w.echoMayEscape())
		if !ok || strings.ContainsAny(text, `'"\`) {
			return nil, false
		}
		// The names are the pieces the runner cuts the text into, and the
		// pieces at blanks too, where one is not cut there.
		toks := strings.Fields(text)
		if feed.sep != "" {
			toks = append(toks, strings.Split(text, feed.sep)...)
		}
		for _, tok := range toks {
			if tok == "" {
				continue
			}
			// A piece that starts with - or @ is an option, or an @file, to
			// the program the runner feeds, not a name.
			if tok[0] == '-' || tok[0] == '@' {
				return nil, false
			}
			for _, d := range pv.cwd.dirs {
				place(d, tok)
			}
		}
		return out, true
	case "find":
		for _, a := range pv.rest {
			if slices.Contains(findUnlisted, a) || strings.HasPrefix(a, "-files0-from") {
				return nil, false
			}
		}
		for _, p := range findPaths(pv.rest) {
			for _, d := range pv.cwd.dirs {
				place(d, p)
			}
		}
		return out, true
	case "fd", "fdfind":
		if !fdPlain(pv.rest) {
			return nil, false
		}
		_, paths := fdArgs(pv.rest)
		for _, p := range paths {
			for _, d := range pv.cwd.dirs {
				place(d, p)
			}
		}
		return out, true
	case "rg", "grep":
		operands, ok := s.searchOperands(pv, nameSearches[pv.name])
		if !ok {
			return nil, false
		}
		for _, xw := range operands {
			abss, unk := s.wordSpellings(pv, xw)
			if unk {
				return nil, false
			}
			for _, a := range abss {
				out = append(out, spot{path: a, container: true, only: true})
			}
		}
		if len(operands) == 0 {
			for _, d := range pv.cwd.dirs {
				out = append(out, spot{path: d, container: true, only: true})
			}
		}
		return out, true
	case "ls":
		if !lsPlain(pv.rest) {
			return nil, false
		}
		here()
		operands := false
		for _, xw := range s.restWords(pv) {
			if xw.redirect || strings.HasPrefix(xw.text, "-") {
				continue
			}
			operands = true
			abss, unk := s.wordSpellings(pv, xw)
			if unk {
				return nil, false
			}
			for _, a := range abss {
				out = append(out, spot{path: a, container: true, only: true})
			}
		}
		if !operands {
			for _, d := range pv.cwd.dirs {
				out = append(out, spot{path: d, container: true, only: true})
			}
		}
		return out, true
	case "git":
		if !gitNamesOnly(pv) {
			return nil, false
		}
		return gitSpots(s, pv), true
	}
	return nil, false
}

// findUnlisted are the words of find that make it print something other
// than the paths it walks, run a command or follow a link into a folder it
// was not given.
var findUnlisted = []string{
	"-printf", "-fprintf", "-ls", "-fls", "-fprint", "-fprint0", "-exec", "-execdir", "-ok", "-okdir",
	"-L", "-H", "-follow", "-f", "-D",
}

// fdPlain reports whether every option of fd is one that only selects what
// it lists: it runs no command and follows no link.
func fdPlain(args []string) bool {
	flags := []string{
		"-H", "--hidden", "-I", "--no-ignore", "-a", "--absolute-path", "-0", "--print0", "-g", "--glob", "-s",
		"--case-sensitive", "-i", "--ignore-case", "-F", "--fixed-strings",
	}
	values := []string{"-e", "--extension", "-t", "--type", "-d", "--max-depth", "-E", "--exclude"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return true
		case slices.Contains(values, a):
			i++
		case strings.HasPrefix(a, "-") && len(a) > 1 && !slices.Contains(flags, a):
			return false
		}
	}
	return true
}

// lsPlain reports whether ls prints only names: one per line, in no tree,
// no sizes or times that put words of their own among them.
func lsPlain(args []string) bool {
	for _, a := range args {
		switch {
		case a == "--":
			return true
		case strings.HasPrefix(a, "--"):
			return false
		case len(a) > 1 && a[0] == '-':
			if strings.Trim(a[1:], "1aAdFpBrtSUvXqbQ") != "" {
				return false
			}
		}
	}
	return true
}

// gitNamesOnly reports whether v, a git command, only prints the names of
// files: ls-files, ls-tree --name-only, diff and its plumbing with
// --name-only or --name-status, status --porcelain or --short, run with
// every word known and nothing that points git elsewhere, formats its
// output or reads files outside the repository.
func gitNamesOnly(v view) bool {
	if !v.cwd.known() {
		return false
	}
	for _, d := range v.cwd.dirs {
		inv, ok := parseGit(v.rest, d)
		if !ok || inv.unsafe || inv.ambiguous || inv.dashHidden || inv.elsewhere {
			return false
		}
		for _, a := range inv.rest {
			name, _, _ := strings.Cut(a, "=")
			if strings.HasPrefix(a, "--") && (abbreviates(name, "--format") || abbreviates(name, "--pretty") ||
				abbreviates(name, "--no-index") || abbreviates(name, "--output")) {
				return false
			}
		}
		named := func(flags ...string) bool {
			return slices.ContainsFunc(inv.args, func(a string) bool { return slices.Contains(flags, a) })
		}
		switch inv.sub {
		case "ls-files":
		case "ls-tree":
			if !named("--name-only", "--name-status") {
				return false
			}
		case "diff", "diff-index", "diff-files":
			if !named("--name-only", "--name-status") {
				return false
			}
		case "status":
			if !named("--porcelain", "--porcelain=v1", "-s", "--short") || verbosity(inv.args, "") != 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// filterSpec is a filter's options: the single letters with no value and
// with one (attached or the next word), the long options and whether they
// take one, whether a bare number is an option (head -5), and whether the
// first operand is a pattern (grep).
type filterSpec struct {
	flags, values string
	long          map[string]bool
	digits        bool
	pattern       bool
}

// filters are the programs that pass the lines they are given on whole,
// each by a table of the options understood. An option not in the table, or
// an operand (a file instead of the pipe), is not a filter the gate follows.
// cut, sed and awk are not here: they can make a path from pieces of one
// (cut -d/ -f1-3 of an absolute path names a folder above it).
var filters = map[string]filterSpec{
	"sort": {flags: "bdfgiMhnRrsuVz", values: "ktST", long: map[string]bool{
		"reverse": false, "unique": false, "numeric-sort": false, "ignore-case": false, "version-sort": false,
		"stable": false, "zero-terminated": false, "human-numeric-sort": false, "general-numeric-sort": false,
		"month-sort": false, "random-sort": false, "dictionary-order": false, "ignore-leading-blanks": false,
		"ignore-nonprinting": false, "key": true, "field-separator": true,
	}},
	"uniq": {flags: "diuz", values: "fsw", long: map[string]bool{
		"ignore-case": false, "repeated": false, "unique": false, "zero-terminated": false,
		"skip-fields": true, "skip-chars": true, "check-chars": true,
	}},
	"head": {flags: "qvz", values: "n", digits: true, long: map[string]bool{
		"quiet": false, "silent": false, "verbose": false, "zero-terminated": false, "lines": true,
	}},
	"tail": {flags: "qvzfFr", values: "n", digits: true, long: map[string]bool{
		"quiet": false, "silent": false, "verbose": false, "zero-terminated": false, "lines": true,
	}},
	"grep": {flags: "ivwxEFGPaIsqz", values: "efmABC", pattern: true, long: map[string]bool{
		"ignore-case": false, "invert-match": false, "word-regexp": false, "line-regexp": false,
		"extended-regexp": false, "fixed-strings": false, "basic-regexp": false, "perl-regexp": false, "text": false,
		"no-messages": false, "null-data": false, "regexp": true, "file": true, "max-count": true,
	}},
}

// filterConfined reports whether v is one of the filters, with only options
// the table has and no file operand, so the lines it prints are lines it was
// given.
func filterConfined(v view) bool {
	spec, ok := filters[v.name]
	if !ok || !allKnown(v.restKnown, len(v.rest)) {
		return false
	}
	for _, xw := range v.words {
		if xw.redirect {
			return false
		}
	}
	operands, pat := 0, false
	args := v.rest
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			operands += len(args) - i - 1
			i = len(args)
		case strings.HasPrefix(a, "--"):
			name, _, hasValue := strings.Cut(a[2:], "=")
			takes, found := spec.long[name]
			if !found || !takes && hasValue {
				return false
			}
			if takes && !hasValue {
				i++
				if i >= len(args) {
					return false
				}
			}
			pat = pat || name == "regexp" || name == "file"
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c := a[j]
				switch {
				case strings.IndexByte(spec.values, c) >= 0:
					if j+1 == len(a) {
						i++
						if i >= len(args) {
							return false
						}
					}
					pat = pat || c == 'e' || c == 'f'
					j = len(a)
				case strings.IndexByte(spec.flags, c) >= 0, spec.digits && c >= '0' && c <= '9':
				default:
					return false
				}
			}
		default:
			operands++
		}
	}
	if spec.pattern && !pat {
		return operands == 1
	}
	return operands == 0
}

// abbreviates reports whether name is long or an abbreviation of it git would
// take for it (at least four characters, "--" included).
func abbreviates(name, long string) bool {
	return len(name) >= 4 && strings.HasPrefix(long, name)
}
