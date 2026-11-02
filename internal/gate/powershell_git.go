package gate

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// The git rule for PowerShell (behavior.md, "The gate", PowerShell). The
// gate has no parser for PowerShell, but it reads a line into statements
// far enough to find git in each, and judges each git it finds with the
// Bash rule's judge (gitContent).

// maxPSGroups bounds how deeply groups ((...), $(...), @(...), {...}) nest
// in a PowerShell line the gate reads into statements.
const maxPSGroups = 64

// psWord is one word of a PowerShell statement: its text, as PowerShell
// passes it to a program, and whether that text cannot be told (it holds
// an expansion, an escape, a group or a splat), holds a quoted part, or is
// joined to the word before it by a comma (an array). bare is set when an
// expansion (a variable, a subexpression, a group or a splat) is outside
// double quotes: its value may be an array, which PowerShell passes to a
// program as several arguments.
type psWord struct {
	text                         string
	unknown, quoted, comma, bare bool
}

// psStmt is one PowerShell statement: its words, after any assignment to
// a variable, whether it began with the call operator &, and whether a
// pipe feeds it. assign is the
// environment variable the statement assigns ($env:NAME = ...),
// upper-cased, "" for none.
type psStmt struct {
	words  []psWord
	call   bool
	piped  bool // it reads the output of the statement before it
	assign string
}

// psLexer reads a PowerShell line into statements. A group is a list of
// statements of its own: they are read, and in the statement holding it,
// the group is a word that cannot be told.
type psLexer struct {
	rs    []rune
	i     int
	stmts []psStmt
	words int
	over  bool
}

func (l *psLexer) peek(k int) rune {
	if l.i+k < len(l.rs) {
		return l.rs[l.i+k]
	}
	return 0
}

// psStatements reads a PowerShell line into statements: one ends at ;, |,
// && or ||, a background &, a newline, and the bounds of a group or a
// script block. A redirection and its target are left out, and so are
// comments. ok is false past maxPSWords words or maxPSGroups nested
// groups.
func psStatements(line string) (stmts []psStmt, ok bool) {
	l := &psLexer{rs: []rune(line)}
	l.block(0, 0)
	return l.stmts, !l.over
}

// psLexState is what block holds of the statement and the word it reads.
type psLexState struct {
	st        psStmt
	cur       strings.Builder
	w         psWord
	have      bool
	dropNext  bool // the next word is a redirection's target
	nextComma bool
}

func (l *psLexer) flush(x *psLexState) {
	if x.have {
		x.w.text = x.cur.String()
		if x.dropNext {
			x.dropNext = false
		} else {
			x.w.comma = x.nextComma
			x.st.words = append(x.st.words, x.w)
			l.words++
		}
		x.nextComma = false
	}
	x.cur.Reset()
	x.w, x.have = psWord{}, false
}

func (l *psLexer) end(x *psLexState) {
	l.flush(x)
	x.dropNext, x.nextComma = false, false
	if len(x.st.words) > 0 || x.st.assign != "" {
		l.stmts = append(l.stmts, x.st)
	}
	x.st = psStmt{}
}

// group reads a group or a script block whose opening is open runes long.
// In argument position, or after the call operator, it is a word that
// cannot be told.
func (l *psLexer) group(x *psLexState, open int, closeWith rune, depth int) {
	if x.have || len(x.st.words) > 0 || x.st.call {
		x.cur.WriteString("$()")
		x.w.unknown, x.w.bare, x.have = true, true, true
	}
	l.i += open
	l.block(closeWith, depth+1)
}

// block reads statements up to closer (0 for the end of the line).
func (l *psLexer) block(closer rune, depth int) {
	if depth > maxPSGroups {
		l.over = true
		l.i = len(l.rs)
		return
	}
	x := &psLexState{}
	for l.i < len(l.rs) {
		if l.words > maxPSWords {
			l.over = true
			l.i = len(l.rs)
			return
		}
		r := l.rs[l.i]
		switch {
		case closer != 0 && r == closer:
			l.end(x)
			l.i++
			return
		case r == '#' && !x.have:
			for l.i < len(l.rs) && l.rs[l.i] != '\n' {
				l.i++
			}
		case r == '<' && l.peek(1) == '#':
			l.i += 2
			for l.i < len(l.rs) && (l.rs[l.i] != '#' || l.peek(1) != '>') {
				l.i++
			}
			l.i += 2
		case r == '`':
			l.escape(x)
		case psSingle(r):
			l.single(x)
		case psDouble(r):
			l.double(x, depth)
		case r == '$':
			l.variable(x, depth)
		case r == '@' && (l.peek(1) == '(' || l.peek(1) == '{'):
			closeWith := ')'
			if l.peek(1) == '{' {
				closeWith = '}'
			}
			l.group(x, 2, closeWith, depth)
			x.cur.WriteString("@()")
			x.w.unknown, x.w.bare, x.have = true, true, true
		case r == '@' && !x.have && (unicode.IsLetter(l.peek(1)) || l.peek(1) == '_'):
			// A splat: the arguments a variable holds.
			x.w.unknown, x.w.bare, x.have = true, true, true
			x.cur.WriteRune(r)
			l.i++
		case r == '(':
			l.group(x, 1, ')', depth)
		case r == '{':
			l.group(x, 1, '}', depth)
		case r == ')' || r == '}' || r == ';' || r == '\n' || r == '\r':
			l.end(x)
			l.i++
		case r == '|':
			l.end(x)
			l.i++
			if l.peek(0) == '|' {
				l.i++
			} else {
				x.st.piped = true
			}
		case r == '&':
			l.ampersand(x)
		case r == '>' || r == '<':
			l.redirect(x)
		case unicode.IsSpace(r):
			l.flush(x)
			l.i++
		case r == ',':
			l.flush(x)
			x.nextComma = true
			l.i++
		case r == '=' && psAssigns(x):
			// An assignment: the statement starts again after it.
			target := x.cur.String()
			if !x.have {
				target = x.st.words[0].text
			}
			x.st = psStmt{assign: psEnvName(target)}
			x.cur.Reset()
			x.w, x.have = psWord{}, false
			l.i++
		default:
			x.cur.WriteRune(r)
			x.have = true
			l.i++
		}
	}
	l.end(x)
}

// escape reads a backtick: before a line break it continues the line,
// and otherwise the character it escapes cannot be told (`n is a line
// break).
func (l *psLexer) escape(x *psLexState) {
	if n := l.peek(1); n == '\n' || n == '\r' || n == 0 {
		l.flush(x)
		l.i += 2
		return
	}
	x.w.unknown, x.have = true, true
	x.cur.WriteRune(l.rs[l.i+1])
	l.i += 2
}

// single reads a single-quoted string: literal, a doubled quote standing
// for one.
func (l *psLexer) single(x *psLexState) {
	x.w.quoted, x.have = true, true
	for l.i++; l.i < len(l.rs); l.i++ {
		if psSingle(l.rs[l.i]) {
			if psSingle(l.peek(1)) {
				l.i++
				x.cur.WriteRune(l.rs[l.i])
				continue
			}
			break
		}
		x.cur.WriteRune(l.rs[l.i])
	}
	l.i++
}

// double reads a double-quoted string: a variable, a subexpression (whose
// statements are read) or an escape in it cannot be told.
func (l *psLexer) double(x *psLexState, depth int) {
	x.w.quoted, x.have = true, true
	l.i++
	for l.i < len(l.rs) && !psDouble(l.rs[l.i]) {
		switch c := l.rs[l.i]; {
		case c == '`' && l.i+1 < len(l.rs):
			x.w.unknown = true
			x.cur.WriteRune(l.rs[l.i+1])
			l.i += 2
		case c == '$' && l.peek(1) == '(':
			x.w.unknown = true
			x.cur.WriteString("$()")
			l.i += 2
			l.block(')', depth+1)
		case c == '$':
			x.w.unknown = true
			x.cur.WriteRune(c)
			l.i++
		default:
			x.cur.WriteRune(c)
			l.i++
		}
	}
	l.i++
}

// variable reads $: a subexpression $(...), whose statements are read, or
// a variable ($name, ${name}); either cannot be told.
func (l *psLexer) variable(x *psLexState, depth int) {
	if l.peek(1) == '(' {
		x.cur.WriteString("$()")
		x.w.unknown, x.w.bare, x.have = true, true, true
		l.i += 2
		l.block(')', depth+1)
		return
	}
	x.w.unknown, x.w.bare, x.have = true, true, true
	x.cur.WriteRune('$')
	l.i++
	if l.peek(0) == '{' {
		for ; l.i < len(l.rs) && l.rs[l.i] != '}'; l.i++ {
			x.cur.WriteRune(l.rs[l.i])
		}
		if l.i < len(l.rs) {
			x.cur.WriteRune('}')
			l.i++
		}
	}
}

// ampersand reads &: && ends a statement, & before a statement's first
// word is the call operator, and & after it runs the statement in the
// background.
func (l *psLexer) ampersand(x *psLexState) {
	switch {
	case l.peek(1) == '&':
		l.end(x)
		l.i += 2
	case !x.have && len(x.st.words) == 0:
		x.st.call = true
		l.i++
	default:
		l.end(x)
		l.i++
	}
}

// redirect reads a redirection: neither the stream number before it nor
// its target is an argument.
func (l *psLexer) redirect(x *psLexState) {
	if x.have && !x.w.quoted && !x.w.unknown && psStream(x.cur.String()) {
		x.cur.Reset()
		x.w, x.have = psWord{}, false
	}
	l.flush(x)
	l.i++
	if l.peek(0) == '>' {
		l.i++
	}
	if l.peek(0) == '&' {
		l.i += 2 // 2>&1
		return
	}
	x.dropNext = true
}

// psStream reports whether w is a stream number before a redirection.
func psStream(w string) bool { return w == "*" || (len(w) == 1 && w[0] >= '1' && w[0] <= '6') }

// psAssigns reports whether an "=" read now is an assignment: the
// statement so far is one word naming a variable.
func psAssigns(x *psLexState) bool {
	switch {
	case x.have && len(x.st.words) == 0:
		return x.w.unknown && !x.w.quoted && strings.Contains(x.cur.String(), "$")
	case !x.have && len(x.st.words) == 1:
		w := x.st.words[0]
		return w.unknown && !w.quoted && strings.Contains(w.text, "$")
	}
	return false
}

// psEnvName is the environment variable a variable reference names
// ($env:NAME, with the operator of += and the like), upper-cased; "" for
// any other variable.
func psEnvName(ref string) string {
	ref = strings.TrimRight(ref, "+-*/%?")
	name, ok := cutPrefixFold(ref, "$env:")
	if !ok {
		if name, ok = cutPrefixFold(ref, "${env:"); !ok {
			return ""
		}
	}
	return strings.ToUpper(strings.TrimSuffix(name, "}"))
}

// cutPrefixFold is strings.CutPrefix, ignoring case.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

// psName is the command a PowerShell word names, by its base name,
// lower-cased, without a program extension; "" when the word cannot be
// told.
func psName(w psWord) string {
	if w.unknown {
		return ""
	}
	name := strings.ToLower(baseName(w.text))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		if n, ok := strings.CutSuffix(name, ext); ok {
			return n
		}
	}
	return name
}

// psBareVariable reports whether a command word is a variable alone
// ($name or ${name}), which PowerShell runs as a command after & only, but
// whose value can be a program's path.
func psBareVariable(w psWord) bool {
	t := w.text
	if !strings.HasPrefix(t, "$") || len(t) < 2 || w.quoted {
		return false
	}
	return !strings.ContainsAny(t[1:], ".([$")
}

// psMovers are the commands that change PowerShell's working directory.
var psMovers = map[string]bool{
	"cd": true, "chdir": true, "sl": true, "set-location": true,
	"pushd": true, "push-location": true, "popd": true, "pop-location": true,
}

// psRunners are the programs whose arguments are a command line they run.
var psRunners = map[string]bool{
	"pwsh": true, "powershell": true, "cmd": true, "wsl": true, "bash": true, "sh": true,
	"zsh": true, "dash": true, "ksh": true, "fish": true,
}

// psStarters are Start-Process and its aliases.
var psStarters = map[string]bool{"start-process": true, "start": true, "saps": true}

// psAliasers are the commands that define an alias.
var psAliasers = map[string]bool{"set-alias": true, "new-alias": true, "sal": true, "nal": true}

// psInert are the commands that take a program's name as an argument
// without running it.
var psInert = map[string]bool{
	"get-command": true, "gcm": true, "get-help": true, "help": true, "man": true, "where": true,
	"where.exe": true, "which": true, "write-output": true, "echo": true, "write": true,
	"write-host": true, "select-string": true, "sls": true, "test-path": true, "get-process": true,
	"ps": true, "gps": true, "stop-process": true, "kill": true, "spps": true, "winget": true,
	"choco": true, "scoop": true, "get-item": true, "gi": true, "get-childitem": true, "gci": true,
	"ls": true, "dir": true, "resolve-path": true, "rvpa": true,
}

// psGitCall is a git command a PowerShell line runs: the words after git,
// and whether it may run from a directory other than the call's.
type psGitCall struct {
	args  []psWord
	moved bool
}

// psFound is what the gate reads in one PowerShell statement: the git
// commands it runs, the command lines it runs (Invoke-Expression,
// pwsh -Command, cmd /c), and, when it may run git in a way the gate
// cannot follow, why.
type psFound struct {
	calls  []psGitCall
	nested []string
	opaque string
	// moves is true when the statement starts a program in another
	// working directory (Start-Process -WorkingDirectory).
	moves bool
}

// psUnit is one statement of a PowerShell line, those of the command lines
// it nests included, in the order they run: the git commands it runs, and
// whether it can read files other than through git.
type psUnit struct {
	calls []psGitCall
	reads bool
}

// psLine is what the gate reads in a whole PowerShell line, the command
// lines nested in it included: a nested line shares the session of the
// line around it, so a location change or an environment assignment in
// either counts for both.
type psLine struct {
	units []psUnit
	moved bool
	env   []string
}

// collect reads line, nested depth deep, into p. It returns a deny for a
// line that cannot be read, or one that runs git in a way the gate cannot
// follow.
func (s *setup) collect(p *psLine, line string, depth int) (Decision, bool) {
	if depth > maxDepth {
		return deny(ReasonGitContent, "%s; it could run git printing content, and git history cannot be vetted. %s",
			whyTooDeep, hintPSPlain), true
	}
	stmts, ok := psStatements(line)
	if !ok {
		return deny(ReasonInternalError, "the PowerShell command holds more than %d words, or groups nested more than %d deep, "+
			"too many to judge", maxPSWords, maxPSGroups), true
	}
	p.env = append(p.env, psEnv(stmts)...)
	for _, st := range stmts {
		f := s.psStatement(st)
		if f.opaque != "" {
			return deny(ReasonGitContent, "git may run %s; git history cannot be vetted. %s", f.opaque, hintPSPlain), true
		}
		name := ""
		if len(st.words) > 0 {
			name = psName(st.words[0])
		}
		if psMovers[name] || f.moves {
			p.moved = true
		}
		// git's own reads are judged by the git rule, as for Bash.
		p.units = append(p.units, psUnit{calls: f.calls, reads: len(st.words) > 0 && name != "git"})
		for _, n := range f.nested {
			if d, ok := s.collect(p, n, depth+1); ok {
				return d, true
			}
		}
	}
	return Decision{}, false
}

// psGit decides rule 2 for a PowerShell line, and the rule against a
// command that changes the working tree before another reads it: each git
// command the line runs, directly, through the call operator, a script
// block, Start-Process, Invoke-Expression or a shell it starts, is judged
// as the Bash rule judges it (gitContent), with the location changes and
// environment assignments of the whole line, nested lines included. It
// returns a deny, or ok false.
func (s *setup) psGit(line string) (Decision, bool) {
	var p psLine
	if d, ok := s.collect(&p, line, 0); ok {
		return d, true
	}
	// git config setting a key that a git command anywhere in the line
	// reads, as for Bash.
	configured := false
	for _, u := range p.units {
		for _, c := range u.calls {
			argv, unknown, _ := psArgv(c, nil)
			configured = configured || configWrite(argv, unknown)
		}
	}
	if configured {
		for _, u := range p.units {
			for _, c := range u.calls {
				argv, _, _ := psArgv(c, nil)
				if sub := afterConfig(argv); sub != "" {
					return deny(ReasonGitContent, "%s", gitDenial{sub, whyAfterConfig, hintAfterConfig}.message()), true
				}
			}
		}
	}
	changed := ""
	seen := map[string]bool{}
	for _, u := range p.units {
		if changed != "" && u.reads {
			return deny(ReasonGitContent,
				"%s changes the working tree, and a later command in the line can read files; the read would be judged "+
					"against the tree as it was before. %s", changed, hintChange), true
		}
		for _, c := range u.calls {
			// The same git command is judged once.
			var key strings.Builder
			fmt.Fprint(&key, c.moved)
			for _, w := range c.args {
				fmt.Fprintf(&key, "\x00%t%s", w.unknown, w.text)
			}
			if seen[key.String()] {
				continue
			}
			seen[key.String()] = true
			if g := s.psJudge(c, p.env, p.moved); g != nil {
				return deny(ReasonGitContent, "%s", g.message()), true
			}
			if changed == "" {
				changed = s.psChanges(c)
			}
		}
	}
	return Decision{}, false
}

// hintPSPlain ends a refusal for git run in a way the gate cannot follow.
const hintPSPlain = "Run git directly, with its arguments written out in full."

// psStatement reads what one statement runs.
func (s *setup) psStatement(st psStmt) (f psFound) {
	words := st.words
	if len(words) > 1 && !words[0].quoted && words[0].text == "." {
		// The dot-source operator.
		words, st.call = words[1:], true
	}
	if len(words) == 0 {
		return f
	}
	for _, w := range words {
		if !w.quoted && psBuildsScript(w.text) {
			f.opaque = "in a script block built from a string, which the gate cannot read"
			return f
		}
	}
	cmd, args := words[0], words[1:]
	name := psName(cmd)
	switch {
	case cmd.unknown:
		// & $git log -p: the program cannot be told, and could be git.
		if (st.call || psBareVariable(cmd)) && len(args) > 0 {
			f.calls = append(f.calls, psGitCall{args: args})
		}
		return f
	case name == "git":
		f.calls = append(f.calls, psGitCall{args: args})
		return f
	case name == "invoke-expression" || name == "iex":
		if st.piped {
			f.opaque = "through Invoke-Expression, reading its command line from a pipe"
			return f
		}
		var parts []string
		for _, a := range args {
			if a.unknown {
				f.opaque = "through Invoke-Expression, whose command line the gate cannot read"
				return f
			}
			if strings.HasPrefix(a.text, "-") && !a.quoted {
				continue // -Command
			}
			parts = append(parts, a.text)
		}
		f.nested = append(f.nested, strings.Join(parts, " "))
		return f
	case psStarters[name]:
		return psStartProcess(args)
	case psAliasers[name]:
		for _, a := range args {
			if a.unknown || psName(a) == "git" {
				f.opaque = "under an alias, which the gate cannot follow"
				return f
			}
		}
		return f
	case psRunners[name]:
		if st.piped {
			f.opaque = "through a shell reading its commands from a pipe"
			return f
		}
		for _, a := range args {
			switch {
			case a.unknown:
				f.opaque = "through a shell given an argument the gate cannot read"
				return f
			case (name == "pwsh" || name == "powershell") && !a.quoted && psEncoded(a.text):
				f.opaque = "through an encoded PowerShell command, which the gate cannot read"
				return f
			case strings.ContainsAny(a.text, " \t;|&"):
				f.nested = append(f.nested, a.text)
			}
		}
	case psInert[name]:
		return f
	}
	// An unknown wrapper is judged by what it runs: git as the first word
	// after it that is not an option (cmd /c git log -p).
	for i, a := range args {
		if strings.HasPrefix(a.text, "-") || (strings.HasPrefix(a.text, "/") && !strings.ContainsAny(a.text[1:], `/\`)) {
			continue
		}
		if psName(a) == "git" {
			f.calls = append(f.calls, psGitCall{args: args[i+1:]})
		}
		break
	}
	return f
}

// psBuildsScript reports whether a word builds a script block from a
// string or runs a string as a script ([scriptblock]::Create,
// InvokeScript, NewScriptBlock).
func psBuildsScript(w string) bool {
	w = strings.ToLower(w)
	return strings.Contains(w, "scriptblock]::create") || strings.Contains(w, "invokescript") ||
		strings.Contains(w, "newscriptblock")
}

// psEncoded reports whether a pwsh word is its -EncodedCommand option, by
// any prefix PowerShell accepts.
func psEncoded(w string) bool {
	p := strings.ToLower(strings.TrimLeft(w, "-/"))
	if p == "" || !strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "/") {
		return false
	}
	return p == "e" || p == "ec" || p == "ea" || (len(p) >= 2 && psParam(p, "encodedcommand", "encodedarguments"))
}

// psStartProcess reads Start-Process's arguments: the program it starts
// (-FilePath, or the first positional), and its arguments (-ArgumentList,
// or the second positional), each element split as Windows splits a
// command line. A program that cannot be told is judged as git.
func psStartProcess(args []psWord) (f psFound) {
	var file *psWord
	var list []psWord
	moved := false
	positional := 0
	// value is the group of words at i: a word and those joined to it by
	// commas.
	value := func(i int) (group []psWord, next int) {
		if i >= len(args) {
			return nil, i
		}
		group = append(group, args[i])
		for i++; i < len(args) && args[i].comma; i++ {
			group = append(group, args[i])
		}
		return group, i
	}
	for i := 0; i < len(args); {
		a := args[i]
		if strings.HasPrefix(a.text, "-") && len(a.text) > 1 && !a.quoted && !a.unknown {
			p, attached, hasValue := strings.Cut(strings.ToLower(a.text[1:]), ":")
			var group []psWord
			switch {
			case psParam(p, "loaduserprofile", "lup", "nonewwindow", "nnw", "passthru", "wait", "usenewenvironment"):
				i++
				continue
			case hasValue:
				group = []psWord{{text: attached, unknown: a.unknown}}
				i++
			default:
				group, i = value(i + 1)
			}
			switch {
			case psParam(p, "filepath", "pspath", "path"):
				if len(group) > 0 {
					file = &group[0]
				}
			case psParam(p, "argumentlist", "args"):
				list = group
			case psParam(p, "workingdirectory"):
				moved = true
			}
			continue
		}
		group, next := value(i)
		i = next
		switch positional {
		case 0:
			file = &group[0]
		case 1:
			list = group
		}
		positional++
	}
	if file == nil {
		return f
	}
	name := psName(*file)
	var words []psWord
	for _, e := range list {
		if e.unknown {
			// Its value goes into the command line git splits.
			words = append(words, psWord{text: e.text, unknown: true, bare: true})
			continue
		}
		for _, t := range winFields(e.text) {
			words = append(words, psWord{text: t})
		}
	}
	switch {
	case file.unknown || name == "git":
		f.calls = append(f.calls, psGitCall{args: words, moved: moved})
	case psRunners[name] || name == "invoke-expression":
		if slices.ContainsFunc(words, func(w psWord) bool { return w.unknown }) {
			f.opaque = "through a shell given an argument the gate cannot read"
			return f
		}
		var parts []string
		for _, w := range words {
			parts = append(parts, w.text)
		}
		f.nested = append(f.nested, strings.Join(parts, " "))
		// The line it runs starts in that directory.
		f.moves = moved
	}
	return f
}

// psParam reports whether p, a parameter's name lower-cased without its
// dash, is a prefix of one of names, as PowerShell accepts it.
func psParam(p string, names ...string) bool {
	return p != "" && slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, p) })
}

// winFields splits a command line as a Windows program does, far enough
// for git's arguments: whitespace separates, and double quotes group.
func winFields(line string) []string {
	var out []string
	var cur strings.Builder
	have, quoted := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted, have = !quoted, true
		case unicode.IsSpace(r) && !quoted:
			if have {
				out = append(out, cur.String())
			}
			cur.Reset()
			have = false
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

// psEnv are the environment assignments of a line as git would see them,
// NAME=value: an assignment to $env:NAME of one known word, and, as one
// whose value cannot be told, any other word naming an environment
// variable git reads (Set-Item env:GIT_PAGER, a .NET call).
func psEnv(stmts []psStmt) []string {
	var env []string
	for _, st := range stmts {
		if st.assign != "" {
			value := "\x00"
			if len(st.words) == 1 && !st.words[0].unknown {
				value = st.words[0].text
			}
			env = append(env, st.assign+"="+value)
			continue
		}
		for _, w := range st.words {
			t := strings.ToUpper(w.text)
			for _, prefix := range []string{"$ENV:", "${ENV:", "ENV:\\", "ENV:/", "ENV:"} {
				if rest, ok := strings.CutPrefix(t, prefix); ok {
					t = strings.TrimSuffix(rest, "}")
					break
				}
			}
			named := t != strings.ToUpper(w.text) || w.quoted
			if named && isName(t) && unsafeEnv([]string{t + "=\x00"}) {
				env = append(env, t+"=\x00")
			}
		}
	}
	return env
}

// psArgv is a git call's command line for gitContent, the line's
// environment assignments first, and the words in it that cannot be told.
// After --% (stop parsing) PowerShell passes the rest as written, expanding
// %NAME%: a word holding % cannot be told. shapes says, for each of argv,
// whether its value may be several arguments: an expansion outside double
// quotes (psWord.bare), or %NAME% after --%, whose value the program's
// command line splits. PowerShell does not expand globs for a program.
func psArgv(c psGitCall, env []string) (argv, unknown []string, shapes []argShape) {
	argv = append(slices.Clone(env), "git")
	shapes = make([]argShape, len(argv))
	stop := false
	for _, w := range c.args {
		if !w.quoted && w.text == "--%" {
			stop = true
			continue
		}
		argv = append(argv, w.text)
		pct := stop && strings.Contains(w.text, "%")
		shapes = append(shapes, argShape{splits: w.bare || pct})
		if w.unknown || pct {
			unknown = append(unknown, w.text)
		}
	}
	return argv, unknown, shapes
}

// psJudge judges one git call: from the call's working directory, and when
// the line may change it (cd, Set-Location, Start-Process
// -WorkingDirectory), from each protected root and the repository's root
// as well, as the Bash rule judges a working directory it cannot tell.
func (s *setup) psJudge(c psGitCall, env []string, moved bool) *gitDenial {
	argv, unknown, shapes := psArgv(c, env)
	dirs := []string{s.cwd}
	if moved || c.moved {
		dirs = append(dirs, s.repo.Root)
		for _, root := range s.policy.ProtectedRoots() {
			dirs = append(dirs, filepath.Join(s.repo.Root, filepath.FromSlash(root)))
		}
	}
	for _, d := range dirs {
		in := *s
		in.cwd = d
		if g := in.gitContent(argv, unknown, shapes); g != nil {
			return g
		}
	}
	return nil
}

// psChanges is "git SUB" when a git call changes the working tree, as the
// Bash rule reads it (treeChange), and "" otherwise.
func (s *setup) psChanges(c psGitCall) string {
	argv, unknown, _ := psArgv(c, nil)
	inv, ok := parseGit(argv[1:], s.cwd)
	switch {
	case !ok:
		return ""
	case slices.Contains(unknown, inv.sub) || !gitBuiltins[inv.sub]:
		return "git"
	case s.branchOnly(inv):
		return ""
	case gitChangesTree(inv.sub, inv.rest):
		return "git " + inv.sub
	}
	return ""
}
