package gate

import (
	"slices"
	"strconv"
	"strings"
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
}

// word is one word of a simple command, with its quotes removed.
type word struct {
	// text is the word with quotes removed and expansions as written.
	text string
	// parts are its pieces, in order.
	parts []part
	// quoted is true when any part of the word was quoted or escaped.
	quoted bool
	// redirect is true for a redirection's target, which is not an
	// argument of the command.
	redirect bool
	// input is true for the target of a redirection that reads (<, <>);
	// here for a here-string's (<<<), which is the command's input itself,
	// not a file.
	input, here bool
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
	pipe     bool // the next command reads this one's output
	// litBuf is the literal part being read, open when litOpen.
	litBuf    strings.Builder
	litOpen   bool
	litQuoted bool
	// pending are the here-documents whose bodies start after the next
	// newline.
	pending []pendingDoc
}

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
	t.inWord = true
	t.raw.WriteString(raw)
	t.flushLit()
	t.parts = append(t.parts, part{kind: kind, text: text, raw: raw})
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
	if t.inWord {
		t.cur.words = append(t.cur.words, word{
			text: t.raw.String(), parts: t.parts, quoted: t.quoted,
			redirect: t.redirect, input: t.redirect && t.input, here: t.redirect && t.here,
		})
		t.redirect, t.input, t.here = false, false, false
	}
	t.raw.Reset()
	t.parts = nil
	t.inWord, t.quoted = false, false
}

func (t *tokenizer) endCommand() {
	t.endWord()
	if len(t.cur.words) > 0 || len(t.cur.subs) > 0 || len(t.cur.docs) > 0 {
		t.cur.pipeIn = t.pipe
		t.cmds = append(t.cmds, t.cur)
		t.pipe = false
	}
	t.cur = simple{}
	t.redirect, t.input, t.here = false, false, false
}

// tokenize is splitCommands for a line whose quotes balance; ok is false
// when they do not.
func tokenize(line string) (cmds []simple, ok bool) {
	t := &tokenizer{rs: []rune(line)}
	rs := t.rs
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case ' ', '\t', '\r':
			t.endWord()
		case '\n':
			t.endCommand()
			if len(t.pending) > 0 {
				i = t.bodies(i+1) - 1
			}
		case '(', ')':
			t.endCommand()
		case ';':
			t.endCommand()
			t.pipe = false
		case '&':
			if i+1 < len(rs) && rs[i+1] == '>' {
				// &> and &>> redirect both outputs to the target.
				t.endWord()
				i = redirectEnd(rs, i+1)
				t.redirect = true
				continue
			}
			t.endCommand()
			t.pipe = false
			if i+1 < len(rs) && rs[i+1] == '&' {
				i++
			}
		case '|':
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
			if t.inWord && !t.quoted && isDigits(t.raw.String()) {
				t.dropWord()
			}
			t.endWord()
			if c == '<' && i+1 < len(rs) && rs[i+1] == '<' && (i+2 >= len(rs) || rs[i+2] != '<') {
				end, ok := t.heredocOp(i + 2)
				if !ok {
					return nil, false
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
		case '#':
			if t.inWord {
				t.lit("#", false)
				continue
			}
			for i+1 < len(rs) && rs[i+1] != '\n' {
				i++
			}
		case '\\':
			if i+1 >= len(rs) {
				return nil, false
			}
			i++
			if rs[i] != '\n' { // a backslash-newline joins lines
				t.lit(string(rs[i]), true)
			}
		case '\'':
			end := indexRune(rs, i+1, '\'')
			if end < 0 {
				return nil, false
			}
			t.lit(string(rs[i+1:end]), true)
			i = end
		case '"':
			end, ok := t.double(i + 1)
			if !ok {
				return nil, false
			}
			i = end
		case '`':
			end, ok := t.backtick(i + 1)
			if !ok {
				return nil, false
			}
			i = end
		case '$':
			if i+1 < len(rs) && rs[i+1] == '\'' {
				s, end, ok := ansiC(rs, i+2)
				if !ok {
					return nil, false
				}
				t.lit(s, true)
				i = end
				continue
			}
			if i+1 < len(rs) && rs[i+1] == '"' {
				end, ok := t.double(i + 2)
				if !ok {
					return nil, false
				}
				i = end
				continue
			}
			end, ok := t.dollar(i)
			if !ok {
				return nil, false
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
	t.endCommand()
	if len(t.pending) > 0 {
		// The line ends before a body starts: each is empty, as in bash.
		t.bodies(len(rs))
	}
	return t.cmds, true
}

// heredocOp reads a here-document operator's delimiter, after the "<<" that
// ends just before i, and returns the index of its last rune. The body is
// read at the next newline.
func (t *tokenizer) heredocOp(i int) (int, bool) {
	delim, strip, quoted, end, ok := readDelim(t.rs, i)
	if !ok {
		return 0, false
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
		body, next := readBody(t.rs, i, p.delim, p.strip)
		i = next
		p.doc.body = body
		if !p.doc.quoted {
			p.doc.subs = bodySubs([]rune(body))
		}
	}
	t.pending = nil
	return i
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
	for ; i < len(rs) && !strings.ContainsRune(" \t\r\n;&|()<>", rs[i]); i++ {
		switch rs[i] {
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

// readBody reads a here-document's body from i, the start of a line, up to
// the line that is the delimiter (after its leading tabs, for <<-), and
// returns the body and the index just past the delimiter line. Without a
// delimiter line the body runs to the end, as in bash.
func readBody(rs []rune, i int, delim string, strip bool) (string, int) {
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
			return b.String(), i
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), len(rs)
}

// bodySubs are the command substitutions, $(...) and `...`, in an unquoted
// here-document's body, which run when the body is expanded. One that does
// not close runs to the end of the body.
func bodySubs(rs []rune) []string {
	var subs []string
	for j := 0; j < len(rs); j++ {
		switch {
		case rs[j] == '\\':
			j++
		case rs[j] == '$' && j+1 < len(rs) && rs[j+1] == '(':
			end := matchClose(rs, j+1, '(', ')')
			if end < 0 {
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
		end := matchClose(rs, i+1, '(', ')')
		if end < 0 {
			return 0, false
		}
		body := string(rs[i+2 : end])
		if strings.HasPrefix(body, "(") && strings.HasSuffix(body, ")") {
			// $((...)) is arithmetic; what it holds is read all the same.
			body = body[1 : len(body)-1]
		}
		t.cur.subs = append(t.cur.subs, body)
		t.expansion(opaquePart, "", string(rs[i:end+1]))
		return end, true
	case c == '{':
		end := matchClose(rs, i+1, '{', '}')
		if end < 0 {
			return 0, false
		}
		inner := string(rs[i+2 : end])
		if isName(inner) {
			t.expansion(paramPart, inner, string(rs[i:end+1]))
		} else {
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

// matchClose returns the index of the close that matches the open at i,
// skipping quoted text, escapes, comments and here-document bodies, or -1.
func matchClose(rs []rune, i int, open, close rune) int {
	depth := 0
	type doc struct {
		delim string
		strip bool
	}
	var pending []doc
	for j := i; j < len(rs); j++ {
		switch rs[j] {
		case '\n':
			for _, d := range pending {
				_, next := readBody(rs, j+1, d.delim, d.strip)
				j = next - 1
			}
			pending = nil
		case '#':
			if j > i && strings.ContainsRune(" \t\n;&|(", rs[j-1]) {
				for j+1 < len(rs) && rs[j+1] != '\n' {
					j++
				}
			}
		case '<':
			if j+1 < len(rs) && rs[j+1] == '<' && (j+2 >= len(rs) || rs[j+2] != '<') && rs[j-1] != '<' {
				delim, strip, _, end, ok := readDelim(rs, j+2)
				if !ok {
					return -1
				}
				pending = append(pending, doc{delim, strip})
				j = end
			}
		case '\\':
			j++
		case '\'':
			k := indexRune(rs, j+1, '\'')
			if k < 0 {
				return -1
			}
			j = k
		case '"':
			k := j + 1
			for ; k < len(rs) && rs[k] != '"'; k++ {
				if rs[k] == '\\' {
					k++
				}
			}
			if k >= len(rs) {
				return -1
			}
			j = k
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return j
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
		if strings.ContainsAny(w, "$`") {
			parts = append(parts, part{kind: opaquePart, raw: ""})
		}
		cur.words = append(cur.words, word{text: w, parts: parts, quoted: quoted})
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
	"echo": true, "printf": true, "which": true, "whereis": true, "type": true, "man": true,
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

// stripKeywords drops the reserved words and grouping tokens a command
// starts with, and `function NAME`. A for, select or case runs nothing of
// its own: nil.
func stripKeywords(args []string) []string {
	for len(args) > 0 {
		switch w := args[0]; {
		case keywords[w]:
			args = args[1:]
		case w == "function":
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
		base := baseName(w)
		wr, ok := wrappers[base]
		if !ok {
			if !inert[base] && !judged(base) {
				j := i + 1
				for j < len(args) && strings.HasPrefix(args[j], "-") {
					j++
				}
				if j < len(args) && judged(baseName(args[j])) {
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
