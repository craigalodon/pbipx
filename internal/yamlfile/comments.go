package yamlfile

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

// comment is one YAML comment: its text after the #, trimmed, and its line.
type comment struct {
	text string
	line int
}

// allComments is every comment of a file: those the scanner finds in the
// text, and those the parser attached to the nodes of docs, so that a
// mistake in either cannot by itself hide a comment. The two sets are
// merged as multisets of text: a parser comment is added only where the
// scanner found fewer comments with its text. (The parser does not say
// which line a comment is on, only the node it is attached to, so text is
// the key the two share.)
func allComments(src string, docs []*yaml.Node) []comment {
	out := comments(src)
	found := map[string]int{}
	for _, c := range out {
		found[c.text]++
	}
	for _, c := range nodeComments(docs) {
		if found[c.text] > 0 {
			found[c.text]--
			continue
		}
		out = append(out, c)
	}
	return out
}

// nodeComments is every head, line and foot comment the parser attached to
// a node of docs, one per line of comment text. Aliases are not followed:
// every node is reached once, through its parent's Content.
func nodeComments(docs []*yaml.Node) []comment {
	var out []comment
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for _, text := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			for _, l := range strings.Split(text, "\n") {
				l = strings.TrimSpace(l)
				if l == "" {
					continue
				}
				// The parser keeps the #. Text without one is judged whole.
				out = append(out, comment{text: strings.TrimSpace(strings.TrimPrefix(l, "#")), line: n.Line})
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, d := range docs {
		walk(d)
	}
	return out
}

// comments finds the comments of a YAML text: a # at the start of a line or
// after whitespace, outside any scalar. It follows the scalars that can hide
// a #: quoted ones, which may span lines, and block scalars (| and >),
// whose content is every following line indented more than the node that
// owns them. A # inside a plain scalar with no whitespace before it is text.
//
// A quote or a block indicator opens a scalar only where a node can start:
// at the start of a line that does not continue a plain scalar, after a
// key's :, after a sequence entry's - or an explicit key's ?, after a
// document marker, and at the start of a flow collection or after its
// commas. On a line that continues a multi-line plain scalar it is text.
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

	// A plain scalar that may continue on the next line: plain is set when
	// the last line ended inside one. In block context the next line
	// continues it when indented more than plainOwner, the column of the
	// node that owns it; in a flow collection any line does.
	plain      bool
	plainOwner int

	flow int // depth of [ ] and { } collections
}

func isBlank(b byte) bool { return b == ' ' || b == '\t' }

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func (c *commentScanner) line(line string, n int) {
	if strings.TrimSpace(line) == "" {
		// A blank line ends nothing: not a block scalar, a quoted scalar
		// nor a plain one, which folds it.
		return
	}
	if c.block {
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
	// scalar. owner is the column a block scalar here would belong to, and
	// owned says whether an indicator on this line (:, -, ?, a document
	// marker) set it.
	start := c.quote == 0
	owner := indentOf(line)
	owned := false
	keyCol := owner
	marker := c.quote == 0 && (strings.HasPrefix(line, "---") || strings.HasPrefix(line, "...")) &&
		(len(line) == 3 || isBlank(line[3]))
	// inPlain is set while the scan is inside a plain scalar, and
	// plainOwner is that scalar's owner.
	inPlain := false
	plainOwner := c.plainOwner
	switch {
	case marker:
		c.flow = 0
		owner, owned = -1, true
		line = strings.Repeat(" ", 3) + line[3:]
	case c.quote == 0 && c.plain && (c.flow > 0 || indentOf(line) > c.plainOwner):
		// A continuation line of a plain scalar: text up to a comment, or
		// in a flow collection up to its next indicator.
		start, inPlain = false, true
	}
	c.plain = false
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
			// A comment, which also ends a plain scalar.
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
			owner, owned = i, true // a sequence entry or explicit key
		case b == '[' || b == '{':
			if start || c.flow > 0 {
				c.flow++
				start, inPlain = true, false
			} else {
				start = false
			}
		case (b == ']' || b == '}') && c.flow > 0:
			c.flow--
			start, inPlain = false, false
		case b == ',' && c.flow > 0:
			start, inPlain = true, false
		case b == ':' && (i+1 == len(line) || isBlank(line[i+1]) || (c.flow > 0 && strings.IndexByte(",]}", line[i+1]) >= 0)):
			owner, owned = keyCol, true // a value: a scalar here belongs to its key
			start, inPlain = true, false
		default:
			if start {
				// A plain scalar starts. One that is first on its line,
				// with no indicator before it, is owned by a node less
				// indented than it.
				keyCol = i
				inPlain, plainOwner = true, owner
				if !owned {
					plainOwner = owner - 1
				}
			}
			start = false
		}
	}
	c.plain, c.plainOwner = inPlain, plainOwner
}
