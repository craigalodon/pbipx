package shape

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/secret"
)

// Prose rules (docs/design/behavior.md, "Prose"): email, SSN and phone count
// anywhere in a line; the date shape is not checked; the name shape counts
// only where a name stands alone.

// leadMarkers matches the Markdown markers before a line's text: quote
// markers, headings, list bullets, list numbers and task boxes, in any
// nesting.
var leadMarkers = regexp.MustCompile(`^(?:` + ws + `*(?:>|#{1,6}` + ws + `|[-*+]` + ws + `|\d{1,9}[.)]` + ws + `|\[[ xX]\]` + ws + `))*`)

// wordTail continues a word: letters, marks, digits, "_", apostrophes, "."
// and "-". A comma ends a word and is not part of one.
const wordTail = `[\p{L}\p{M}\p{N}_'’.\-]*`

// aloneRE is a name standing alone: Word, Word, then at most two more words
// separated by whitespace, and nothing else. The first two words have the
// name shape; the extra words start with a letter but need not be
// capitalized.
var aloneRE = regexp.MustCompile(`^` + last + `,` + ws + `+` + initial + wordTail + `(?:` + ws + `+\p{L}` + wordTail + `){0,2}$`)

// ProseLine returns the shapes one line of prose has, in reason order,
// skipping those sw turns off. Each literal in allow is replaced with a space
// before the line is checked.
func ProseLine(line string, allow []string, sw Switches) []finding.Reason {
	line = clean(line, allow)
	var reasons []finding.Reason
	if !sw.PersonNameOff && nameStandsAlone(line) {
		reasons = append(reasons, finding.PersonNameShape)
	}
	reasons = appendAnywhere(reasons, line, sw)
	if !sw.PhoneOff && Phone(line) {
		reasons = append(reasons, finding.PhoneShape)
	}
	return reasons
}

// clean replaces each allowlisted literal with a space, so the words around
// it cannot join into a new match, and un-doubles apostrophes. Removal runs
// before and after un-doubling, so an allowlisted name matches whether the
// line quotes it SQL-style or not. Longer literals go first, so one that
// contains another is removed whole. A literal is removed only where it is
// word-bounded, with no letter or digit next to it on either side, so an
// allowed word cannot cut down a name it starts ("All" in "Allen, Jane").
func clean(line string, allow []string) string {
	sorted := slices.Clone(allow)
	slices.SortStableFunc(sorted, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	remove := func(s string) string {
		for _, a := range sorted {
			if a != "" {
				s = removeBounded(s, a)
			}
		}
		return s
	}
	return remove(strings.ReplaceAll(remove(line), "''", "'"))
}

// removeBounded replaces each occurrence of lit in s with a space where
// neither the rune before it nor the rune after it is a word rune.
func removeBounded(s, lit string) string {
	var b strings.Builder
	start := 0 // s[:start] is written to b
	from := 0  // where the next search begins
	for {
		i := strings.Index(s[from:], lit)
		if i < 0 {
			b.WriteString(s[start:])
			return b.String()
		}
		i += from
		end := i + len(lit)
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[end:])
		if (i == 0 || !wordRune(before)) && (end == len(s) || !wordRune(after)) {
			b.WriteString(s[start:i])
			b.WriteString(" ")
			start, from = end, end
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		from = i + size
	}
}

// nameStandsAlone reports whether the line, after its lead markers, or any
// one field of it, is a name and at most two more words.
func nameStandsAlone(line string) bool {
	if standsAlone(line[len(leadMarkers.FindString(line)):]) {
		return true
	}
	return slices.ContainsFunc(fields(line), standsAlone)
}

func standsAlone(s string) bool {
	return aloneRE.MatchString(strings.TrimFunc(s, unicode.IsSpace))
}

// fields splits a line where a pasted row or a quotation separates values:
// at tabs, pipes, double quotes, backticks and curly quotes, and at straight
// or curly apostrophes that are not inside a word. An apostrophe between two
// letters or digits (a contraction, O'Doe) does not split. The right single
// quote is treated as an apostrophe, because it is also the typographic one.
func fields(line string) []string {
	rs := []rune(line)
	var out []string
	start := 0
	for i, r := range rs {
		if !splits(rs, i, r) {
			continue
		}
		out = append(out, string(rs[start:i]))
		start = i + 1
	}
	if start == 0 {
		return nil // no split: the whole line was already judged
	}
	return append(out, string(rs[start:]))
}

func splits(rs []rune, i int, r rune) bool {
	switch r {
	case '\t', '|', '"', '`', '“', '”', '„', '‟', '‘', '‚', '‛':
		return true
	case '\'', '’':
		return i <= 0 || i+1 >= len(rs) || !wordRune(rs[i-1]) || !wordRune(rs[i+1])
	}
	return false
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r)
}

// Hit is a line of prose with a shape. Line is 1-based.
type Hit struct {
	Line    int
	Reasons []finding.Reason
	Text    secret.Value // the line as written
}

// ProseOptions configures Prose.
type ProseOptions struct {
	Allow    []string // allowlisted literals, replaced with a space in each line
	Switches Switches
	// Scissors, when not empty, is a commit message's scissors line (see
	// ScissorsLine): scanning stops before the first line equal to it, as
	// git discards that line and everything below. Comment lines are
	// scanned: a hook cannot tell which of them git's cleanup will keep.
	Scissors string
}

// scissorsMark follows the comment character on git's scissors line.
const scissorsMark = " ------------------------ >8 ------------------------"

// ScissorsLine is git's scissors line for a comment character (or string).
func ScissorsLine(comment string) string { return comment + scissorsMark }

// bom is the UTF-8 byte order mark.
const bom = "\xef\xbb\xbf"

// Prose checks each line of text and returns one hit per line with a
// shape. Lines end at "\n", with a trailing "\r" dropped; a leading byte
// order mark is ignored.
func Prose(text string, opts ProseOptions) []Hit {
	text = strings.TrimPrefix(text, bom)
	var hits []Hit
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if opts.Scissors != "" && line == opts.Scissors {
			break
		}
		if reasons := ProseLine(line, opts.Allow, opts.Switches); len(reasons) > 0 {
			hits = append(hits, Hit{Line: i + 1, Reasons: reasons, Text: secret.New(line)})
		}
	}
	return hits
}
