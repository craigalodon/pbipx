package gate

import (
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
// the end of its line. With unbalanced quotes, or a trailing backslash, the
// line is split on whitespace and those operators instead, so nothing is
// hidden inside an unterminated quote.
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
	pipe     bool // the next command reads this one's output
}

func (t *tokenizer) lit(s string, quoted bool) {
	t.inWord = true
	t.quoted = t.quoted || quoted
	t.raw.WriteString(s)
	if n := len(t.parts); n > 0 && t.parts[n-1].kind == litPart && t.parts[n-1].quoted == quoted {
		t.parts[n-1].text += s
		return
	}
	t.parts = append(t.parts, part{kind: litPart, text: s, quoted: quoted})
}

func (t *tokenizer) expansion(kind partKind, text, raw string) {
	t.inWord = true
	t.raw.WriteString(raw)
	t.parts = append(t.parts, part{kind: kind, text: text, raw: raw})
}

func (t *tokenizer) endWord() {
	if t.inWord {
		t.cur.words = append(t.cur.words, word{text: t.raw.String(), parts: t.parts, quoted: t.quoted, redirect: t.redirect})
		t.redirect = false
	}
	t.raw.Reset()
	t.parts = nil
	t.inWord, t.quoted = false, false
}

func (t *tokenizer) endCommand() {
	t.endWord()
	if len(t.cur.words) > 0 || len(t.cur.subs) > 0 {
		t.cur.pipeIn = t.pipe
		t.cmds = append(t.cmds, t.cur)
		t.pipe = false
	}
	t.cur = simple{}
	t.redirect = false
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
		case '\n', '(', ')':
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
				t.raw.Reset()
				t.parts = nil
				t.inWord = false
			}
			t.endWord()
			i = redirectEnd(rs, i)
			// >&2, <&0, >&- duplicate a descriptor and have no target.
			if rs[i] == '&' && i+1 < len(rs) && (isDigit(rs[i+1]) || rs[i+1] == '-') {
				for i+1 < len(rs) && (isDigit(rs[i+1]) || rs[i+1] == '-') {
					i++
				}
				continue
			}
			t.redirect = true
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
	return t.cmds, true
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
// skipping quoted text and escapes, or -1.
func matchClose(rs []rune, i int, open, close rune) int {
	depth := 0
	for j := i; j < len(rs); j++ {
		switch rs[j] {
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
// split on whitespace, and a command ends at a word that is an operator. A
// word with a $ or a backtick holds an opaque part.
func splitFields(line string) []simple {
	var cmds []simple
	var cur simple
	pipe := false
	for f := range strings.FieldsSeq(line) {
		switch f {
		case ";", "&&", "||", "|", "|&", "&", "(", ")":
			if len(cur.words) > 0 {
				cur.pipeIn = pipe
				cmds = append(cmds, cur)
			}
			cur = simple{}
			pipe = f == "|" || f == "|&"
			continue
		}
		quoted := strings.ContainsAny(f, `'"\`)
		parts := []part{{kind: litPart, text: f, quoted: quoted}}
		if strings.ContainsAny(f, "$`") {
			parts = append(parts, part{kind: opaquePart, raw: ""})
		}
		cur.words = append(cur.words, word{text: f, parts: parts, quoted: quoted})
	}
	if len(cur.words) > 0 {
		cur.pipeIn = pipe
		cmds = append(cmds, cur)
	}
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

// wrappers run the command that follows them, after their own options.
// Each maps to the short options that take a separate value.
var wrappers = map[string]string{
	"env": "uCS", "command": "", "exec": "a", "nohup": "", "time": "fo",
	"nice": "n", "sudo": "ugCDhprtUT", "doas": "uC", "xargs": "IndPaELsE", "builtin": "",
	"stdbuf": "ioe", "timeout": "sk", "ionice": "cnp",
	"setsid": "", "caffeinate": "tw",
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
// options and assignments.
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
		withValue, ok := wrappers[base]
		if !ok {
			return base, args[i+1:], under
		}
		under = append(under, base)
		i++
		for i < len(args) && (strings.HasPrefix(args[i], "-") || isAssignment(args[i])) {
			o := args[i]
			i++
			// A short option that takes a value, given apart, takes the next word.
			if len(o) == 2 && o[0] == '-' && strings.IndexByte(withValue, o[1]) >= 0 && i < len(args) {
				i++
			}
			if o == "--" {
				break
			}
		}
		// timeout takes its duration before the command.
		if base == "timeout" && i < len(args) {
			i++
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
