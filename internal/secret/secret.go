// Package secret holds values read from scanned files.
//
// A Value cannot render itself. Every formatting, marshalling and logging path
// prints Redacted, and Reveal is the only way back to the plaintext
// (docs/design/principles.md, principle 2). Code that judges a literal works
// on the plain string; anything that leaves it, such as a finding or an error,
// carries a Value.
package secret

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// Redacted is what a Value prints as, everywhere.
const Redacted = "<redacted>"

// Value is a redacted string. The zero Value holds the empty string.
//
// The plaintext sits behind a pointer. When fmt prints a struct holding a Value
// in an unexported field, it cannot call the field's methods and prints its
// contents instead; behind a pointer, that is an address, not the text.
type Value struct {
	p *string
}

// New wraps s.
func New(s string) Value {
	return Value{p: &s}
}

// Reveal returns the plaintext. Only the interactive --show-values path may
// call it.
func Reveal(v Value) string {
	if v.p == nil {
		return ""
	}
	return *v.p
}

// String implements fmt.Stringer.
func (Value) String() string { return Redacted }

// GoString implements fmt.GoStringer, for %#v.
func (Value) GoString() string { return "secret.Value(" + Redacted + ")" }

// Format implements fmt.Formatter, so every verb, %q and %x included, prints
// Redacted.
func (Value) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		_, _ = io.WriteString(f, "secret.Value("+Redacted+")")
		return
	}
	_, _ = io.WriteString(f, Redacted)
}

// MarshalText implements encoding.TextMarshaler.
func (Value) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

// MarshalJSON implements json.Marshaler.
func (Value) MarshalJSON() ([]byte, error) { return json.Marshal(Redacted) }

// LogValue implements slog.LogValuer.
func (Value) LogValue() slog.Value { return slog.StringValue(Redacted) }
