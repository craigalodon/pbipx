package yamlfile

import "strings"

// comment is one YAML comment: its text after the #, trimmed, and its line.
type comment struct {
	text string
	line int
}

// comments finds the comments of a YAML text: a # at the start of a line or
// after whitespace, outside any scalar. It follows the scalars that can hide
// a #: quoted ones, which may span lines, and block scalars (| and >),
// whose content is every following line indented more than the node that
// owns them. A # inside a plain scalar with no whitespace before it is text.
//
// It runs on text the parser has accepted, so it does not check syntax.
func comments(src string) []comment {
	c := &commentScanner{}
	for i, line := range strings.Split(src, "\n") {
		c.line(strings.TrimSuffix(line, "\r"), i+1)
	}
	return c.out
}

type commentScanner struct {
	out []comment

	quote byte // the open quote of a scalar that continues on the next line

	// A block scalar: block is set from its header to its last content
	// line. owner is the column of the node that owns it; content lines are
	// indented more. indent is the content's indentation, once known (-1
	// before its first non-blank line).
	block  bool
	owner  int
	indent int

	flow int // depth of [ ] and { } collections
}

func isBlank(b byte) bool { return b == ' ' || b == '\t' }

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func (c *commentScanner) line(line string, n int) {
	if c.block {
		if strings.TrimSpace(line) == "" {
			return
		}
		ind := indentOf(line)
		if c.indent < 0 && ind > c.owner {
			c.indent = ind
		}
		if c.indent >= 0 && ind >= c.indent {
			return
		}
		c.block = false
	}
	// At a token start a quote opens a quoted scalar, and | or > a block
	// scalar. owner is the column a block scalar here would belong to.
	start := c.quote == 0
	owner := indentOf(line)
	keyCol := owner
	if c.quote == 0 && (strings.HasPrefix(line, "---") || strings.HasPrefix(line, "...")) &&
		(len(line) == 3 || isBlank(line[3])) {
		c.flow = 0
		owner = -1
		line = strings.Repeat(" ", 3) + line[3:]
	}
	for i := 0; i < len(line); i++ {
		b := line[i]
		if c.quote != 0 {
			switch {
			case c.quote == '"' && b == '\\':
				i++ // an escape: the next byte is text
			case b == c.quote && c.quote == '\'' && i+1 < len(line) && line[i+1] == '\'':
				i++ // '' stands for '
			case b == c.quote:
				c.quote = 0
			}
			continue
		}
		switch {
		case isBlank(b):
			continue
		case b == '#' && (i == 0 || isBlank(line[i-1])):
			c.out = append(c.out, comment{text: strings.TrimSpace(line[i+1:]), line: n})
			return
		case start && (b == '"' || b == '\''):
			c.quote = b
			keyCol = i
			start = false
		case start && (b == '|' || b == '>') && c.flow == 0:
			c.block, c.owner, c.indent = true, owner, -1
			start = false
		case start && (b == '&' || b == '!' || b == '*'):
			// An anchor, tag or alias: a token up to the next blank.
			for i+1 < len(line) && !isBlank(line[i+1]) {
				i++
			}
			start = b != '*'
		case start && (b == '-' || b == '?') && (i+1 == len(line) || isBlank(line[i+1])):
			owner = i // a sequence entry or explicit key
		case b == '[' || b == '{':
			if start || c.flow > 0 {
				c.flow++
				start = true
			} else {
				start = false
			}
		case (b == ']' || b == '}') && c.flow > 0:
			c.flow--
			start = false
		case b == ',' && c.flow > 0:
			start = true
		case b == ':' && (i+1 == len(line) || isBlank(line[i+1]) || (c.flow > 0 && strings.IndexByte(",]}", line[i+1]) >= 0)):
			owner = keyCol // a value: a block scalar here belongs to its key
			start = true
		default:
			if start {
				keyCol = i
			}
			start = false
		}
	}
}
