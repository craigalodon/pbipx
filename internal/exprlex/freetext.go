package exprlex

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/shape"
)

// FreeValue is a value read out of free text inside code: a comment or a
// description.
type FreeValue struct {
	Value   string
	Suffix  string // appended to the comment's locator: ".quoted0", ".compared1"
	Number  bool
	Columns []finding.Column
}

// FreeText reads the two extra readings of a comment or description
// (behavior.md, "Free text inside code"); judging the text whole is the
// caller's.
//
// Every quoted run with the person-name shape is returned, suffixed
// ".quotedN" with N its ordinal among the text's quoted runs. When sql is
// set, the text is also lexed as SQL, best effort, and every literal it ties
// to a column is returned, suffixed ".comparedN" with N its ordinal among
// those. A value both quoted and compared is returned once, as compared,
// with its columns.
func FreeText(text string, sql bool) []FreeValue {
	var compared []FreeValue
	if sql {
		for _, s := range commentSites(text) {
			if len(s.Columns) == 0 {
				continue
			}
			compared = append(compared, FreeValue{
				Value:   s.Value,
				Suffix:  ".compared" + strconv.Itoa(len(compared)),
				Number:  s.Number,
				Columns: s.Columns,
			})
		}
	}
	var out []FreeValue
	for n, run := range quotedRuns(text) {
		if !shape.PersonName(run) {
			continue
		}
		if slices.ContainsFunc(compared, func(c FreeValue) bool { return !c.Number && c.Value == run }) {
			continue
		}
		out = append(out, FreeValue{Value: run, Suffix: ".quoted" + strconv.Itoa(n)})
	}
	return append(out, compared...)
}

// commentSites lexes a comment as SQL: as is; then with prose apostrophes
// blanked; then line by line, keeping the lines that lex either way.
func commentSites(text string) []Site {
	if s, ok := sqlSitesOf(text); ok {
		return s
	}
	blanked := blankApostrophes(text)
	if s, ok := sqlSitesOf(blanked); ok {
		return s
	}
	var out []Site
	for _, line := range strings.Split(text, "\n") {
		s, ok := sqlSitesOf(line)
		if !ok {
			s, ok = sqlSitesOf(blankApostrophes(line))
		}
		if ok {
			out = append(out, s...)
		}
	}
	return out
}

func sqlSitesOf(text string) ([]Site, bool) {
	s, _, err := SQLSites(text)
	return s, err == nil
}

// blankApostrophes turns an apostrophe between two letters (don't, Doe's)
// into a space, so prose does not open a string.
func blankApostrophes(s string) string {
	rs := []rune(s)
	for i := 1; i+1 < len(rs); i++ {
		if rs[i] == '\'' && unicode.IsLetter(rs[i-1]) && unicode.IsLetter(rs[i+1]) {
			rs[i] = ' '
		}
	}
	return string(rs)
}

// quotedRuns lists the runs of text inside quotes, straight or curly, each
// ending at its closing quote or the end of its line. A straight or curly
// apostrophe inside a word (O'Doe, don't) neither opens nor closes a run.
func quotedRuns(text string) []string {
	var out []string
	rs := []rune(text)
	inWord := func(i int) bool {
		return i > 0 && i+1 < len(rs) && isWordRune(rs[i-1]) && isWordRune(rs[i+1])
	}
	for i := 0; i < len(rs); i++ {
		closers := quoteClosers(rs[i])
		if closers == "" || inWord(i) {
			continue
		}
		if apostrophe(rs[i]) && i > 0 && isWordRune(rs[i-1]) {
			continue // a possessive (students') opens nothing
		}
		j := i + 1
		for ; j < len(rs) && rs[j] != '\n'; j++ {
			if strings.ContainsRune(closers, rs[j]) && !inWord(j) {
				break
			}
		}
		out = append(out, string(rs[i+1:j]))
		if j < len(rs) && rs[j] == '\n' {
			j--
		}
		i = j
	}
	return out
}

// quoteClosers returns the quotes that close a run opened by r, or "" when r
// opens none.
func quoteClosers(r rune) string {
	switch r {
	case '"':
		return `"`
	case '\'':
		return `'`
	case '“', '”', '„':
		return "”“"
	case '‘', '’', '‚':
		return "’‘"
	}
	return ""
}

func apostrophe(r rune) bool { return r == '\'' || r == '’' }

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
