// Package shape recognizes personal-data shapes in a literal or a line of
// prose (docs/design/behavior.md, "Shapes" and "Prose").
//
// The functions here judge plain strings and return reasons. They never
// return the string they judged; a prose hit carries its line as a
// secret.Value (principle 2).
package shape

import (
	"regexp"

	"github.com/craigalodon/pbipx/internal/finding"
)

// Switches turns shapes off. The zero value checks every shape, so a caller
// that forgets to set it fails towards blocking. Policy's per-shape switches
// (all on by default) map to these negated.
type Switches struct {
	PersonNameOff bool
	EmailOff      bool
	SSNOff        bool
	DateOff       bool
	PhoneOff      bool
}

const (
	// ws is any whitespace, including Unicode spaces such as no-break space.
	ws = `[\s\p{Zs}]`
	// initial is an uppercase or titlecase letter.
	initial = `[\p{Lu}\p{Lt}]`
	// last is one or more letter runs joined by a hyphen or a straight or
	// curly apostrophe, starting with an initial. Combining marks count as
	// letters, so a decomposed accent does not end a run.
	last = initial + `[\p{L}\p{M}]*(?:[-'’][\p{L}\p{M}]+)*`
	// notWord is a character that cannot continue a word or number, used to
	// make a match a whole word where \b does not fit (it is ASCII-only, and
	// cannot sit before "+" or "(").
	notWord = `[^\p{L}\p{N}_]`
)

var (
	personNameRE = regexp.MustCompile(`^` + ws + `*` + last + `,` + ws + `+` + initial)
	emailRE      = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	ssnRE        = regexp.MustCompile(`(?:^|` + notWord + `)\d{3}-\d{2}-\d{4}(?:$|` + notWord + `)`)
	// Years 19xx to 21xx: the 21xx years are a divergence from the
	// predecessor, so synthetic 2100s fixtures exercise the shape. Month and
	// day are not range-checked: a shape check errs towards blocking.
	dateRE = regexp.MustCompile(
		`(?:^|` + notWord + `)(?:` +
			`(?:19|20|21)\d{2}-\d{2}-\d{2}` +
			`|\d{1,2}/\d{1,2}/(?:19|20|21)\d{2}` +
			`)(?:$|` + notWord + `)`)
	// A North American number: an optional +1, an area code that may be
	// parenthesized, then ddd-dddd, separated by "-", "." or a space.
	phoneRE = regexp.MustCompile(
		`(?:^|` + notWord + `)(?:\+1[-. ]?)?(?:\(\d{3}\)[-. ]?|\d{3}[-. ])\d{3}[-. ]\d{4}(?:$|` + notWord + `)`)
)

// PersonName reports whether s starts "Last, First": Last is letter runs
// joined by hyphens or apostrophes, then a comma, whitespace and a letter,
// with both initials uppercase or titlecase. Leading whitespace is ignored.
func PersonName(s string) bool { return personNameRE.MatchString(s) }

// Email reports whether s holds local@domain.tld anywhere.
func Email(s string) bool { return emailRE.MatchString(s) }

// SSN reports whether s holds ddd-dd-dddd as a whole word anywhere.
func SSN(s string) bool { return ssnRE.MatchString(s) }

// Date reports whether s holds an ISO yyyy-mm-dd or US m/d/yyyy date as a
// whole word anywhere, with a year from 1900 to 2199.
func Date(s string) bool { return dateRE.MatchString(s) }

// Phone reports whether s holds a North American phone number as a whole
// word anywhere.
func Phone(s string) bool { return phoneRE.MatchString(s) }

// Literal returns the shapes s has, in reason order, skipping those sw
// turns off. Whether a literal is judged at all (an empty, allowlisted or
// numeric literal is not) is the caller's decision.
func Literal(s string, sw Switches) []finding.Reason {
	var reasons []finding.Reason
	if !sw.PersonNameOff && PersonName(s) {
		reasons = append(reasons, finding.PersonNameShape)
	}
	reasons = appendAnywhere(reasons, s, sw)
	if !sw.DateOff && Date(s) {
		reasons = append(reasons, finding.DateShape)
	}
	if !sw.PhoneOff && Phone(s) {
		reasons = append(reasons, finding.PhoneShape)
	}
	return reasons
}

// appendAnywhere appends the email and SSN shapes, which literals and prose
// check the same way.
func appendAnywhere(reasons []finding.Reason, s string, sw Switches) []finding.Reason {
	if !sw.EmailOff && Email(s) {
		reasons = append(reasons, finding.EmailShape)
	}
	if !sw.SSNOff && SSN(s) {
		reasons = append(reasons, finding.SSNShape)
	}
	return reasons
}

// identifierKey and hexKey are the keys PrintableKey lets through: an
// identifier, or a hex run with at least one letter, all lowercase or all
// uppercase (Power BI's visual and section names).
var (
	identifierKey = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	hexKey        = regexp.MustCompile(`^(?:[0-9a-f]*[a-f][0-9a-f]*|[0-9A-F]*[A-F][0-9A-F]*)$`)
)

// PrintableKey reports whether a key read from a scanned or configuration
// file may be printed or written into a locator: it is an identifier or a
// hex name, and it has no personal-data shape. Any other key is free text
// that could hold a value, and is printed as a placeholder.
func PrintableKey(key string) bool {
	if !identifierKey.MatchString(key) && !hexKey.MatchString(key) {
		return false
	}
	return len(Literal(key, Switches{})) == 0
}
