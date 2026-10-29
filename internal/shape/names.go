package shape

import (
	"regexp"
	"unicode"
)

// The placeholders explain prints in place of a name it will not print
// (docs/design/explain.md, "Names and placeholders").
const (
	// Withheld replaces a model object name with a personal-data shape.
	Withheld = "<withheld>"
	// NotIdentifier replaces a part of a relation name, or a navigation
	// step, that is not a plain identifier or has a shape.
	NotIdentifier = "<not an identifier>"
	// Dynamic replaces a template call whose arguments are not quoted names.
	Dynamic = "<dynamic>"
)

// plainIdentifier is letters, digits and _, not all digits.
var plainIdentifier = regexp.MustCompile(`^[A-Za-z0-9_]*[A-Za-z_][A-Za-z0-9_]*$`)

// ObjectName is a model object's name as explain prints it: the name
// itself, or Withheld when it has any personal-data shape. A name that is
// empty or holds a control character, which no renderer can show safely, is
// withheld too.
func ObjectName(name string) string {
	if name == "" || len(Literal(name, Switches{})) > 0 {
		return Withheld
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return Withheld
		}
	}
	return name
}

// IdentifierPart is one part of a relation name, or a navigation step, as
// explain prints it: the part itself when it is a plain identifier (letters,
// digits and _, not all digits) with no shape, and NotIdentifier otherwise.
// A name read from a string literal is printed only this way (principle 11).
func IdentifierPart(part string) string {
	if !plainIdentifier.MatchString(part) || len(Literal(part, Switches{})) > 0 {
		return NotIdentifier
	}
	return part
}

// HiddenName replaces a table or column name a finding will not print.
const HiddenName = "{?}"

// DisplayName is a table or column name as a finding prints it: the name
// itself, or HiddenName when it is empty, has a personal-data shape, or
// holds a control character. Names with spaces, which Power BI models use
// freely, are printed.
func DisplayName(name string) string {
	if ObjectName(name) == Withheld {
		return HiddenName
	}
	return name
}
