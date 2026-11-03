package gate

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// partKind is what one part of a word is.
type partKind int

const (
	// litPart is literal text.
	litPart partKind = iota
	// paramPart is a parameter expansion, $NAME or ${NAME}; text is NAME.
	paramPart
	// tildePart is a leading unquoted ~ or ~user; text is the user, "" for
	// the current one.
	tildePart
	// opaquePart is an expansion whose value cannot be told from the text:
	// a command substitution, a positional or special parameter, or a
	// ${...} with an operator.
	opaquePart
)

// part is one piece of a word.
type part struct {
	kind partKind
	// text is the literal text, the parameter's name or the tilde's user.
	text string
	// raw is the part as written, for an expansion.
	raw string
	// quoted is true for literal text that was quoted or escaped, where a
	// glob character is literal.
	quoted bool
	// inDouble is true for an expansion inside double quotes, which the
	// shell does not split into words.
	inDouble bool
}

// word is one word of a simple command, with its quotes removed.
type word struct {
	// text is the word with quotes removed and expansions as written.
	text string
	// parts are its pieces, in order.
	parts []part
	// quoted is true when any part of the word was quoted or escaped.
	quoted bool
	// splits is true when an expansion in the word ($X, ${X}, $(...),
	// backticks, $1) is outside double quotes: the shell splits its value
	// into words, so the word may become several arguments.
	splits bool
	// braceUnknown is set for a word whose brace expansion the gate does
	// not expand (more than maxBraceWords words): it is unknown, and
	// splits.
	braceUnknown bool
	// redirect is true for a redirection's target, which is not an
	// argument of the command.
	redirect bool
	// input is true for the target of a redirection that reads (<, <>);
	// here for a here-string's (<<<), which is the command's input itself,
	// not a file.
	input, here bool
	// rw is true for a redirection that reads and writes (<>).
	rw bool
}

// heredoc is one here-document, <<WORD or <<-WORD.
type heredoc struct {
	// quoted is true when any part of the delimiter was quoted: the body is
	// then pure data. An unquoted body expands, and its command
	// substitutions run.
	quoted bool
	// body is the text between the operator's line and the delimiter line,
	// leading tabs removed for <<-.
	body string
	// subs are the bodies of the command substitutions in an unquoted
	// body, each a command line of its own.
	subs []string
}

// simple is one simple command: its words in order, redirection targets
// included and marked.
type simple struct {
	words []word
	// pipeIn is true when the command reads the previous one's output
	// through | or |&.
	pipeIn bool
	// subs are the bodies of the command substitutions in its words,
	// $(...) and `...`, each a command line of its own.
	subs []string
	// docs are its here-documents, in order.
	docs []*heredoc
	// fallback is true when the line it is in did not tokenize (unbalanced
	// quotes, a trailing backslash): its words are only approximate.
	fallback bool
	// funcDef is true for the name of a function definition, name().
	funcDef bool
	// overflow is set when brace expansion in the line made more words
	// than the gate judges (maxBraceLine).
	overflow bool
	// procArg is set for a command given a process substitution (cmd
	// <(...)), which it reads or writes as a file.
	procArg bool
	// arith is set for an arithmetic command, ((...)), that names a
	// variable.
	arith bool
	// background is set for a command run in the background (cmd &).
	background bool
	// uncertain says, on every command of a line the tokenizer cannot
	// read as bash would, which construct it is, in fixed words: what the
	// line runs cannot be told.
	uncertain string
	// procSub is true for a command inside a process substitution,
	// <(...) or >(...), which starts before or alongside the command it
	// is given to.
	procSub bool
	// arrayElems is true for the words of a compound array assignment,
	// a=(...): each becomes an element's value.
	arrayElems bool
	// pipeOut is set by the walker for a command whose output the next
	// command in its pipeline reads.
	pipeOut bool
}

// args is the command's words without redirection targets.
func (s simple) args() []string {
	var out []string
	for _, w := range s.words {
		if !w.redirect {
			out = append(out, w.text)
		}
	}
	return out
}

// splitCommands splits a shell command line into simple commands at ;, &&,
// ||, |, |&, &, parentheses and newlines, the way a shell reads it, without
// expanding anything. Quotes are removed from words, $'...' is decoded as
// bash decodes it, and $"..." is read as "...". A command substitution's
// body is kept for the command it is in; its word holds an opaque part. A
// redirection's target is kept as a word marked redirect. A comment runs to
// the end of its line. A here-document's body is read as bash reads it,
// from the line after its operator to the delimiter line, and kept with its
// command as data (heredoc), never as commands. With unbalanced quotes, or
// a trailing backslash, the line is split on whitespace, newlines and those
// operators instead, so nothing is hidden inside an unterminated quote, and
// every command is marked fallback.
func splitCommands(line string) []simple {
	if cmds, ok := tokenize(line); ok {
		return cmds
	}
	return splitFields(line)
}

// tokenizer is the state of tokenize.
type tokenizer struct {
	rs       []rune
	cmds     []simple
	cur      simple
	parts    []part
	raw      strings.Builder // text of the word being read
	inWord   bool
	quoted   bool
	redirect bool // the word being read is a redirection's target
	input    bool // the redirection reads
	here     bool // the redirection is a here-string
	rw       bool // the redirection reads and writes (<>)
	pipe     bool // the next command reads this one's output
	// litBuf is the literal part being read, open when litOpen.
	litBuf    strings.Builder
	litOpen   bool
	litQuoted bool
	// inDouble is set while a double-quoted string is read.
	inDouble bool
	// pending are the here-documents whose bodies start after the next
	// newline.
	pending []pendingDoc
	// parens are the open parentheses, each true for a process
	// substitution, <(...) or >(...).
	parens []bool
	// arrays runs beside parens: true for one that opens a compound array
	// assignment, a=(...).
	arrays []bool
	// braceWords counts the words brace expansion made in the line.
	braceWords int
	// cases counts the case commands open (after "case WORD in", before
	// "esac"), and casePattern is set while a pattern is read, whose ")"
	// closes no parenthesis.
	cases       int
	casePattern bool
	// patternParen is set when the case pattern being read opened with
	// "(": then esac in it is a pattern, not the case's end.
	patternParen bool
	// patternStart is where the pattern being read starts among the
	// command's words: after "case WORD in" when on the same line.
	patternStart int
	// sc is how deeply this tokenizer's text is nested in command
	// substitutions (substEnd), with the memo the line's readers share.
	sc lineScan
	// uncertain says why the tokenizer cannot be sure what the line runs
	// (a here-document never closed), "" when it can.
	uncertain string
}

// doubt records why the tokenizer cannot read the line as bash would: the
// first reason given is kept.
func (t *tokenizer) doubt(why string) {
	if t.uncertain == "" {
		t.uncertain = why
	}
}

// The constructs the tokenizer cannot read as bash would, in fixed words.
const (
	whyUnclosed   = "the command line leaves a case, a parenthesis or a here-document open, so what it runs cannot be told"
	whyStrayParen = "a ) closes nothing"
	whyDelim      = "a here-document delimiter holds a $, a backtick or a newline"
	whyBodyJoin   = "a backslash-newline in an unquoted here-document body changes which line ends it, or splits a $( or a backtick"
	whyBash52     = "a here-document inside a command substitution has a line that starts with its delimiter, " +
		"which versions of bash read differently"
	whyRejoin        = "a backslash-newline is left after the lines it joins are joined"
	whySplitRedirect = "a backslash-newline splits a redirection or here-document operator"
	whyJoinNotCode   = "a backslash-newline the gate took for a join is inside quotes, a comment or a quoted here-document body"
)

// pendingDoc is a here-document whose body is still to be read.
type pendingDoc struct {
	doc   *heredoc
	delim string
	strip bool // <<-: leading tabs are removed
}

func (t *tokenizer) lit(s string, quoted bool) {
	t.inWord = true
	t.quoted = t.quoted || quoted
	t.raw.WriteString(s)
	if t.litOpen && t.litQuoted == quoted {
		t.litBuf.WriteString(s)
		return
	}
	t.flushLit()
	t.litOpen, t.litQuoted = true, quoted
	t.litBuf.WriteString(s)
}

// flushLit ends the literal part being read, if any.
func (t *tokenizer) flushLit() {
	if t.litOpen {
		t.parts = append(t.parts, part{kind: litPart, text: t.litBuf.String(), quoted: t.litQuoted})
	}
	t.litBuf.Reset()
	t.litOpen = false
}

func (t *tokenizer) expansion(kind partKind, text, raw string) {
	if arithContext(raw) {
		// In double quotes too: "${a[x]}" evaluates x.
		t.sc.arithmetic()
	}
	t.inWord = true
	t.raw.WriteString(raw)
	t.flushLit()
	t.parts = append(t.parts, part{kind: kind, text: text, raw: raw, inDouble: t.inDouble})
}

// arithContext reports whether the expansion raw evaluates arithmetic that
// can name a variable (whose value is evaluated in turn): $((...)) or
// $[...] with a name or an expansion in it, a subscript ${a[...]} or a
// substring offset ${a:...} (not a default, ${a:-x}) with one.
func arithContext(raw string) bool {
	named := func(s string) bool {
		return strings.ContainsFunc(s, func(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) })
	}
	switch {
	case strings.HasPrefix(raw, "$((") || strings.HasPrefix(raw, "$["):
		return named(raw[2:])
	case strings.HasPrefix(raw, "${"):
		inner := strings.TrimSuffix(raw[2:], "}")
		inner = strings.TrimLeft(inner, "#!")
		k := 0
		for k < len(inner) && (isNameRune(rune(inner[k])) || k == 0 && strings.ContainsRune("@*#?$!-", rune(inner[k]))) {
			k++
			if k == 1 && !isNameRune(rune(inner[0])) {
				break
			}
		}
		rest := inner[k:]
		if strings.HasPrefix(rest, "[") {
			end := strings.IndexByte(rest, ']')
			if end < 0 || named(rest[1:end]) {
				// A subscript the gate cannot end is taken to evaluate.
				return true
			}
			rest = rest[end+1:] // ${a[@]:x}: a substring may follow
		}
		if strings.HasPrefix(rest, ":") && len(rest) > 1 && !strings.ContainsRune("-=?+", rune(rest[1])) && named(rest[1:]) {
			return true
		}
		// An operator's operand may hold expansions of its own:
		// ${x:-${s:v}}, ${s#${s:v}}, ${s/x/${s:v}}.
		return nestedArith(rest)
	}
	return false
}

// nestedArith reports whether s, an expansion's operand, holds an
// arithmetic context (arithContext) in a nested ${...}, $((...)) or
// $[...]. A nested ${ whose end cannot be found is taken to hold one.
func nestedArith(s string) bool {
	for k := 0; k < len(s); k++ {
		switch {
		case strings.HasPrefix(s[k:], "$((") || strings.HasPrefix(s[k:], "$["):
			if arithContext(s[k:]) {
				return true
			}
		case strings.HasPrefix(s[k:], "${"):
			depth, end := 0, -1
			for j := k + 1; j < len(s) && end < 0; j++ {
				switch s[j] {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						end = j
					}
				}
			}
			if end < 0 {
				return true
			}
			if arithContext(s[k : end+1]) {
				return true
			}
			k = end // read once: the nested one scanned its own operands
		}
	}
	return false
}

// subscriptAssign matches an assignment to an array element, NAME[...]=
// or NAME[...]+=, and a compound assignment's [...]= element.
var subscriptAssign = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*)?\[([^\]]*)\]\+?=`)

// splitsQuoted reports whether the expansion raw yields several words even
// inside double quotes: "$@", "${@:2}", "${a[@]}", "${!p@}", "${!a[@]}".
// Any ${...} holding @ is taken to. "$*" and "${*}" join into one word.
func splitsQuoted(raw string) bool {
	return raw == "$@" || strings.HasPrefix(raw, "${") && strings.Contains(raw, "@") ||
		strings.HasPrefix(raw, "${(") // zsh parameter flags: (f), (s: :), (@) split
}

// globs reports whether the word holds a glob character outside quotes,
// which the shell expands into the names it matches.
func (w word) globs() bool {
	return slices.ContainsFunc(w.parts, func(p part) bool { return p.kind == litPart && !p.quoted && hasGlob(p.text) })
}

// maxBraceWords is the most words the gate expands one word into by brace
// expansion. Past it, the word is unknown.
const maxBraceWords = 64

// maxBraceRunes is the longest word, in runes and expansions, the gate
// brace-expands: past it, a word holding a brace is unknown.
const maxBraceRunes = 4096

// maxBraceLine bounds the words brace expansion makes in one command
// line; past it the line is denied as too large to judge.
const maxBraceLine = 20000

// btok is one piece of a word for brace expansion: a literal rune, quoted
// or not, or a whole expansion part, which brace expansion never splits.
type btok struct {
	r      rune
	quoted bool
	exp    *part
}

// is reports whether t is the unquoted literal rune r.
func (t btok) is(r rune) bool { return t.exp == nil && !t.quoted && t.r == r }

// braceToks are a word's parts as brace-expansion pieces.
func braceToks(parts []part) []btok {
	var toks []btok
	for i := range parts {
		p := &parts[i]
		if p.kind != litPart {
			toks = append(toks, btok{exp: p})
			continue
		}
		for _, r := range p.text {
			toks = append(toks, btok{r: r, quoted: p.quoted})
		}
	}
	return toks
}

// braceParts rebuilds a word's parts and text from brace-expansion pieces.
func braceParts(toks []btok) ([]part, string) {
	var parts []part
	var text, lit strings.Builder
	quoted, open := false, false
	flush := func() {
		if open {
			parts = append(parts, part{kind: litPart, text: lit.String(), quoted: quoted})
		}
		lit.Reset()
		open = false
	}
	for _, t := range toks {
		if t.exp != nil {
			flush()
			parts = append(parts, *t.exp)
			text.WriteString(t.exp.raw)
			continue
		}
		if open && quoted != t.quoted {
			flush()
		}
		open, quoted = true, t.quoted
		lit.WriteRune(t.r)
		text.WriteRune(t.r)
	}
	flush()
	return parts, text.String()
}

// braceExpand expands toks as bash's brace expansion does: an unquoted
// {...} with an unquoted comma at its top level gives each of its
// alternatives ({a,b}, x{,.bak}, nested ones expanded too), and {x..y} or
// {x..y..step} a sequence of integers (zero-padded when an end is) or of
// single letters. Any other brace is literal ({a}, {}, {a..}, a group's
// { and }). ok is false past maxBraceWords words.
func braceExpand(toks []btok) (out [][]btok, ok bool) {
	for i := range toks {
		if !toks[i].is('{') {
			continue
		}
		depth, end := 0, -1
		var commas []int
	scan:
		for k := i; k < len(toks); k++ {
			switch {
			case toks[k].is('{'):
				depth++
			case toks[k].is('}'):
				depth--
				if depth == 0 {
					end = k
					break scan
				}
			case toks[k].is(',') && depth == 1:
				commas = append(commas, k)
			}
		}
		if end < 0 {
			return [][]btok{toks}, true
		}
		var alts [][]btok
		if len(commas) > 0 {
			start := i + 1
			for _, c := range append(commas, end) {
				alts = append(alts, toks[start:c])
				start = c + 1
			}
		} else {
			seq, isSeq, fits := braceSequence(toks[i+1 : end])
			if !isSeq {
				continue
			}
			if !fits {
				return nil, false
			}
			for _, s := range seq {
				var a []btok
				for _, r := range s {
					a = append(a, btok{r: r})
				}
				alts = append(alts, a)
			}
		}
		rest, ok := braceExpand(toks[end+1:])
		if !ok {
			return nil, false
		}
		for _, a := range alts {
			inner, ok := braceExpand(a)
			if !ok || len(out)+len(inner)*len(rest) > maxBraceWords {
				return nil, false
			}
			for _, x := range inner {
				for _, r := range rest {
					out = append(out, slices.Concat(toks[:i], x, r))
				}
			}
		}
		return out, true
	}
	return [][]btok{toks}, true
}

// braceSequence reads toks, the inside of {...}, as a sequence: two
// integers or two single letters, and an optional integer step, separated
// by "..". isSeq is false when it is not one, and fits false when it has
// more than maxBraceWords elements.
func braceSequence(toks []btok) (seq []string, isSeq, fits bool) {
	var b strings.Builder
	for _, t := range toks {
		if t.exp != nil || t.quoted {
			return nil, false, false
		}
		b.WriteRune(t.r)
	}
	ends := strings.Split(b.String(), "..")
	if len(ends) != 2 && len(ends) != 3 {
		return nil, false, false
	}
	// Every number is an int64; past that range the word is not
	// expanded. Counts are taken in uint64, which cannot overflow for two
	// int64 ends, and the walk runs a counted number of steps.
	step := uint64(1)
	if len(ends) == 3 {
		if !isInteger(ends[2]) {
			return nil, false, false
		}
		n, err := strconv.ParseInt(ends[2], 10, 64)
		if err != nil {
			return nil, true, false
		}
		step = max(absInt64(n), 1)
	}
	span := func(lo, hi int64) (uint64, bool) {
		if lo <= hi {
			return uint64(hi) - uint64(lo), true //nolint:gosec // G115: two's complement difference of int64 ends fits a uint64
		}
		return uint64(lo) - uint64(hi), false //nolint:gosec // G115: as above
	}
	walk := func(lo, hi int64, f func(int64)) bool {
		d, up := span(lo, hi)
		count := d/step + 1
		if count > maxBraceWords {
			return false
		}
		for k := range count {
			off := k * step
			if up {
				f(int64(uint64(lo) + off)) //nolint:gosec // within [lo, hi]: no overflow
			} else {
				f(int64(uint64(lo) - off)) //nolint:gosec // within [hi, lo]: no overflow
			}
		}
		return true
	}
	switch {
	case isInteger(ends[0]) && isInteger(ends[1]):
		lo, errLo := strconv.ParseInt(ends[0], 10, 64)
		hi, errHi := strconv.ParseInt(ends[1], 10, 64)
		if errLo != nil || errHi != nil {
			return nil, true, false
		}
		width := 0
		for _, e := range ends[:2] {
			if digits := strings.TrimLeft(e, "+-"); len(digits) > 1 && digits[0] == '0' {
				width = max(len(ends[0]), len(ends[1]))
			}
		}
		if !walk(lo, hi, func(v int64) { seq = append(seq, fmt.Sprintf("%0*d", width, v)) }) {
			return nil, true, false
		}
	case isLetter(ends[0]) && isLetter(ends[1]):
		a, z := int64(ends[0][0]), int64(ends[1][0])
		if !walk(a, z, func(v int64) { seq = append(seq, asciiLetters[v-'A':v-'A'+1]) }) {
			return nil, true, false
		}
	default:
		return nil, false, false
	}
	return seq, true, true
}

// isInteger reports whether s is an optional sign and decimal digits.
func isInteger(s string) bool {
	s = strings.TrimLeft(s, "+-")
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// absInt64 is |n| as a uint64, MinInt64's included.
func absInt64(n int64) uint64 {
	if n < 0 {
		return uint64(-(n + 1)) + 1 //nolint:gosec // -(n+1) is non-negative
	}
	return uint64(n)
}

// asciiLetters are the characters from A to z, in order, which a sequence
// of letters runs through.
const asciiLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz"

// isLetter reports whether s is one ASCII letter.
func isLetter(s string) bool {
	return len(s) == 1 && ('a' <= s[0] && s[0] <= 'z' || 'A' <= s[0] && s[0] <= 'Z')
}

// wordSplits reports whether parts hold an expansion the shell splits into
// words: one outside double quotes, one that splits inside them too
// (splitsQuoted), or zsh's "$a[@]", a name followed by a subscript.
func wordSplits(parts []part) bool {
	for i, p := range parts {
		if p.kind != paramPart && p.kind != opaquePart {
			continue
		}
		if !p.inDouble || splitsQuoted(p.raw) {
			return true
		}
		if p.kind == paramPart && !strings.HasPrefix(p.raw, "${") && i+1 < len(parts) &&
			parts[i+1].kind == litPart && strings.HasPrefix(parts[i+1].text, "[") {
			return true
		}
	}
	return false
}

// dropWord forgets the word being read.
func (t *tokenizer) dropWord() {
	t.raw.Reset()
	t.litBuf.Reset()
	t.litOpen = false
	t.parts = nil
	t.inWord = false
}

func (t *tokenizer) endWord() {
	t.flushLit()
	if t.inWord && !t.redirect && !t.quoted {
		// An array element's subscript is arithmetic: b[x]=1, a=([x]=1).
		if m := subscriptAssign.FindStringSubmatch(t.raw.String()); m != nil &&
			strings.ContainsFunc(m[1], func(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) }) {
			t.sc.arithmetic()
		}
	}
	if t.inWord {
		w := word{
			text: t.raw.String(), parts: t.parts, quoted: t.quoted, splits: wordSplits(t.parts),
			redirect: t.redirect, input: t.redirect && t.input, here: t.redirect && t.here, rw: t.redirect && t.rw,
		}
		// Brace expansion makes the word several, each a word of its own;
		// past maxBraceWords it is one the gate does not expand.
		var toks []btok
		if strings.Contains(w.text, "{") {
			toks = braceToks(t.parts)
		}
		words, ok := [][]btok{toks}, true
		switch {
		case !slices.ContainsFunc(toks, func(b btok) bool { return b.is('{') }):
		case len(toks) > maxBraceRunes || t.braceWords > maxBraceLine:
			// Too long to expand in bounded time.
			ok = false
		default:
			words, ok = braceExpand(toks)
			t.braceWords += len(words)
		}
		if t.braceWords > maxBraceLine {
			// Past the line's budget: the line is denied (simple.overflow).
			t.cur.overflow = true
		}
		switch {
		case !ok:
			w.splits, w.braceUnknown = true, true
			t.cur.words = append(t.cur.words, w)
		case len(words) == 1 && len(words[0]) == len(toks):
			t.cur.words = append(t.cur.words, w)
		default:
			for _, toks := range words {
				if len(toks) == 0 {
					// bash drops a word brace expansion leaves empty:
					// {grep,} -r x is grep -r x, then -r x.
					continue
				}
				x := w
				x.parts, x.text = braceParts(toks)
				x.splits = wordSplits(x.parts)
				t.cur.words = append(t.cur.words, x)
			}
		}
		t.redirect, t.input, t.here, t.rw = false, false, false, false
		t.caseWord()
	}
	t.raw.Reset()
	t.parts = nil
	t.inWord, t.quoted = false, false
}

// caseWord follows case commands from the word just read: "case WORD in"
// opens one, whose patterns follow, and "esac" as a command's first word
// closes it.
func (t *tokenizer) caseWord() {
	ws := t.cur.words
	last := ws[len(ws)-1]
	switch {
	case last.quoted:
	case last.text == "esac" && t.cases > 0 && !t.patternParen &&
		(leadingKeywords(ws[:len(ws)-1]) == len(ws)-1 || t.casePattern && len(ws) >= 2 && ws[len(ws)-2].text == "in"):
		// esac as a command word, or right after "case WORD in".
		t.cases--
		t.casePattern = false
	case last.text == "in" && len(ws) >= 3 && ws[len(ws)-3].text == "case" && !ws[len(ws)-3].quoted &&
		leadingKeywords(ws[:len(ws)-3]) == len(ws)-3:
		// case as the command word, after any reserved words.
		t.cases++
		t.casePattern, t.patternStart = true, len(ws)
	}
}

// leadingKeywords counts the unquoted reserved words and grouping tokens
// ws starts with.
func leadingKeywords(ws []word) int {
	n := 0
	for n < len(ws) && !ws[n].quoted && (keywords[ws[n].text] || ws[n].text == "time") {
		n++
	}
	return n
}

// definesFunction reports whether the "(" at i, after a command of one
// word (the name, of any characters), opens "()": a function definition,
// name() or name ( ).
func (t *tokenizer) definesFunction(i int) bool {
	// The words after reserved words (if, then, {, !, time ...).
	words := len(t.cur.words) - leadingKeywords(t.cur.words)
	if t.inWord {
		words++
	}
	if words != 1 || t.redirect || (t.inWord && strings.HasSuffix(t.raw.String(), "=")) {
		// a=() is an empty array.
		return false
	}
	for j := i + 1; j < len(t.rs); j++ {
		switch t.rs[j] {
		case ' ', '\t':
			continue
		case ')':
			return true
		}
		return false
	}
	return false
}

func (t *tokenizer) endCommand() {
	t.endWord()
	if len(t.cur.words) > 0 || len(t.cur.subs) > 0 || len(t.cur.docs) > 0 || t.cur.arith {
		t.cur.pipeIn = t.pipe
		t.cur.procSub = slices.Contains(t.parens, true)
		t.cur.arrayElems = len(t.arrays) > 0 && t.arrays[len(t.arrays)-1]
		t.cmds = append(t.cmds, t.cur)
		t.pipe = false
	}
	t.cur = simple{}
	t.redirect, t.input, t.here, t.rw = false, false, false, false
}

// tokenize is splitCommands for a line whose quotes balance; ok is false
// when they do not.
//
// A backslash-newline outside single quotes, comments and quoted
// here-document bodies is removed before bash reads the line, wherever it
// is: "&&\<newline>x" is "&&x", "&\<newline>&" is "&&" and
// "$\<newline>(" is "$(". The tokenizer reads the line once to mark where
// no join is made (lineJoins); when there are joins elsewhere, the line is
// read again with them removed, as bash reads it.
func tokenize(line string) (cmds []simple, ok bool) {
	rs := []rune(line)
	t := &tokenizer{rs: rs, sc: newScan()}
	_, ok = t.run(0, false)
	if joins := lineJoins(rs, t.sc.memo.notCode); len(joins) > 0 {
		joined := make([]rune, 0, len(rs))
		doubt := t.sc.memo.joinDoubt
		// Where each join was, in the joined line: between joined[j-1]
		// and joined[j].
		var at []int
		for k := 0; k < len(rs); k++ {
			if joins[&rs[k]] {
				if k > 0 && k+2 < len(rs) && splitsRedirect(rs[k-1], rs[k+2]) {
					// A redirection or here-document operator split: what
					// follows may be a body bash does not join.
					doubt = cmp.Or(doubt, whySplitRedirect)
				}
				at = append(at, len(joined))
				k++ // the backslash, then the newline
				continue
			}
			joined = append(joined, rs[k])
		}
		t = &tokenizer{rs: joined, sc: newScan()}
		_, ok = t.run(0, false)
		if len(lineJoins(joined, t.sc.memo.notCode)) > 0 {
			doubt = cmp.Or(doubt, whyRejoin)
		}
		// Each join removed must be where the joined line is code: one
		// inside single quotes, a comment or a quoted here-document body
		// was not a join (the first reading misread the line).
		for _, j := range at {
			if j > 0 && t.sc.memo.notCode[&joined[j-1]] || j < len(joined) && t.sc.memo.notCode[&joined[j]] {
				doubt = cmp.Or(doubt, whyJoinNotCode)
			}
		}
		if ok && doubt != "" {
			t.doubt(doubt)
		}
	} else if ok {
		t.doubt(t.sc.memo.joinDoubt)
	}
	if !ok {
		return nil, false
	}
	return t.finish(), true
}

// splitsRedirect reports whether a backslash-newline between before and
// after can join them into one redirection operator (<<, >>, <>, >&, <&,
// &>, >|, <(, >(, <<-): an operator character on both sides, one of them
// < or >. A blank on either side splits nothing.
func splitsRedirect(before, after rune) bool {
	return strings.ContainsRune("<>&", before) && strings.ContainsRune("<>&|(-", after) &&
		(before == '<' || before == '>' || after == '<' || after == '>')
}

// lineJoins are the backslashes of the backslash-newlines bash removes in
// rs, by address: every one outside the text a reading of rs marked as no
// code (notCode: single quotes, $'...', comments, quoted here-document
// bodies), wherever else it is (arithmetic, double quotes, ${...},
// backticks included). A backslash escapes the character after it, so
// \\ before a newline is no join.
func lineJoins(rs []rune, notCode map[*rune]bool) map[*rune]bool {
	joins := map[*rune]bool{}
	for k := 0; k+1 < len(rs); k++ {
		if notCode[&rs[k]] || rs[k] != '\\' {
			continue
		}
		if rs[k+1] == '\n' {
			joins[&rs[k]] = true
		}
		k++
	}
	return joins
}

// lineScan is what the readers of one line share: how deeply the text read is
// nested in command substitutions, and the memo of the ends already
// found, keyed by the address of the first rune read, so that nested
// constructs are read once each, in linear time, however a reader that
// failed (an arithmetic expression that turns out to be a subshell)
// hands the text to another.
type lineScan struct {
	depth int
	memo  *scanMemo
}

// scanMemo remembers each reader's answer for a start in the line and a
// depth. An end is kept as an offset from the start, since readers are
// handed slices of the line.
type scanMemo struct {
	subst map[scanKey]substResult
	arith map[scanKey]arithResult
	brace map[scanKey]int
	// joins are the backslashes of the backslash-newlines bash removes
	// (tokenize), by address, and joinDoubt why one of them cannot be
	// removed safely ("" when all can).
	joins     map[*rune]bool
	joinDoubt string
	// notCode marks the runes read as text no join is made in: inside
	// single quotes and $'...', a comment, a quoted here-document body
	// with its delimiter line.
	notCode map[*rune]bool
	// namedArith is set when the line holds arithmetic that can name a
	// variable, anywhere (arithContext, an array element's subscript).
	namedArith bool
}

// arithmetic records that the line evaluates arithmetic that can name a
// variable.
func (sc lineScan) arithmetic() {
	if sc.memo != nil {
		sc.memo.namedArith = true
	}
}

type scanKey struct {
	at    *rune
	depth int
}

type substResult struct {
	end       int
	uncertain string
	ok        bool
}

type arithResult struct {
	end int
	ok  bool
}

func newScan() lineScan {
	return lineScan{memo: &scanMemo{
		subst: map[scanKey]substResult{}, arith: map[scanKey]arithResult{}, brace: map[scanKey]int{},
		joins: map[*rune]bool{}, notCode: map[*rune]bool{},
	}}
}

// notCode marks rs[from:to] as text bash makes no join in.
func (sc lineScan) notCode(rs []rune, from, to int) {
	if sc.memo == nil {
		return
	}
	for k := max(from, 0); k < min(to, len(rs)); k++ {
		sc.memo.notCode[&rs[k]] = true
	}
}

// doubtJoin records that a backslash-newline in a here-document body
// cannot be joined safely (bodyJoins).
func (sc lineScan) doubtJoin() {
	if sc.memo != nil && sc.memo.joinDoubt == "" {
		sc.memo.joinDoubt = whyBodyJoin
	}
}

// join notes the backslash at rs[i], before a newline, as one bash
// removes with the newline.
func (sc lineScan) join(rs []rune, i int) {
	if sc.memo != nil {
		sc.memo.joins[&rs[i]] = true
	}
}

// key is the memo's key for a reader starting at rs[i], ok false past the
// end.
func (sc lineScan) key(rs []rune, i int) (scanKey, bool) {
	if i < 0 || i >= len(rs) || sc.memo == nil {
		return scanKey{}, false
	}
	return scanKey{&rs[i], sc.depth}, true
}

// deeper is sc one command substitution deeper.
func (sc lineScan) deeper() lineScan {
	sc.depth++
	return sc
}

// maxSubstDepth bounds how deeply command substitutions nest in what one
// tokenizer reads: past it the line does not tokenize, and is split on
// whitespace instead (splitFields), which fails closed.
const maxSubstDepth = 64

// substEnd reads the body of a command substitution, $(...), starting at
// i just after "$(", with the tokenizer itself, as bash reads it: quotes,
// ${...}, backticks, nested substitutions, comments, here-documents and
// case patterns are all read as they are elsewhere, and the ")" that ends
// it is the first one at the body's top level. It returns that index.
// uncertain says why when the body cannot be read as bash would (it does
// not end at its plain top level).
func substEnd(rs []rune, i int, sc lineScan) (end int, uncertain string, ok bool) {
	if sc.depth > maxSubstDepth {
		return 0, "", false
	}
	key, memo := sc.key(rs, i)
	if memo {
		if r, seen := sc.memo.subst[key]; seen {
			return i + r.end, r.uncertain, r.ok
		}
	}
	t := &tokenizer{rs: rs, sc: sc}
	end, ok = t.run(i, true)
	if ok {
		uncertain = t.uncertain
		if uncertain == "" && (t.cases > 0 || t.casePattern || len(t.pending) > 0) {
			uncertain = whyUnclosed
		}
	} else {
		end = 0
	}
	if memo {
		sc.memo.subst[key] = substResult{end - i, uncertain, ok}
	}
	return end, uncertain, ok
}

// run reads rs from start. With sub set it reads a command substitution's
// body and stops at the ")" that closes it, whose index it returns; ok is
// false when it never comes, or a quote does not close.
func (t *tokenizer) run(start int, sub bool) (int, bool) {
	rs := t.rs
	for i := start; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case ' ', '\t':
			t.endWord()
		case '\n':
			t.endCommand()
			if len(t.pending) > 0 {
				i = t.bodies(i+1) - 1
			}
		case '(':
			// <(...) and >(...): a redirection with no target yet.
			proc := t.redirect && !t.inWord
			if proc {
				// The command it is given to reads a process substitution.
				t.cur.procArg = true
			}
			if t.casePattern && !t.inWord && len(t.cur.words) == t.patternStart {
				// A case pattern's optional opening parenthesis.
				t.patternParen = true
				continue
			}
			if !t.inWord && !proc && i+1 < len(rs) && rs[i+1] == '(' {
				// ((...)) is an arithmetic command: << in it is a shift.
				if end, ok := arithEnd(rs, i, t.sc); ok {
					t.endCommand()
					t.cur.subs = append(t.cur.subs, bodySubs(rs[i+2:end-1], t.sc)...)
					// A name in it is a variable whose value is evaluated.
					t.cur.arith = strings.ContainsFunc(string(rs[i+2:end-1]), func(r rune) bool { return r == '_' || unicode.IsLetter(r) })
					t.endCommand()
					i = end
					continue
				}
			}
			// a=( and a+=( open a compound array assignment.
			array := !proc && t.inWord && strings.HasSuffix(t.raw.String(), "=")
			if t.definesFunction(i) {
				t.endWord()
				t.cur.funcDef = true
			}
			t.endCommand()
			t.parens = append(t.parens, proc)
			t.arrays = append(t.arrays, array)
		case ')':
			// The word before ends first: an esac there closes the case.
			t.endWord()
			if t.casePattern {
				// The end of a case pattern: its words are no command.
				t.cur.words = t.cur.words[:min(t.patternStart, len(t.cur.words))]
				t.endCommand()
				t.casePattern, t.patternParen = false, false
				continue
			}
			if len(t.parens) == 0 {
				if sub {
					t.endCommand()
					return i, true
				}
				// A ) that closes nothing: what the line means cannot be
				// told.
				t.doubt(whyStrayParen)
			}
			t.endCommand()
			if len(t.parens) > 0 {
				t.parens = t.parens[:len(t.parens)-1]
				t.arrays = t.arrays[:len(t.arrays)-1]
			}
		case ';':
			t.endCommand()
			t.pipe = false
			if t.cases > 0 && i+1 < len(rs) && (rs[i+1] == ';' || rs[i+1] == '&') {
				// ;; ;& ;;& end a case branch: a pattern follows.
				i++
				if i+1 < len(rs) && rs[i] == ';' && rs[i+1] == '&' {
					i++
				}
				t.casePattern, t.patternStart = true, 0
			}
		case '&':
			if i+1 < len(rs) && rs[i+1] == '>' {
				// &> and &>> redirect both outputs to the target.
				t.endWord()
				i = redirectEnd(rs, i+1)
				t.redirect = true
				continue
			}
			n := len(t.cmds)
			t.endCommand()
			t.pipe = false
			if i+1 < len(rs) && rs[i+1] == '&' {
				i++
			} else if len(t.cmds) > 0 && n <= len(t.cmds) {
				// A background job runs alongside what follows.
				t.cmds[len(t.cmds)-1].background = true
			}
		case '|':
			if t.casePattern {
				// a|b in a case pattern: another pattern, not a pipe.
				t.endWord()
				continue
			}
			t.endCommand()
			t.pipe = true
			if i+1 < len(rs) && rs[i+1] == '|' {
				t.pipe = false
				i++
			} else if i+1 < len(rs) && rs[i+1] == '&' {
				i++
			}
		case '<', '>':
			// A word of digits just before is the descriptor: part of the
			// operator, not an argument.
			// So is a {name} just before (bash's named descriptor).
			if t.inWord && !t.quoted && (isDigits(t.raw.String()) || isNamedFd(t.raw.String())) {
				t.dropWord()
			}
			t.endWord()
			if c == '<' && i+1 < len(rs) && rs[i+1] == '<' && (i+2 >= len(rs) || rs[i+2] != '<') {
				end, ok := t.heredocOp(i + 2)
				if !ok {
					return 0, false
				}
				i = end
				continue
			}
			start := i
			i = redirectEnd(rs, i)
			// >&2, <&0, >&- duplicate a descriptor and have no target.
			if rs[i] == '&' && i+1 < len(rs) && (isDigit(rs[i+1]) || rs[i+1] == '-') {
				for i+1 < len(rs) && (isDigit(rs[i+1]) || rs[i+1] == '-') {
					i++
				}
				continue
			}
			t.redirect = true
			t.input = rs[start] == '<'
			t.here = string(rs[start:i+1]) == "<<<"
			t.rw = string(rs[start:i+1]) == "<>"
		case '#':
			if t.inWord {
				t.lit("#", false)
				continue
			}
			start := i + 1
			for i+1 < len(rs) && rs[i+1] != '\n' {
				i++
			}
			t.sc.notCode(rs, start, i+1)
		case '\\':
			if i+1 >= len(rs) {
				return 0, false
			}
			i++
			if rs[i] != '\n' {
				t.lit(string(rs[i]), true)
				continue
			}
			// A backslash-newline bash removes before it reads the line:
			// noted, and the line read again without it (tokenize).
			t.sc.join(rs, i-1)
		case '\'':
			end := indexRune(rs, i+1, '\'')
			if end < 0 {
				return 0, false
			}
			t.lit(string(rs[i+1:end]), true)
			t.sc.notCode(rs, i+1, end)
			i = end
		case '"':
			end, ok := t.double(i + 1)
			if !ok {
				return 0, false
			}
			i = end
		case '`':
			end, ok := t.backtick(i + 1)
			if !ok {
				return 0, false
			}
			i = end
		case '$':
			if i+1 < len(rs) && rs[i+1] == '\'' {
				s, end, ok := ansiC(rs, i+2)
				if !ok {
					return 0, false
				}
				t.lit(s, true)
				t.sc.notCode(rs, i+2, end)
				i = end
				continue
			}
			if i+1 < len(rs) && rs[i+1] == '"' {
				end, ok := t.double(i + 2)
				if !ok {
					return 0, false
				}
				i = end
				continue
			}
			end, ok := t.dollar(i)
			if !ok {
				return 0, false
			}
			i = end
		case '~':
			if t.inWord {
				t.lit("~", false)
				continue
			}
			// A leading ~, up to the first /, is the tilde prefix.
			j := i + 1
			for j < len(rs) && isNameRune(rs[j]) {
				j++
			}
			if j < len(rs) && !strings.ContainsRune("/ \t\r\n;&|()<>", rs[j]) {
				t.lit("~", false)
				continue
			}
			user := string(rs[i+1 : j])
			t.expansion(tildePart, user, "~"+user)
			i = j - 1
		default:
			t.lit(string(c), false)
		}
	}
	if sub {
		return 0, false
	}
	return len(rs), true
}

// isNamedFd reports whether w is {name}, a redirection's named descriptor
// ({fd}>/dev/null).
func isNamedFd(w string) bool {
	return len(w) > 2 && w[0] == '{' && w[len(w)-1] == '}' && isName(w[1:len(w)-1])
}

// finish ends the line run read: its last command, its pending
// here-documents, and the uncertain mark.
func (t *tokenizer) finish() []simple {
	t.endCommand()
	if len(t.pending) > 0 {
		// The line ends before a body starts: each is empty, as in bash,
		// and the line is not read to its end.
		t.bodies(len(t.rs))
		t.doubt(whyUnclosed)
	}
	// The tokenizer fails closed on its own uncertainty: a line that does
	// not end at the plain top level (a case or a parenthesis left open,
	// a case pattern being read, a here-document never closed, a ) that
	// closes nothing), or that it cannot read as bash would, is marked,
	// and the walker denies it.
	if t.cases > 0 || t.casePattern || len(t.parens) > 0 {
		t.doubt(whyUnclosed)
	}
	if t.sc.memo != nil && t.sc.memo.namedArith {
		// Arithmetic anywhere in the line, a case pattern's included:
		// each command is taken to evaluate (evaluates).
		for i := range t.cmds {
			t.cmds[i].arith = true
		}
	}
	if t.uncertain != "" {
		if len(t.cmds) == 0 {
			t.cmds = append(t.cmds, simple{})
		}
		for i := range t.cmds {
			t.cmds[i].uncertain = t.uncertain
		}
	}
	return t.cmds
}

// heredocOp reads a here-document operator's delimiter, after the "<<" that
// ends just before i, and returns the index of its last rune. The body is
// read at the next newline.
func (t *tokenizer) heredocOp(i int) (int, bool) {
	delim, strip, quoted, end, ok := readDelim(t.rs, i)
	if !ok {
		return 0, false
	}
	if strings.ContainsAny(delim, "$`\n") {
		// A delimiter the gate may read other than bash does.
		t.doubt(whyDelim)
	}
	doc := &heredoc{quoted: quoted}
	t.cur.docs = append(t.cur.docs, doc)
	t.pending = append(t.pending, pendingDoc{doc: doc, delim: delim, strip: strip})
	return end, true
}

// bodies reads the bodies of the pending here-documents, in order, from i,
// the start of the line after their operators, and returns the index just
// past the last delimiter line.
func (t *tokenizer) bodies(i int) int {
	for _, p := range t.pending {
		body, next, closed := readBody(t.rs, i, p.delim, p.strip)
		if !closed {
			t.doubt(whyUnclosed)
		}
		if p.doc.quoted {
			// bash joins nothing in a quoted body.
			t.sc.notCode(t.rs, i, min(next, len(t.rs)))
		} else {
			// bash joins a backslash-newline in an unquoted body.
			t.bodyJoins(i, next, p)
		}
		if t.sc.depth > 0 && bodyLineStarts(body, p.delim, p.strip) {
			// Inside a command substitution, bash 5.2 ends the body at a
			// line that starts with the delimiter, and reads the rest of
			// it as commands.
			t.doubt(whyBash52)
		}
		i = next
		p.doc.body = body
		if !p.doc.quoted {
			p.doc.subs = bodySubs([]rune(body), t.sc)
		}
	}
	t.pending = nil
	return i
}

// bodyJoins notes the backslash-newlines bash removes in an unquoted
// here-document body, rs[i:next] with its delimiter line, so that the line
// is read again with them joined (tokenize). A join the gate cannot follow
// so is doubted (whyBodyJoin): one whose next line is the delimiter (bash
// reads on past it), one that makes a line that is the delimiter (bash
// ends the body there), and one in a line with a $( or a backtick, or
// between a $ or a backtick and what follows.
func (t *tokenizer) bodyJoins(i, next int, p pendingDoc) {
	rs := t.rs
	next = min(next, len(rs))
	isDelim := func(line string) bool {
		if p.strip {
			line = strings.TrimLeft(line, "\t")
		}
		return line == p.delim
	}
	var logical strings.Builder
	joined, lineStart := false, i
	for k := i; k < next; k++ {
		c := rs[k]
		switch {
		case c == '\\' && k+1 < next && rs[k+1] == '\n':
			after := k + 2
			nl := indexRune(rs, after, '\n')
			if nl < 0 {
				nl = len(rs)
			}
			line := string(rs[lineStart:k])
			prev, following := ' ', ' '
			if k > lineStart {
				prev = rs[k-1]
			}
			if after < len(rs) {
				following = rs[after]
			}
			if isDelim(string(rs[after:nl])) || strings.Contains(line, "$(") || strings.Contains(line, "`") ||
				prev == '$' || prev == '`' || following == '(' || following == '`' {
				t.sc.doubtJoin()
			}
			t.sc.join(rs, k)
			joined, lineStart = true, after
			k++
		case c == '\\' && k+1 < next:
			logical.WriteRune(c)
			logical.WriteRune(rs[k+1])
			k++
		case c == '\n':
			if joined && isDelim(logical.String()) {
				t.sc.doubtJoin()
			}
			logical.Reset()
			joined, lineStart = false, k+1
		default:
			logical.WriteRune(c)
		}
	}
	if joined && isDelim(logical.String()) {
		t.sc.doubtJoin()
	}
}

// readDelim reads a here-document's delimiter word starting at i, just
// after "<<": an optional "-", blanks, then the word, quotes removed. It
// returns the index of the word's last rune; ok is false when there is no
// word or a quote in it does not close.
func readDelim(rs []rune, i int) (delim string, strip, quoted bool, end int, ok bool) {
	if i < len(rs) && rs[i] == '-' {
		strip = true
		i++
	}
	for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t') {
		i++
	}
	var b strings.Builder
	start := i
	for ; i < len(rs) && !strings.ContainsRune(" \t\n;&|()<>", rs[i]); i++ {
		switch rs[i] {
		case '$':
			// $'...' and $"..." are quoting, which bash removes: <<$'E'
			// ends at a line E.
			if i+1 < len(rs) && rs[i+1] == '\'' {
				s, k, ok := ansiC(rs, i+2)
				if !ok {
					return "", false, false, 0, false
				}
				b.WriteString(s)
				quoted, i = true, k
				continue
			}
			if i+1 < len(rs) && rs[i+1] == '"' {
				i++
				k := i + 1
				for ; k < len(rs) && rs[k] != '"'; k++ {
					if rs[k] == '\\' && k+1 < len(rs) {
						k++
					}
					b.WriteRune(rs[k])
				}
				if k >= len(rs) {
					return "", false, false, 0, false
				}
				quoted, i = true, k
				continue
			}
			b.WriteRune(rs[i])
		case '\'':
			k := indexRune(rs, i+1, '\'')
			if k < 0 {
				return "", false, false, 0, false
			}
			b.WriteString(string(rs[i+1 : k]))
			quoted, i = true, k
		case '"':
			k := i + 1
			for ; k < len(rs) && rs[k] != '"'; k++ {
				if rs[k] == '\\' && k+1 < len(rs) {
					k++
				}
				b.WriteRune(rs[k])
			}
			if k >= len(rs) {
				return "", false, false, 0, false
			}
			quoted, i = true, k
		case '\\':
			if i+1 >= len(rs) {
				return "", false, false, 0, false
			}
			i++
			b.WriteRune(rs[i])
			quoted = true
		default:
			b.WriteRune(rs[i])
		}
	}
	if i == start {
		return "", false, false, 0, false
	}
	return b.String(), strip, quoted, i - 1, true
}

// bodyLineStarts reports whether a line of body starts with delim, other
// than the delimiter line itself, which is not in body.
func bodyLineStarts(body, delim string, strip bool) bool {
	for _, line := range strings.Split(body, "\n") {
		if strip {
			line = strings.TrimLeft(line, "\t")
		}
		if delim != "" && strings.HasPrefix(line, delim) {
			return true
		}
	}
	return false
}

// readBody reads a here-document's body from i, the start of a line, up to
// the line that is the delimiter (after its leading tabs, for <<-), and
// returns the body and the index just past the delimiter line. Without a
// delimiter line the body runs to the end, as in bash, and closed is false.
func readBody(rs []rune, i int, delim string, strip bool) (body string, next int, closed bool) {
	var b strings.Builder
	for i < len(rs) {
		nl := indexRune(rs, i, '\n')
		if nl < 0 {
			nl = len(rs)
		}
		line := string(rs[i:nl])
		if strip {
			line = strings.TrimLeft(line, "\t")
		}
		i = nl + 1
		if line == delim {
			return b.String(), i, true
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), len(rs), false
}

// bodySubs are the command substitutions, $(...) and `...`, in an unquoted
// here-document's body, which run when the body is expanded. One that does
// not close runs to the end of the body.
func bodySubs(rs []rune, sc lineScan) []string {
	var subs []string
	for j := 0; j < len(rs); j++ {
		switch {
		case rs[j] == '\\':
			j++
		case rs[j] == '$' && j+2 < len(rs) && rs[j+1] == '(' && rs[j+2] == '(':
			if end, ok := arithEnd(rs, j+1, sc); ok {
				// $((...)): only the substitutions in it run.
				subs = append(subs, bodySubs(rs[j+3:end-1], sc)...)
				j = end
				continue
			}
			fallthrough
		case rs[j] == '$' && j+1 < len(rs) && rs[j+1] == '(':
			end, _, ok := substEnd(rs, j+2, sc.deeper())
			if !ok {
				return append(subs, string(rs[j+2:]))
			}
			subs = append(subs, string(rs[j+2:end]))
			j = end
		case rs[j] == '`':
			end := j + 1
			for end < len(rs) && rs[end] != '`' {
				if rs[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(rs) {
				return append(subs, string(rs[j+1:]))
			}
			subs = append(subs, string(rs[j+1:end]))
			j = end
		}
	}
	return subs
}

// double reads a double-quoted string whose first rune is at i, and returns
// the index of its closing quote.
func (t *tokenizer) double(i int) (int, bool) {
	rs := t.rs
	t.lit("", true)
	t.inDouble = true
	defer func() { t.inDouble = false }()
	for ; i < len(rs); i++ {
		switch rs[i] {
		case '"':
			return i, true
		case '\\':
			if i+1 < len(rs) && strings.ContainsRune("\\\"$`\n", rs[i+1]) {
				i++
				if rs[i] != '\n' {
					t.lit(string(rs[i]), true)
				}
				continue
			}
			t.lit(`\`, true)
		case '`':
			end, ok := t.backtick(i + 1)
			if !ok {
				return 0, false
			}
			i = end
		case '$':
			end, ok := t.dollar(i)
			if !ok {
				return 0, false
			}
			i = end
		default:
			t.lit(string(rs[i]), true)
		}
	}
	return 0, false
}

// backtick reads a `...` substitution whose body starts at i, and returns
// the index of its closing backtick.
func (t *tokenizer) backtick(i int) (int, bool) {
	rs := t.rs
	var body strings.Builder
	for j := i; j < len(rs); j++ {
		switch rs[j] {
		case '\\':
			if j+1 < len(rs) {
				j++
				if !strings.ContainsRune("$`\\", rs[j]) {
					body.WriteRune('\\')
				}
				body.WriteRune(rs[j])
			}
		case '`':
			t.cur.subs = append(t.cur.subs, body.String())
			t.expansion(opaquePart, "", string(rs[i-1:j+1]))
			return j, true
		default:
			body.WriteRune(rs[j])
		}
	}
	return 0, false
}

// dollar reads an expansion starting with the $ at i, and returns the index
// of its last rune. A $ that starts no expansion is literal.
func (t *tokenizer) dollar(i int) (int, bool) {
	rs := t.rs
	if i+1 >= len(rs) {
		t.lit("$", false)
		return i, true
	}
	switch c := rs[i+1]; {
	case c == '(':
		if i+2 < len(rs) && rs[i+2] == '(' {
			if end, ok := arithEnd(rs, i+1, t.sc); ok {
				// $((...)) is arithmetic: << in it is a shift, and only the
				// substitutions in it run.
				t.cur.subs = append(t.cur.subs, bodySubs(rs[i+3:end-1], t.sc)...)
				t.expansion(opaquePart, "", string(rs[i:end+1]))
				return end, true
			}
		}
		end, uncertain, ok := substEnd(rs, i+2, t.sc.deeper())
		if !ok {
			return 0, false
		}
		t.doubt(uncertain)
		t.cur.subs = append(t.cur.subs, string(rs[i+2:end]))
		t.expansion(opaquePart, "", string(rs[i:end+1]))
		return end, true
	case c == '[':
		// $[...] is bash's old arithmetic.
		end := bracketEnd(rs, i+1)
		if end < 0 {
			return 0, false
		}
		t.cur.subs = append(t.cur.subs, bodySubs(rs[i+2:end], t.sc)...)
		t.expansion(opaquePart, "", string(rs[i:end+1]))
		return end, true
	case c == '{':
		end := braceEnd(rs, i+1, t.sc)
		if end < 0 {
			return 0, false
		}
		inner := string(rs[i+2 : end])
		if isName(inner) {
			t.expansion(paramPart, inner, string(rs[i:end+1]))
		} else {
			// ${X:-$(cmd)} runs cmd: the substitutions in the body,
			// nested ${...} included, are the command's own.
			t.cur.subs = append(t.cur.subs, bodySubs([]rune(inner), t.sc)...)
			t.expansion(opaquePart, "", string(rs[i:end+1]))
		}
		return end, true
	case isNameRune(c) && !isDigit(c):
		j := i + 1
		for j < len(rs) && isNameRune(rs[j]) {
			j++
		}
		t.expansion(paramPart, string(rs[i+1:j]), string(rs[i:j]))
		return j - 1, true
	case isDigit(c) || strings.ContainsRune("@*#?$!-", c):
		t.expansion(opaquePart, "", string(rs[i:i+2]))
		return i + 1, true
	}
	t.lit("$", false)
	return i, true
}

// skipQuoted returns the index of the last rune of the quoted text, the
// escape or the backtick substitution starting at k, or k when there is
// none there; ok is false when it does not close.
func skipQuoted(rs []rune, k int) (int, bool) {
	switch rs[k] {
	case '\\':
		return min(k+1, len(rs)-1), true
	case '\'':
		e := indexRune(rs, k+1, '\'')
		return e, e >= 0
	case '"', '`':
		q := rs[k]
		for e := k + 1; e < len(rs); e++ {
			switch rs[e] {
			case '\\':
				e++
			case q:
				return e, true
			}
		}
		return 0, false
	}
	return k, true
}

// arithEnd reads an arithmetic expression ((...)) whose first "(" is at j
// (after a $, or as a command), and returns the index of its last ")". ok
// is false when the parentheses do not close as "))", as with a subshell
// in a subshell, or never close. Quotes, backticks, ${...} and nested
// substitutions in it are skipped as bash reads them.
func arithEndRaw(rs []rune, j int, sc lineScan) (int, bool) {
	d := 0
	for k := j; k < len(rs); k++ {
		switch rs[k] {
		case '\\', '\'', '"', '`':
			e, ok := skipQuoted(rs, k)
			if !ok {
				return 0, false
			}
			k = e
		case '$':
			if k+1 < len(rs) && rs[k+1] == '{' {
				e := braceEnd(rs, k+1, sc)
				if e < 0 {
					return 0, false
				}
				k = e
			} else if k+1 < len(rs) && rs[k+1] == '(' {
				if k+2 < len(rs) && rs[k+2] == '(' {
					if e, ok := arithEnd(rs, k+1, sc); ok {
						k = e
						continue
					}
				}
				e, _, ok := substEnd(rs, k+2, sc.deeper())
				if !ok {
					return 0, false
				}
				k = e
			}
		case '(':
			d++
		case ')':
			d--
			if d == 1 {
				return k + 1, k+1 < len(rs) && rs[k+1] == ')'
			}
			if d == 0 {
				return 0, false
			}
		}
	}
	return 0, false
}

// braceEnd returns the index of the "}" that closes the ${ whose "{" is at
// i, or -1. Quotes, backticks, nested ${...} and substitutions in it are
// skipped as bash reads them; a ) or # in it is text.
func braceEndRaw(rs []rune, i int, sc lineScan) int {
	d := 0
	for k := i; k < len(rs); k++ {
		switch rs[k] {
		case '\\', '\'', '"', '`':
			e, ok := skipQuoted(rs, k)
			if !ok {
				return -1
			}
			k = e
		case '$':
			if k+1 < len(rs) && rs[k+1] == '(' {
				if k+2 < len(rs) && rs[k+2] == '(' {
					if e, ok := arithEnd(rs, k+1, sc); ok {
						k = e
						continue
					}
				}
				e, _, ok := substEnd(rs, k+2, sc.deeper())
				if !ok {
					return -1
				}
				k = e
			}
		case '{':
			d++
		case '}':
			d--
			if d == 0 {
				return k
			}
		}
	}
	return -1
}

// arithEnd is arithEndRaw, remembered for the line (lineScan).
func arithEnd(rs []rune, j int, sc lineScan) (int, bool) {
	key, memo := sc.key(rs, j)
	if memo {
		if r, seen := sc.memo.arith[key]; seen {
			return j + r.end, r.ok
		}
	}
	end, ok := arithEndRaw(rs, j, sc)
	if memo {
		sc.memo.arith[key] = arithResult{end - j, ok}
	}
	return end, ok
}

// braceEnd is braceEndRaw, remembered for the line (lineScan).
func braceEnd(rs []rune, i int, sc lineScan) int {
	key, memo := sc.key(rs, i)
	if memo {
		if r, seen := sc.memo.brace[key]; seen {
			if r < 0 {
				return -1
			}
			return i + r
		}
	}
	end := braceEndRaw(rs, i, sc)
	if memo {
		if end < 0 {
			sc.memo.brace[key] = -1
		} else {
			sc.memo.brace[key] = end - i
		}
	}
	return end
}

// bracketEnd returns the index of the "]" that closes the $[ whose "[" is
// at i, or -1.
func bracketEnd(rs []rune, i int) int {
	d := 0
	for k := i; k < len(rs); k++ {
		switch rs[k] {
		case '[':
			d++
		case ']':
			d--
			if d == 0 {
				return k
			}
		}
	}
	return -1
}

// ansiC decodes a $'...' string whose body starts at i, as bash does, and
// returns it with the index of the closing quote.
func ansiC(rs []rune, i int) (string, int, bool) {
	var b strings.Builder
	for j := i; j < len(rs); j++ {
		c := rs[j]
		if c == '\'' {
			return b.String(), j, true
		}
		if c != '\\' {
			b.WriteRune(c)
			continue
		}
		if j+1 >= len(rs) {
			return "", 0, false
		}
		j++
		switch e := rs[j]; e {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'e', 'E':
			b.WriteByte(0x1b)
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case 'c':
			if j+1 < len(rs) {
				j++
				b.WriteRune(rs[j] & 0x1f)
			}
		case 'x', 'u', 'U':
			limit := map[rune]int{'x': 2, 'u': 4, 'U': 8}[e]
			k := j + 1
			for k < len(rs) && k-j-1 < limit && isHex(rs[k]) {
				k++
			}
			if k == j+1 {
				b.WriteRune('\\')
				b.WriteRune(e)
				continue
			}
			n, _ := strconv.ParseUint(string(rs[j+1:k]), 16, 32)
			if e == 'x' {
				b.WriteByte(byte(n)) //nolint:gosec // G115: at most two hex digits, which fit a byte
			} else {
				b.WriteRune(rune(n)) //nolint:gosec // G115: at most eight hex digits, parsed to 32 bits; bash takes them as is
			}
			j = k - 1
		case '0', '1', '2', '3', '4', '5', '6', '7':
			k := j
			for k < len(rs) && k-j < 3 && '0' <= rs[k] && rs[k] <= '7' {
				k++
			}
			n, _ := strconv.ParseUint(string(rs[j:k]), 8, 32)
			b.WriteByte(byte(n)) //nolint:gosec // G115: bash keeps the low byte of three octal digits, as this does
			j = k - 1
		case '\\', '\'', '"', '?':
			b.WriteRune(e)
		default:
			b.WriteRune('\\')
			b.WriteRune(e)
		}
	}
	return "", 0, false
}

func isHex(r rune) bool {
	return isDigit(r) || 'a' <= r && r <= 'f' || 'A' <= r && r <= 'F'
}

// redirectEnd returns the index of the last rune of the redirection
// operator that starts at i: <, >, >>, <<, <<<, <>, >|, <&, >&.
func redirectEnd(rs []rune, i int) int {
	for i+1 < len(rs) && strings.ContainsRune("<>|&", rs[i+1]) {
		// A pipe after > is >|; anywhere else it starts a pipeline.
		if rs[i+1] == '|' && rs[i] != '>' {
			break
		}
		i++
		if rs[i] == '&' || rs[i] == '|' {
			break
		}
	}
	return i
}

// splitFields is the fallback for a line that does not tokenize: words
// split on whitespace, and a command ends at a newline or at ;, &, |, ( or
// ), whether or not a space sets it apart, and whatever quote is open.
// Quotes are kept as written. A word with a $ or a backtick holds an
// opaque part. Every command is marked fallback.
func splitFields(line string) []simple {
	var cmds []simple
	var cur simple
	var f strings.Builder
	pipe := false
	endWord := func() {
		if f.Len() == 0 {
			return
		}
		w := f.String()
		f.Reset()
		quoted := strings.ContainsAny(w, `'"\`)
		parts := []part{{kind: litPart, text: w, quoted: quoted}}
		splits := strings.ContainsAny(w, "$`")
		if splits {
			// Where the quotes are cannot be told: it may split.
			parts = append(parts, part{kind: opaquePart, raw: ""})
		}
		cur.words = append(cur.words, word{text: w, parts: parts, quoted: quoted, splits: splits})
	}
	endCommand := func(nextPipe bool) {
		endWord()
		if len(cur.words) > 0 {
			cur.pipeIn = pipe
			cur.fallback = true
			cmds = append(cmds, cur)
		}
		cur = simple{}
		pipe = nextPipe
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		switch c := rs[i]; c {
		case ' ', '\t', '\r':
			endWord()
		case '\n', ';', '(', ')':
			endCommand(false)
		case '&':
			endCommand(false)
			if i+1 < len(rs) && rs[i+1] == '&' {
				i++
			}
		case '|':
			switch {
			case i+1 < len(rs) && rs[i+1] == '|':
				endCommand(false)
				i++
			case i+1 < len(rs) && rs[i+1] == '&':
				endCommand(true)
				i++
			default:
				endCommand(true)
			}
		default:
			f.WriteRune(c)
		}
	}
	endCommand(false)
	return cmds
}

func indexRune(rs []rune, from int, r rune) int {
	for j := from; j < len(rs); j++ {
		if rs[j] == r {
			return j
		}
	}
	return -1
}

func isDigit(r rune) bool { return '0' <= r && r <= '9' }

func isNameRune(r rune) bool {
	return r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || isDigit(r)
}

// isName reports whether s is a shell variable name.
func isName(s string) bool {
	if s == "" || isDigit(rune(s[0])) {
		return false
	}
	for _, r := range s {
		if !isNameRune(r) {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isDigit(r) {
			return false
		}
	}
	return true
}

// isAssignment reports whether w is an environment assignment, NAME=value.
func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	return ok && isName(name)
}

// keywords are the shell's reserved words and grouping tokens that can
// stand before a command's name.
var keywords = map[string]bool{
	"if": true, "then": true, "elif": true, "else": true, "fi": true, "do": true, "done": true,
	"while": true, "until": true, "esac": true, "!": true, "{": true, "}": true,
}

// listKeywords start a compound command whose words are a list, not a
// command: nothing in it runs.
var listKeywords = map[string]bool{"for": true, "select": true, "case": true, "in": true}

// wrapper describes a command that runs the command after its own options.
type wrapper struct {
	// short are the short options that take a value; long the long ones
	// (without "--") that take one given apart.
	short string
	long  []string
	// operands is how many words, after the options, come before the
	// command: timeout's duration, taskset's mask, flock's file.
	operands int
}

// wrappers run the command that follows them, by name.
var wrappers = map[string]wrapper{
	"env":     {short: "uCS", long: []string{"unset", "chdir", "split-string", "block-signal", "default-signal", "ignore-signal"}},
	"command": {}, "exec": {short: "a"}, "nohup": {}, "builtin": {},
	"time": {short: "fo", long: []string{"format", "output"}},
	"nice": {short: "n", long: []string{"adjustment"}},
	"sudo": {short: "ugCDhprtUT", long: []string{
		"user", "group", "close-from", "chdir", "host", "prompt", "role", "type", "other-user", "command-timeout",
	}},
	"doas": {short: "uC"},
	"xargs": {short: "IndPaELsE", long: []string{
		"arg-file", "delimiter", "max-lines", "max-args", "max-procs", "max-chars", "process-slot-var",
	}},
	"stdbuf":      {short: "ioe", long: []string{"input", "output", "error"}},
	"timeout":     {short: "sk", long: []string{"signal", "kill-after"}, operands: 1},
	"ionice":      {short: "cnp", long: []string{"class", "classdata", "pid"}},
	"setsid":      {},
	"caffeinate":  {short: "tw"},
	"unbuffer":    {},
	"chronic":     {},
	"taskset":     {operands: 1},
	"flock":       {short: "wE", long: []string{"timeout", "wait", "conflict-exit-code"}, operands: 1},
	"coproc":      {},
	"busybox":     {},
	"xvfb-run":    {short: "efnpsw", long: []string{"error-file", "auth-file", "server-num", "xauth-protocol", "server-args", "wait"}},
	"nocache":     {},
	"proxychains": {short: "f"}, "proxychains4": {short: "f"},
	"torsocks": {}, "firejail": {}, "catchsegv": {}, "valgrind": {},
	"strace": {short: "eoOpPsSuUXI"}, "ltrace": {short: "eoOpPsSuUXIn"},
}

// inert are programs that never run a word they are given as a command,
// though one may name a program: `which rg`, `man grep`, `echo git diff`.
// Any other program whose first word that is not an option is a program
// the rules judge is judged as that program (command).
var inert = map[string]bool{
	"gh": true, "echo": true, "printf": true, "which": true, "whereis": true, "type": true, "man": true,
	"info": true, "help": true, "apropos": true, "whatis": true, "hash": true, "alias": true,
	"unalias": true, "unset": true, "export": true, "declare": true, "typeset": true, "local": true,
	"readonly": true, "set": true, "shopt": true, "test": true, "[": true, "[[": true, "true": true,
	"false": true, "mkdir": true, "rmdir": true, "touch": true, "rm": true, "ls": true, "chmod": true,
	"chown": true, "kill": true, "pkill": true, "pgrep": true, "killall": true, "ps": true,
	"apt": true, "apt-get": true, "apt-cache": true, "brew": true, "dnf": true, "yum": true,
	"pacman": true, "apk": true, "snap": true, "pip": true, "pip3": true, "pipx": true, "npm": true,
	"pnpm": true, "yarn": true, "cargo": true, "go": true, "rustup": true, "gem": true,
	"complete": true, "compgen": true, "trap": true, "read": true, "wait": true, "jobs": true,
	"fg": true, "bg": true, "disown": true, "exit": true, "return": true, "shift": true,
	"mv": true, "pwd": true, "date": true, "sleep": true, "basename": true, "dirname": true,
	"realpath": true, "readlink": true, "stat": true, "file": true, "du": true, "df": true,
	"tree": true, "make": true, "pbipx": true, "git": true, "unzip": true, "zip": true,
	"cd": true, "pushd": true, "popd": true,
}

// judged reports whether the rules judge a program by name: a reader, a
// search, git, find, fd, a shell or eval.
func judged(name string) bool {
	_, search := searchers[name]
	return readers[name] || search || shells[name] || name == "git" || name == "eval" ||
		name == "find" || name == "fd" || name == "fdfind"
}

// shells are the shell interpreters: their -c string, and a script they
// read from a here-document, a here-string or a pipe, are command lines.
var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "mksh": true, "ash": true,
	"yash": true, "fish": true, "rbash": true, "pdksh": true, "posh": true, "tcsh": true, "csh": true,
}

// interpreter is how the gate reads a script interpreter's command line:
// the options that give it its program as text (code), or name a module
// or file it runs (runs), each with the program attached or as the next
// word, the other options that take the next word as a value, and the
// options known to take none (flags). Any other option may take the next
// word as its value.
type interpreter struct {
	code, runs, values, flags []string
}

// interpreters are the script interpreters that read their program from
// their input when given no program: no script operand and no code
// option, or their input as the script (stdinScripts). A versioned name
// (python3.14, perl5.36, node22) is its family's (interpreterFamily).
// Names are in lower case, as on a file system that folds case; R and
// Rscript are found as r and rscript.
var interpreters = map[string]interpreter{
	"python": {code: []string{"-c"}, runs: []string{"-m"}, values: []string{"-W", "-X", "-Q"},
		flags: []string{"-b", "-bb", "-B", "-d", "-E", "-i", "-I", "-O", "-OO", "-P", "-q", "-s", "-S", "-u", "-v", "-x", "-R"}},
	"ipython": {code: []string{"-c"}, runs: []string{"-m"},
		values: []string{"--profile", "--ipython-dir", "--log-level", "--config"}},
	"node": {code: []string{"-e", "--eval", "-p", "--print"},
		values: []string{"-r", "--require", "--import", "--input-type", "--loader", "--experimental-loader", "--title"}},
	"perl": {code: []string{"-e", "-E"}, values: []string{"-I", "-M", "-m", "-x"},
		flags: []string{"-w", "-W", "-X", "-n", "-p", "-l", "-a", "-s", "-T", "-t", "-c", "-d", "-U"}},
	"ruby": {code: []string{"-e"}, values: []string{"-r", "-I", "-C", "-E", "--encoding"},
		flags: []string{"-w", "-W", "-v", "-d", "-n", "-p", "-l", "-a", "-s", "-c"}},
	"php":     {code: []string{"-r", "-R", "-B", "-E"}, runs: []string{"-f"}, values: []string{"-d", "-c", "-z"}},
	"r":       {code: []string{"-e"}, runs: []string{"-f", "--file"}, values: []string{"--encoding"}},
	"rscript": {code: []string{"-e"}},
	"lua":     {code: []string{"-e"}, values: []string{"-l"}},
	"luajit":  {code: []string{"-e"}, values: []string{"-l", "-b", "-j", "-O"}},
	"tclsh":   {},
	"deno":    {},
	"bun":     {code: []string{"-e", "--eval", "-p", "--print"}},
}

// interpreterFamily is the name interpreters knows the program name by: a
// version after the name is dropped (python3.14 is python, pypy3 is
// python, perl5.36 is perl, node22 and nodejs are node, lua5.4 is lua).
func interpreterFamily(name string) string {
	// Names are looked up in lower case on every platform: R is r, as a
	// folding file system finds it (a lower-case r elsewhere is taken for
	// R too, which can only deny).
	name = strings.ToLower(name)
	if _, ok := interpreters[name]; ok {
		return name
	}
	for _, f := range []string{"python", "pypy", "perl", "ruby", "php", "lua", "node", "tclsh"} {
		rest, ok := strings.CutPrefix(name, f)
		if !ok || strings.Trim(rest, "0123456789.") != "" && (f != "node" || rest != "js") {
			continue
		}
		if f == "pypy" {
			return "python"
		}
		return f
	}
	return name
}

// stdinScripts are the script operands that name the program's input.
var stdinScripts = []string{"-", "/dev/stdin", "/dev/fd/0", "/proc/self/fd/0"}

// awks are the awk programs: one reads its program from its input with -f
// and a name of its input (stdinScripts).
var awks = map[string]bool{"awk": true, "gawk": true, "mawk": true, "nawk": true, "busybox-awk": true}

// readsProgram reports whether interpreter name, given rest, reads its
// program from its input: its input named as the script, or no script
// operand, no code option and no module. deno and bun read it so after
// their run subcommand (deno alone too); awk only with -f naming its
// input. A request for the version or help reads nothing.
func readsProgram(name string, rest []string) bool {
	if awks[name] {
		for i, a := range rest {
			switch {
			case a == "--":
				return false
			case a == "-f" || a == "--file":
				if i+1 < len(rest) && slices.Contains(stdinScripts, rest[i+1]) {
					return true
				}
			case strings.HasPrefix(a, "-f") && slices.Contains(stdinScripts, a[2:]),
				strings.HasPrefix(a, "--file=") && slices.Contains(stdinScripts, a[len("--file="):]):
				return true
			}
		}
		return false
	}
	name = interpreterFamily(name)
	in, ok := interpreters[name]
	if !ok || (len(rest) > 0 && versionOnly(rest)) {
		return false
	}
	switch name {
	case "deno":
		if len(rest) == 0 {
			return true // the REPL reads its input
		}
		if rest[0] != "run" {
			return false
		}
		rest = rest[1:]
	case "bun":
		if len(rest) > 0 && rest[0] == "run" {
			rest = rest[1:]
		} else if len(rest) == 0 || !slices.Contains(stdinScripts, rest[0]) {
			return false
		}
	}
	matches := func(a string, opts []string) (bool, bool) {
		for _, o := range opts {
			switch {
			case a == o:
				return true, false
			case strings.HasPrefix(a, o+"="), !strings.HasPrefix(o, "--") && strings.HasPrefix(a, o) && len(a) > len(o):
				return true, true
			}
		}
		return false, false
	}
	// unsure is set after an option the gate does not know, whose value
	// the next word may be: then that word is no clear script, and the
	// program is taken to come from the input (a miss denies).
	unsure := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case slices.Contains(stdinScripts, a):
			return true
		case a == "--":
			return i+1 >= len(rest) || slices.Contains(stdinScripts, rest[i+1])
		case !strings.HasPrefix(a, "-"):
			return unsure // the script, or an option's value
		}
		if ok, _ := matches(a, in.code); ok {
			return false
		}
		if ok, _ := matches(a, in.runs); ok {
			return false
		}
		if clusterHas(a, in.code) {
			// perl -ne 'print': a cluster ending in the code option.
			return false
		}
		ok, attached := matches(a, in.values)
		switch {
		case ok && !attached:
			i++
		case ok, strings.Contains(a, "="), slices.Contains(in.flags, a):
		default:
			unsure = true
		}
	}
	return true
}

// clusterHas reports whether a, a cluster of short options (-ne), ends in
// one of the single-letter options opts names.
func clusterHas(a string, opts []string) bool {
	if len(a) < 3 || a[0] != '-' || a[1] == '-' {
		return false
	}
	return slices.ContainsFunc(opts, func(o string) bool { return len(o) == 2 && o[0] == '-' && a[len(a)-1] == o[1] })
}

// runners are the programs that run the program named after a subcommand
// (uv run python, pnpm exec node) or after their own options (npx node):
// command reads them as wrappers.
var runners = map[string]string{
	"uv": "run", "poetry": "run", "pipenv": "run", "conda": "run", "pnpm": "exec", "npx": "",
	"bunx": "", "pdm": "run", "hatch": "run", "rye": "run",
}

// stripKeywords drops the reserved words and grouping tokens a command
// starts with, and `function NAME`. A for, select or case runs nothing of
// its own: nil.
func stripKeywords(args []string) []string {
	for len(args) > 0 {
		switch w := args[0]; {
		case keywords[w]:
			args = args[1:]
		case w == "function", w == "repeat":
			// function NAME, and zsh's repeat COUNT.
			args = args[min(2, len(args)):]
		case listKeywords[w]:
			return nil
		default:
			return args
		}
	}
	return args
}

// command returns the command a simple command runs, by its base name, the
// arguments after it and the wrappers it runs under: leading reserved words
// and assignments are skipped, and so are wrappers such as env, with their
// options, assignments and operands. `command -v` and `-V` run nothing. A
// program that is not a known wrapper and not inert, whose first word that
// is not an option is a program the rules judge, counts as a wrapper of
// it: an unknown wrapper is judged by what it runs.
func command(args []string) (name string, rest []string, under []string) {
	args = stripKeywords(args)
	i := 0
	for i < len(args) {
		w := args[i]
		if isAssignment(w) {
			i++
			continue
		}
		base := progName(w)
		if sub, ok := runners[base]; ok && (sub == "" || i+1 < len(args) && args[i+1] == sub) {
			// uv run [options] PROGRAM ...: options are skipped, a value
			// given apart is taken for the program (a missed rule only
			// when that value is itself a program the rules judge).
			under = append(under, base)
			i++
			if sub != "" {
				i++
			}
			for i < len(args) && strings.HasPrefix(args[i], "-") {
				i++
			}
			continue
		}
		wr, ok := wrappers[base]
		if !ok {
			// hub runs git with its words: its subcommand is git's, not a
			// program it wraps (hub diff, hub grep).
			if !inert[base] && !judged(base) && base != "hub" {
				j := i + 1
				for j < len(args) && strings.HasPrefix(args[j], "-") {
					j++
				}
				// A word "." after a program is a path (gofmt -l .), not the
				// dot-source builtin it would be as a command.
				if j < len(args) && args[j] != "." && judged(progName(args[j])) {
					under = append(under, base)
					i = j
					continue
				}
			}
			return base, args[i+1:], under
		}
		under = append(under, base)
		i++
		for i < len(args) && (strings.HasPrefix(args[i], "-") || isAssignment(args[i])) {
			o := args[i]
			i++
			if o == "--" {
				break
			}
			if base == "command" && (strings.ContainsRune(o, 'v') || strings.ContainsRune(o, 'V')) && !strings.HasPrefix(o, "--") {
				return "", nil, under
			}
			if long, ok := strings.CutPrefix(o, "--"); ok {
				if !strings.Contains(long, "=") && slices.Contains(wr.long, long) && i < len(args) {
					i++
				}
				continue
			}
			// In a cluster of short options, one that takes a value takes
			// the rest of the word, or the next word when it ends the word.
			for k := 1; k < len(o); k++ {
				if strings.IndexByte(wr.short, o[k]) >= 0 {
					if k == len(o)-1 && i < len(args) {
						i++
					}
					break
				}
			}
		}
		i = min(i+wr.operands, len(args))
		if base == "coproc" {
			// coproc NAME { ...; }: the name, then the group.
			if i+1 < len(args) && args[i+1] == "{" {
				i++
			}
			args = append(args[:i:i], stripKeywords(args[i:])...)
		}
		if base == "time" {
			// time { ...; }, time ! cmd, time function f { ...; }: the
			// reserved words after time and its options.
			args = append(args[:i:i], stripKeywords(args[i:])...)
		}
	}
	return "", nil, under
}

// program returns the command a simple command runs, by its base name, and
// the arguments after it (command, without the wrappers). A wrapper's
// option that takes a separate value it does not know can be mistaken for
// the command; the cost is a missed rule, which other layers cover
// (docs/design/threat-model.md).
func program(args []string) (name string, rest []string) {
	name, rest, _ = command(args)
	return name, rest
}

// baseName is the last element of a slash-separated command path.
func baseName(w string) string {
	if i := strings.LastIndexAny(w, `/\`); i >= 0 {
		return w[i+1:]
	}
	return w
}

// progName is the name the rules know the program w by: its base name
// without a trailing ".exe" in any case (Windows runs rg.exe for rg, and
// Git Bash and WSL run a .exe by its full name), folded to lower case where
// the file system finds a program regardless of case (foldCase). Elsewhere
// LS is not ls.
func progName(w string) string {
	b := baseName(w)
	if n := len(b) - len(".exe"); n > 0 && strings.EqualFold(b[n:], ".exe") {
		b = b[:n]
	}
	if foldCase {
		b = strings.ToLower(b)
	}
	return b
}
