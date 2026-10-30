package gate

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/scan"
)

// toolName is the command that runs pbipx itself.
const toolName = "pbipx"

// maxDepth is how deeply command lines nested in a command line (sh -c,
// eval, a command substitution) are read. A line nested deeper counts as
// touching every protected root.
const maxDepth = 4

// shell decides a shell command line (behavior.md, "The gate", Bash, and
// the local-state divergence in Clarifications). The line is never quoted
// in a message: it can carry anything.
func (s *setup) shell(line string) Decision {
	top := splitCommands(line)
	w := &walker{s: s}
	st := cwdState{dirs: []string{s.cwd}}
	w.commands(line, top, &st, 0)

	// Before anything else: changing Claude Code's settings in the
	// repository, or approving the policy. Even the tool alone could
	// redirect into a settings file.
	ctl := loadControls()
	for _, v := range w.views {
		if what, ok := s.reachesControl(v, ctl); ok {
			return deny(ReasonAgentSettings, "the command names a path %s; %s", what, controlWhy)
		}
		if s.viewNamesAgentSettings(v) || s.intoAgentDir(v) {
			return deny(ReasonAgentSettings, "the command names a Claude Code settings file "+
				"(.claude/settings.json or .claude/settings.local.json) or writes into a .claude directory; %s", agentSettingsWhy)
		}
		if selfApproval(v) {
			return deny(ReasonAgentSettings, "the command runs 'pbipx policy approve'; "+
				"approving the policy is a person's step at a terminal, and an agent may not approve the policy that binds it")
		}
	}
	// 1. The tool alone redacts on its own.
	if toolAlone(line, top) {
		return allow()
	}
	// 2. git printing content git history holds.
	for _, v := range w.views {
		if sub := s.gitRule(v); sub != "" {
			return deny(ReasonGitContent,
				"git %s prints file content, and its pathspecs do not provably avoid every protected root; "+
					"git history cannot be vetted. A summary (--stat, --name-only) or pathspecs that exclude the roots are allowed",
				displayPath(sub))
		}
	}
	// Divergence: naming local state denies, as a Read of it does.
	for _, v := range w.views {
		for _, xw := range v.words {
			if rel, ok := s.namesLocalState(v, xw); ok {
				return deny(ReasonLocalState, "the command names %s, Power BI Desktop local state, which is never read",
					displayPath(rel))
			}
		}
	}
	// 3 and 4: a command that can reach a protected root scans the tree.
	if w.reaches() {
		return s.tree("a shell command over a protected root")
	}
	return allow()
}

// toolAlone reports whether line is one simple command running the tool,
// optionally after environment assignments, with no command substitution,
// and not approving the policy.
func toolAlone(line string, cmds []simple) bool {
	if len(cmds) != 1 || len(cmds[0].subs) > 0 || strings.Contains(line, "$(") || strings.Contains(line, "`") {
		return false
	}
	args := cmds[0].args()
	for i, w := range args {
		if !isAssignment(w) {
			return w == toolName && !policyApprove(args[i+1:])
		}
	}
	return false
}

// cwdState is the working directory a command runs in, as far as the
// command line tells.
type cwdState struct {
	// dirs are the spellings of the effective working directory: as cd
	// leaves it logically, and with its symlinks resolved. Nil when it
	// cannot be told.
	dirs []string
	// oldpwd is the previous working directory, "" when unknown.
	oldpwd string
}

func (c cwdState) known() bool { return c.dirs != nil }

func (c cwdState) clone() cwdState { return cwdState{dirs: slices.Clone(c.dirs), oldpwd: c.oldpwd} }

// xword is one word of a command, expanded as far as the gate can.
type xword struct {
	// text is the word with HOME, PWD, OLDPWD and a leading ~ expanded,
	// and any other expansion as written.
	text string
	// known is false when an expansion's value cannot be told.
	known bool
	// pattern is text as a glob: quoted glob characters escaped. glob is
	// true when an unquoted glob character makes the shell expand it.
	pattern  string
	glob     bool
	redirect bool
}

// view is one simple command as the rules see it.
type view struct {
	words []xword
	// argv are the expanded arguments, redirection targets left out and
	// leading reserved words stripped; argvKnown says which are known.
	argv      []string
	argvKnown []bool
	// name is the command run, rest its arguments and under the wrappers
	// it runs under (command); restKnown says which of rest are known.
	name      string
	rest      []string
	restKnown []bool
	under     []string
	cwd       cwdState
	pipeIn    bool
}

// walker reads a command line, the lines nested in it, and the working
// directory each command runs in.
type walker struct {
	s     *setup
	views []view
	lines []string
	// reach is set when something read along the way touches every
	// protected root: a line nested too deep or built at run time, or a
	// find or xargs reading under a root.
	reach bool
}

// commands reads the simple commands cmds of line, nested depth deep, from
// the working directory st, which cd changes for the commands after it.
func (w *walker) commands(line string, cmds []simple, st *cwdState, depth int) {
	if depth > maxDepth {
		w.reach = true
		return
	}
	w.lines = append(w.lines, line)
	var find *view // a find whose output this pipeline carries
	for _, c := range cmds {
		v := w.simple(c.words, c.pipeIn, st, depth)
		for _, sub := range c.subs {
			// A substitution runs in a subshell, in this directory.
			sst := v.cwd.clone()
			w.line(sub, &sst, depth+1)
		}
		if !c.pipeIn {
			find = nil
		}
		if v.name == "find" {
			find = &v
		} else if find != nil && slices.Contains(v.under, "xargs") {
			// find P ... | xargs ... reads every file find prints.
			for _, p := range findPaths(find.rest) {
				w.reachesIn(find.cwd, p)
			}
		}
	}
}

// line reads a nested command line.
func (w *walker) line(line string, st *cwdState, depth int) {
	w.commands(line, splitCommands(line), st, depth)
}

// reachesIn sets reach when p, from the working directory st, touches a
// protected root, or when st is unknown and p relative.
func (w *walker) reachesIn(st cwdState, p string) {
	if !st.known() {
		if !filepath.IsAbs(p) || w.s.touchesIn("/", p) {
			w.reach = true
		}
		return
	}
	for _, d := range st.dirs {
		if w.s.touchesIn(d, p) {
			w.reach = true
		}
	}
}

// simple records one simple command run from st, reads what it nests, and
// applies a cd to st.
func (w *walker) simple(words []word, pipeIn bool, st *cwdState, depth int) view {
	s := w.s
	v := view{cwd: st.clone(), pipeIn: pipeIn}
	var args []string
	var known []bool
	for _, wd := range words {
		xw := s.expandWord(wd, v.cwd)
		v.words = append(v.words, xw)
		if !xw.redirect {
			args = append(args, xw.text)
			known = append(known, xw.known)
		}
	}
	v.argv = stripKeywords(args)
	v.argvKnown = known[len(known)-len(v.argv):]
	v.name, v.rest, v.under = command(v.argv)
	v.restKnown = v.argvKnown[len(v.argvKnown)-len(v.rest):]
	w.views = append(w.views, v)

	switch v.name {
	case "sh", "bash", "zsh", "dash", "ksh", "mksh", "ash", "yash", "fish":
		if i := shellString(v.rest); i >= 0 {
			w.nested(v.rest[i], v.restKnown[i], v.cwd.clone(), depth)
		}
	case "script":
		// script -c 'command' runs the command under a terminal.
		for i, a := range v.rest {
			if (a == "-c" || a == "--command") && i+1 < len(v.rest) {
				w.nested(v.rest[i+1], v.restKnown[i+1], v.cwd.clone(), depth)
			}
		}
	case "eval":
		// eval runs in this shell: a cd in it moves the commands after it.
		w.nestedIn(strings.Join(v.rest, " "), !slices.Contains(v.restKnown, false), st, depth)
	case "find":
		for _, exec := range findExecs(v.rest) {
			w.simple(literalWords(exec), false, new(v.cwd.clone()), depth+1)
		}
		if findReads(v.rest) {
			for _, p := range findPaths(v.rest) {
				w.reachesIn(v.cwd, p)
			}
		}
	case "cd", "pushd", "popd", "chdir":
		s.chdir(v, st)
	}
	// xargs running a reader reads files named on its input, from here.
	if slices.Contains(v.under, "xargs") && readers[v.name] {
		w.reachesIn(v.cwd, ".")
	}
	return v
}

// nested reads a command line passed as a word, in a working directory of
// its own; nestedIn reads it in st.
func (w *walker) nested(line string, known bool, st cwdState, depth int) {
	w.nestedIn(line, known, &st, depth)
}

func (w *walker) nestedIn(line string, known bool, st *cwdState, depth int) {
	if !known {
		// Built at run time: what it runs cannot be told.
		w.reach = true
	}
	w.line(line, st, depth+1)
}

// literalWords makes words of texts already expanded: quoted, so no glob.
func literalWords(texts []string) []word {
	out := make([]word, len(texts))
	for i, t := range texts {
		out[i] = word{text: t, parts: []part{{kind: litPart, text: t, quoted: true}}, quoted: true}
	}
	return out
}

// shellString returns the index in a shell's arguments of the command
// string -c gives it, or -1.
func shellString(rest []string) int {
	c := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--":
			return -1
		case a == "-o" || a == "+o" || a == "-O" || a == "+O":
			i++
		case a == "--command":
			c = true
		case strings.HasPrefix(a, "--"):
		case len(a) > 1 && (a[0] == '-' || a[0] == '+'):
			c = c || strings.Contains(a[1:], "c")
		default:
			if c {
				return i
			}
			return -1
		}
	}
	return -1
}

// findPaths are the paths find starts from: the arguments before its
// expression, after its own options; "." when there are none.
func findPaths(rest []string) []string {
	i := 0
	for i < len(rest) {
		a := rest[i]
		switch {
		case a == "-H" || a == "-L" || a == "-P" || strings.HasPrefix(a, "-O"):
			i++
			continue
		case a == "-D" || a == "-f":
			i += 2
			continue
		}
		break
	}
	var paths []string
	for ; i < len(rest); i++ {
		a := rest[i]
		if strings.HasPrefix(a, "-") || a == "(" || a == "!" || a == ")" || a == "," {
			break
		}
		paths = append(paths, a)
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	return paths
}

// findExecActions are find's actions that run a command on each file.
var findExecActions = []string{"-exec", "-execdir", "-ok", "-okdir"}

// findExecs are the commands find's -exec actions run, each up to its ";"
// or "+".
func findExecs(rest []string) [][]string {
	var out [][]string
	for i := 0; i < len(rest); i++ {
		if !slices.Contains(findExecActions, rest[i]) {
			continue
		}
		j := i + 1
		for j < len(rest) && rest[j] != ";" && (rest[j] != "+" || rest[j-1] != "{}") {
			j++
		}
		out = append(out, rest[i+1:j])
		i = j
	}
	return out
}

// findReads reports whether find runs a command on the files it finds.
func findReads(rest []string) bool {
	return slices.ContainsFunc(rest, func(a string) bool { return slices.Contains(findExecActions, a) })
}

// chdir applies cd, pushd or popd, as v runs it, to st. A target that
// cannot be told leaves the working directory unknown.
func (s *setup) chdir(v view, st *cwdState) {
	unknown := func() { *st = cwdState{} }
	if v.name == "popd" {
		unknown()
		return
	}
	args, known := v.rest, v.restKnown
	i := 0
	for i < len(args) && len(args[i]) > 1 && strings.HasPrefix(args[i], "-") {
		if args[i] == "--" {
			i++
			break
		}
		if v.name != "cd" && v.name != "chdir" {
			// pushd -n, -N: a stack this gate does not keep.
			unknown()
			return
		}
		i++
	}
	var target string
	switch {
	case i >= len(args) && v.name == "pushd":
		unknown()
		return
	case i >= len(args):
		target = s.home
	case !known[i]:
		unknown()
		return
	case args[i] == "-":
		target = st.oldpwd
	case strings.HasPrefix(args[i], "+") && v.name == "pushd":
		unknown()
		return
	default:
		target = args[i]
		if os.Getenv("CDPATH") != "" && !filepath.IsAbs(target) && !dotRelative(target) {
			// CDPATH can take a bare name anywhere.
			unknown()
			return
		}
	}
	if target == "" || (!st.known() && !filepath.IsAbs(target)) {
		unknown()
		return
	}
	bases := st.dirs
	if filepath.IsAbs(target) {
		bases = []string{string(filepath.Separator)}
	}
	var dirs []string
	for _, d := range bases {
		// cd is logical by default: ".." is applied to the path as
		// written. The physical directory is judged as well.
		for _, nd := range []string{abs(d, target), resolvePath(rawJoin(d, target))} {
			if !slices.Contains(dirs, nd) {
				dirs = append(dirs, nd)
			}
		}
	}
	old := ""
	if st.known() {
		old = st.dirs[0]
	}
	*st = cwdState{dirs: dirs, oldpwd: old}
}

// dotRelative reports whether p starts with "." or "..", which CDPATH
// does not search.
func dotRelative(p string) bool {
	return p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
}

// expandWord expands w as far as the gate can, for a command run from st:
// HOME (the gate's own), PWD and OLDPWD (as the line leaves them) and a
// leading ~. Any other expansion leaves it unknown.
func (s *setup) expandWord(w word, st cwdState) xword {
	xw := xword{known: true, redirect: w.redirect}
	var text, pattern strings.Builder
	quotedGlob := false
	add := func(t string, quoted bool) {
		text.WriteString(t)
		if quoted || !hasGlob(t) {
			pattern.WriteString(escapeGlob(t))
			quotedGlob = quotedGlob || hasGlob(t)
			return
		}
		xw.glob = true
		pattern.WriteString(t)
	}
	value := func(name string) (string, bool) {
		switch name {
		case "HOME":
			return s.home, s.home != ""
		case "PWD":
			if st.known() {
				return st.dirs[0], true
			}
		case "OLDPWD":
			return st.oldpwd, st.oldpwd != ""
		}
		return "", false
	}
	for _, p := range w.parts {
		switch p.kind {
		case litPart:
			add(p.text, p.quoted)
		case paramPart:
			if v, ok := value(p.text); ok {
				add(v, true)
				continue
			}
			xw.known = false
			add(p.raw, true)
		case tildePart:
			if p.text == "" && s.home != "" {
				add(s.home, true)
				continue
			}
			xw.known = false
			add(p.raw, true)
		default:
			xw.known = false
			add(p.raw, true)
		}
	}
	xw.text, xw.pattern = text.String(), pattern.String()
	if quotedGlob && filepath.Separator == '\\' {
		// Windows has no glob escape: a quoted glob character would match.
		xw.glob = false
	}
	return xw
}

// escapeGlob escapes glob characters for filepath.Glob. Windows has no
// escape, so there a quoted glob character is left as is.
func escapeGlob(t string) string {
	if filepath.Separator == '\\' || !strings.ContainsAny(t, `*?[\`) {
		return t
	}
	var b strings.Builder
	for _, r := range t {
		if strings.ContainsRune(`*?[\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// maxGlobWalk is how many entries a ** glob walks before it stops.
const maxGlobWalk = 20000

// wordSpellings are the absolute paths an expanded word can stand for in
// v: as written against each spelling of the working directory, the value
// of a --flag=value, what an unquoted glob matches, and for a word with
// "**" everything under its literal prefix (the prefix itself stands for
// that: "is inside or contains" judges it). unknown is true when the word
// cannot be placed: an expansion the gate cannot tell, or a relative path
// from an unknown working directory.
func (s *setup) wordSpellings(v view, xw xword) (out []string, unknown bool) {
	if !xw.known {
		return nil, true
	}
	cands := []string{xw.text}
	if _, val, ok := strings.Cut(xw.text, "="); ok {
		cands = append(cands, expandHome(val, s.home))
	}
	if xw.glob && strings.Contains(xw.pattern, "**") {
		cands = append(cands, globPrefix(xw.pattern))
	}
	dirs := v.cwd.dirs
	if !v.cwd.known() {
		dirs = []string{string(filepath.Separator)}
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if !v.cwd.known() && !filepath.IsAbs(c) {
			unknown = true
			continue
		}
		for _, d := range dirs {
			out = append(out, spellings(d, c)...)
		}
	}
	if xw.glob {
		for _, d := range dirs {
			if !v.cwd.known() && !filepath.IsAbs(xw.pattern) {
				break
			}
			pat := xw.pattern
			if !filepath.IsAbs(pat) {
				pat = d + string(filepath.Separator) + pat
			}
			var matches []string
			if strings.Contains(pat, "**") {
				matches = globStar(pat)
			} else {
				matches, _ = filepath.Glob(pat)
			}
			for _, m := range matches {
				out = append(out, spellings(d, m)...)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out), unknown
}

// globPrefix is the directory a glob pattern starts in: the text before
// its first unescaped glob character, up to the last separator, unescaped.
func globPrefix(pattern string) string {
	var b strings.Builder
	lastSep := -1
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '\\' && filepath.Separator != '\\' && i+1 < len(pattern) {
			i++
			b.WriteByte(pattern[i])
			continue
		}
		if strings.IndexByte("*?[", c) >= 0 {
			break
		}
		if c == '/' || c == filepath.Separator {
			lastSep = b.Len()
		}
		b.WriteByte(c)
	}
	prefix := b.String()
	switch {
	case lastSep < 0:
		return "."
	case lastSep == 0:
		return prefix[:1]
	}
	return prefix[:lastSep]
}

// globStar is what the absolute pattern matches when "**" is a whole
// segment that matches any number of directories, as zsh and bash with
// globstar read it. A "**" inside a segment is "*". It walks at most
// maxGlobWalk entries, without following symlinks.
func globStar(pattern string) []string {
	base := globPrefix(pattern)
	rest := strings.TrimPrefix(filepath.ToSlash(pattern), filepath.ToSlash(base))
	pats := segments(rest)
	// The prefix was unescaped; the pattern after it keeps its escapes.
	var out []string
	walked := 0
	_ = filepath.WalkDir(base, func(p string, _ fs.DirEntry, err error) error {
		walked++
		if err != nil || walked > maxGlobWalk {
			return filepath.SkipAll
		}
		r, rerr := filepath.Rel(base, p)
		if rerr != nil || r == "." {
			return nil //nolint:nilerr // a path that cannot be placed matches nothing
		}
		if matchStar(pats, strings.Split(filepath.ToSlash(r), "/")) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// matchStar matches glob segments against path segments; a "**" segment
// matches any number of them.
func matchStar(pats, segs []string) bool {
	if len(pats) == 0 {
		return len(segs) == 0
	}
	if pats[0] == "**" {
		for k := 0; k <= len(segs); k++ {
			if matchStar(pats[1:], segs[k:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, err := path.Match(strings.ReplaceAll(pats[0], "**", "*"), segs[0])
	return err == nil && ok && matchStar(pats[1:], segs[1:])
}

// viewNamesAgentSettings reports whether a word of v names a Claude Code
// settings file in the repository, under any spelling. No path is returned
// for a message: a file that does not exist yet is named only by the
// command's text.
func (s *setup) viewNamesAgentSettings(v view) bool {
	for _, xw := range v.words {
		if !mayWrite(v, xw) {
			continue
		}
		abss, _ := s.wordSpellings(v, xw)
		for _, rel := range s.relsOf(abss) {
			if scan.IsAgentSettings(rel) {
				return true
			}
		}
	}
	return false
}

// reachesControl reports whether v names a path outside the repository
// that controls the gate (controls.target) in any word, or copies or
// writes into a user-level Claude Code settings directory. It returns what
// the path is, in words.
func (s *setup) reachesControl(v view, ctl controls) (string, bool) {
	for _, xw := range v.words {
		if !mayWrite(v, xw) {
			continue
		}
		abss, _ := s.wordSpellings(v, xw)
		if what, ok := ctl.target(abss); ok {
			return what, true
		}
	}
	dests, _ := s.destinations(v)
	for _, d := range dests {
		abss, _ := s.wordSpellings(v, d)
		if what, ok := ctl.dir(abss); ok {
			return what, true
		}
	}
	return "", false
}

// copiers write into their last argument, or into -t's.
var copiers = map[string]string{
	"cp": "S", "mv": "S", "ln": "S", "install": "mogS", "rsync": "efB",
}

// intoAgentDir reports whether v writes into a .claude directory inside
// the repository: as a redirection's target, the destination of cp, mv,
// ln, install or rsync (or a .claude directory copied into the
// repository), a file tee writes or dd's of=. A settings file can be made
// there under any name the command does not show.
func (s *setup) intoAgentDir(v view) bool {
	dests, sources := s.destinations(v)
	intoRepo := false
	for _, d := range dests {
		abss, _ := s.wordSpellings(v, d)
		for _, a := range abss {
			rel, ok := s.rel(a)
			if !ok {
				continue
			}
			intoRepo = true
			if isAgentDir(rel) {
				return true
			}
		}
	}
	if !intoRepo {
		return false
	}
	for _, src := range sources {
		if strings.EqualFold(path.Base(filepath.ToSlash(strings.TrimRight(src.text, `/\`))), ".claude") {
			return true
		}
	}
	return false
}

// destinations are the words v writes into: its redirection targets, the
// destination of cp, mv, ln, install or rsync, the files tee writes and
// dd's of=. sources are what a copier copies.
func (s *setup) destinations(v view) (dests, sources []xword) {
	for _, xw := range v.words {
		if xw.redirect {
			dests = append(dests, xw)
		}
	}
	rest := s.restWords(v)
	switch withValue, copier := copiers[v.name]; {
	case copier:
		var pos []xword
		for i := 0; i < len(rest); i++ {
			a := rest[i].text
			switch {
			case a == "-t" || a == "--target-directory":
				if i+1 < len(rest) {
					dests = append(dests, rest[i+1])
				}
				i++
			case strings.HasPrefix(a, "--target-directory="):
				dests = append(dests, xword{text: strings.TrimPrefix(a, "--target-directory="), known: rest[i].known})
			case len(a) == 2 && a[0] == '-' && strings.IndexByte(withValue, a[1]) >= 0:
				i++
			case strings.HasPrefix(a, "-") && a != "-":
			default:
				pos = append(pos, rest[i])
			}
		}
		if len(pos) >= 2 {
			dests = append(dests, pos[len(pos)-1])
			sources = pos[:len(pos)-1]
		}
	case v.name == "tee":
		for _, xw := range rest {
			if !strings.HasPrefix(xw.text, "-") {
				dests = append(dests, xw)
			}
		}
	case v.name == "dd":
		for _, xw := range rest {
			if of, ok := strings.CutPrefix(xw.text, "of="); ok {
				dests = append(dests, xword{text: of, known: xw.known})
			}
		}
	}
	return dests, sources
}

// isAgentDir reports whether the repository-relative rel is a .claude
// directory.
func isAgentDir(rel string) bool { return strings.EqualFold(path.Base(rel), ".claude") }

// restWords are v's expanded words after its command name, redirection
// targets left out.
func (s *setup) restWords(v view) []xword {
	var args []xword
	for _, xw := range v.words {
		if !xw.redirect {
			args = append(args, xw)
		}
	}
	return args[len(args)-len(v.rest):]
}

// selfApproval reports whether v runs 'pbipx policy approve': the tool by
// any path, or under script, expect or unbuffer, which give it the
// terminal approval asks for.
func selfApproval(v view) bool {
	switch v.name {
	case toolName, toolName + ".exe":
		return policyApprove(v.rest)
	case "script", "expect", "unbuffer":
		tokens := strings.FieldsFunc(strings.Join(v.rest, " "), func(r rune) bool {
			return strings.ContainsRune(" \t\r\n;&|'\"{}[]()", r)
		})
		for i, t := range tokens {
			if b := baseName(t); b == toolName || b == toolName+".exe" {
				if policyApprove(tokens[i+1:]) {
					return true
				}
			}
		}
	}
	return false
}

// policyApprove reports whether the tool's arguments hold "policy" and,
// after it, "approve".
func policyApprove(args []string) bool {
	i := slices.Index(args, "policy")
	if i < 0 {
		return false
	}
	return slices.ContainsFunc(args[i+1:], func(a string) bool { return strings.HasPrefix(a, "approve") })
}

// gitRule decides rule 2 for v, from each spelling of its working
// directory; from an unknown one, as if from each protected root, where
// every relative pathspec touches the root.
func (s *setup) gitRule(v view) string {
	dirs := v.cwd.dirs
	if !v.cwd.known() {
		dirs = []string{s.repo.Root}
		for _, root := range s.policy.ProtectedRoots() {
			dirs = append(dirs, filepath.Join(s.repo.Root, filepath.FromSlash(root)))
		}
	}
	for _, d := range dirs {
		in := *s
		in.cwd = d
		if sub := in.gitContent(v.argv); sub != "" {
			return sub
		}
	}
	return ""
}

// namesLocalState reports whether xw, in v, names a local-state file or a
// directory local state is kept in, under any spelling. It returns the
// path, repository-relative.
func (s *setup) namesLocalState(v view, xw xword) (string, bool) {
	abss, _ := s.wordSpellings(v, xw)
	for _, rel := range s.relsOf(abss) {
		if class, _ := s.policy.Classify(rel); class == policy.LocalState || s.policy.LocalStateDir(rel) || s.nestedLocalState(rel) {
			return rel, true
		}
	}
	return "", false
}

// readers are commands that print or copy what files hold. An argument of
// one the gate cannot expand could name any file.
var readers = map[string]bool{
	"cat": true, "tac": true, "nl": true, "head": true, "tail": true, "less": true, "more": true,
	"most": true, "bat": true, "batcat": true, "view": true, "zcat": true, "zless": true, "zmore": true,
	"grep": true, "egrep": true, "fgrep": true, "zgrep": true, "rg": true, "ag": true, "ack": true,
	"sed": true, "awk": true, "gawk": true, "mawk": true, "nawk": true, "strings": true, "od": true,
	"xxd": true, "hexdump": true, "hd": true, "base64": true, "base32": true, "sort": true, "uniq": true,
	"cut": true, "wc": true, "paste": true, "join": true, "comm": true, "diff": true, "cmp": true,
	"jq": true, "yq": true, "cp": true, "rsync": true, "tar": true, "zip": true, "find": true,
	"iconv": true, "fold": true, "fmt": true, "pr": true, "column": true, "source": true, ".": true,
	"vi": true, "vim": true, "nvim": true, "nano": true, "emacs": true, "ed": true, "ex": true,
}

// reaches reports whether the command line can reach a protected root
// (rules 3 and 4): a command runs from inside one or from a working
// directory that cannot be told; a line names one as a whole word; a word
// of a command, placed against its working directory, is one, is inside
// one, or contains one; a reading command has a word that cannot be
// placed; a recursive search covers a path that touches one; or find,
// xargs or a nested line reach one (walker.reach).
func (w *walker) reaches() bool {
	s := w.s
	if w.reach {
		return true
	}
	for _, line := range w.lines {
		for _, root := range s.policy.ProtectedRoots() {
			if namesWord(line, root) {
				return true
			}
		}
	}
	for _, v := range w.views {
		if !v.cwd.known() || slices.ContainsFunc(v.cwd.dirs, s.dirInRoot) {
			return true
		}
		reader := readers[v.name] || searchers[v.name].always || searchers[v.name].recursive != ""
		for _, xw := range v.words {
			abss, unknown := s.wordSpellings(v, xw)
			if unknown && reader && !optionOnly(xw.text) {
				return true
			}
			if slices.ContainsFunc(abss, s.touchesAbs) {
				return true
			}
		}
		if paths, ok := searchPaths(v.argv); ok {
			for _, p := range paths {
				for _, d := range v.cwd.dirs {
					if s.touchesIn(d, p) {
						return true
					}
				}
			}
		}
	}
	return false
}

// optionOnly reports whether a word is an option with no value in it.
func optionOnly(t string) bool { return strings.HasPrefix(t, "-") && !strings.Contains(t, "=") }

// dirInRoot reports whether the working directory d, as given or with its
// symlinks resolved, is a protected root or inside one.
func (s *setup) dirInRoot(d string) bool {
	for _, a := range []string{d, resolvePath(d)} {
		if r, ok := relTo(s.repo.Root, a, true); ok && s.underRoot(r) {
			return true
		}
	}
	return false
}

// namesWord reports whether name occurs in line as a whole word, without
// regard to case: neither side touches a letter, a digit, "_", "-" or
// ".".
func namesWord(line, name string) bool {
	line, name = strings.ToLower(line), strings.ToLower(name)
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
