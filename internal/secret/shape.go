package secret

import (
	"strconv"
	"strings"
	"unicode"
)

// Shape returns v's silhouette for --show-shape: uppercase and titlecase
// letters become A, lowercase letters a, digits 9; spaces and punctuation are
// kept; anything else becomes ".". A run of the same class is written with
// its length, so "Doe, Jane" is "Aa{2}, Aa{3}". The silhouette shows length
// and character classes and no letter of the value (principle 2).
func Shape(v Value) string {
	if v.p == nil {
		return ""
	}
	var b strings.Builder
	var prev rune
	n := 0
	flush := func() {
		if n == 0 {
			return
		}
		b.WriteRune(prev)
		if n > 1 {
			b.WriteString("{" + strconv.Itoa(n) + "}")
		}
		n = 0
	}
	for _, r := range *v.p {
		c, kept := class(r)
		if kept {
			flush()
			b.WriteRune(c)
			continue
		}
		if c != prev {
			flush()
		}
		prev = c
		n++
	}
	flush()
	return b.String()
}

// class maps r to its silhouette rune. kept is true for a space or
// punctuation, which is written as itself and never run-length encoded.
func class(r rune) (c rune, kept bool) {
	switch {
	case unicode.IsUpper(r) || unicode.IsTitle(r):
		return 'A', false
	case unicode.IsLower(r):
		return 'a', false
	case unicode.IsDigit(r):
		return '9', false
	case r == ' ':
		return r, true
	case unicode.IsPunct(r) && unicode.IsPrint(r):
		return r, true
	case r < unicode.MaxASCII && unicode.IsSymbol(r):
		return r, true // ASCII symbols such as + < = > are punctuation too
	}
	return '.', false
}
