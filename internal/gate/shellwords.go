package gate

import (
	"strings"
)

// word is one word of a simple command, with its quotes removed.
type word struct {
	text string
	// quoted is true when any part of the word was quoted or escaped, so
	// the shell would not expand a glob in it.
	quoted bool
	// redirect is true for a redirection's target, which is not an
	// argument of the command.
	redirect bool
}

// simple is one simple command: its words in order, redirection targets
// included and marked.
type simple []word

// args is the command's words without redirection targets.
func (s simple) args() []string {
	var out []string
	for _, w := range s {
		if !w.redirect {
			out = append(out, w.text)
		}
	}
	return out
}

// splitCommands splits a shell command line into simple commands at ;, &&,
// ||, |, |&, &, parentheses and newlines, the way a shell reads it, without
// expanding anything. Quotes are removed from words; a redirection's target
// is kept as a word marked redirect. A comment runs to the end of its line.
// With unbalanced quotes, or a trailing backslash, the line is split on
// whitespace and those operators instead, so nothing is hidden inside an
// unterminated quote.
func splitCommands(line string) []simple {
	if cmds, ok := tokenize(line); ok {
		return cmds
	}
	return splitFields(line)
}

// tokenize is splitCommands for a line whose quotes balance; ok is false
// when they do not.
func tokenize(line string) (cmds []simple, ok bool) {
	var (
		cur      simple
		buf      strings.Builder
		inWord   bool
		quoted   bool
		redirect bool // the next word is a redirection's target
	)
	endWord := func() {
		if inWord {
			cur = append(cur, word{text: buf.String(), quoted: quoted, redirect: redirect})
			redirect = false
		}
		buf.Reset()
		inWord, quoted = false, false
	}
	endCommand := func() {
		endWord()
		if len(cur) > 0 {
			cmds = append(cmds, cur)
		}
		cur = nil
		redirect = false
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case ' ', '\t', '\r':
			endWord()
		case '\n', ';', '(', ')':
			endCommand()
		case '&':
			if i+1 < len(rs) && rs[i+1] == '>' {
				// &> and &>> redirect both outputs to the target.
				endWord()
				i = redirectEnd(rs, i+1)
				redirect = true
				continue
			}
			endCommand()
			if i+1 < len(rs) && rs[i+1] == '&' {
				i++
			}
		case '|':
			endCommand()
			if i+1 < len(rs) && (rs[i+1] == '|' || rs[i+1] == '&') {
				i++
			}
		case '<', '>':
			// A word of digits just before is the descriptor: part of the
			// operator, not an argument.
			if inWord && !quoted && isDigits(buf.String()) {
				buf.Reset()
				inWord = false
			}
			endWord()
			i = redirectEnd(rs, i)
			// >&2, <&0, >&- duplicate a descriptor and have no target.
			if rs[i] == '&' && i+1 < len(rs) && (isDigit(rs[i+1]) || rs[i+1] == '-') {
				for i+1 < len(rs) && (isDigit(rs[i+1]) || rs[i+1] == '-') {
					i++
				}
				continue
			}
			redirect = true
		case '#':
			if inWord {
				buf.WriteRune(c)
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
				buf.WriteRune(rs[i])
				inWord, quoted = true, true
			}
		case '\'':
			end := indexRune(rs, i+1, '\'')
			if end < 0 {
				return nil, false
			}
			buf.WriteString(string(rs[i+1 : end]))
			inWord, quoted = true, true
			i = end
		case '"':
			j := i + 1
			for ; j < len(rs) && rs[j] != '"'; j++ {
				if rs[j] == '\\' && j+1 < len(rs) && strings.ContainsRune("\\\"$`\n", rs[j+1]) {
					j++
				}
				buf.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, false
			}
			inWord, quoted = true, true
			i = j
		default:
			buf.WriteRune(c)
			inWord = true
		}
	}
	endCommand()
	return cmds, true
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
// split on whitespace, and a command ends at a word that is an operator.
func splitFields(line string) []simple {
	var cmds []simple
	var cur simple
	for f := range strings.FieldsSeq(line) {
		switch f {
		case ";", "&&", "||", "|", "|&", "&", "(", ")":
			if len(cur) > 0 {
				cmds = append(cmds, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, word{text: f, quoted: strings.ContainsAny(f, `'"\`)})
	}
	if len(cur) > 0 {
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
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		letter := r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'
		if !letter && (i == 0 || !isDigit(r)) {
			return false
		}
	}
	return true
}

// wrappers run the command that follows them, after their own options.
var wrappers = map[string]bool{
	"env": true, "command": true, "exec": true, "nohup": true, "time": true,
	"nice": true, "sudo": true, "xargs": true, "builtin": true, "stdbuf": true,
	"timeout": true,
}

// program returns the command a simple command runs, by its base name, and
// the arguments after it: leading assignments are skipped, and so are
// wrappers such as env, with their options and assignments. A wrapper's
// option that takes a separate value can be mistaken for the command; the
// cost is a missed rule, which other layers cover (docs/design/threat-model.md).
func program(args []string) (name string, rest []string) {
	i := 0
	for i < len(args) {
		w := args[i]
		switch {
		case isAssignment(w):
			i++
			continue
		case wrappers[baseName(w)]:
			i++
			for i < len(args) && (strings.HasPrefix(args[i], "-") || isAssignment(args[i])) {
				i++
			}
			// timeout takes its duration before the command.
			if baseName(w) == "timeout" && i < len(args) {
				i++
			}
			continue
		}
		return baseName(w), args[i+1:]
	}
	return "", nil
}

// baseName is the last element of a slash-separated command path.
func baseName(w string) string {
	if i := strings.LastIndex(w, "/"); i >= 0 {
		return w[i+1:]
	}
	return w
}
