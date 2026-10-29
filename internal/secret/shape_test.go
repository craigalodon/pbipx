package secret

import (
	"strings"
	"testing"
	"unicode"
)

func TestShape(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"name", "Doe, Jane", "Aa{2}, Aa{3}"},
		{"email", "jane.doe@example.com", "a{4}.a{3}@a{7}.a{3}"},
		{"phone", "(555) 555-0142", "(9{3}) 9{3}-9{4}"},
		{"ssn", "900-12-3456", "9{3}-9{2}-9{4}"},
		{"date", "6/30/2101", "9/9{2}/9{4}"},
		{"apostrophe", "O'Doe, Jane", "A'Aa{2}, Aa{3}"},
		{"curly apostrophe", "O’Doe, Jane", "A’Aa{2}, Aa{3}"},
		{"non-ASCII letters", "Éxample, Pät", "Aa{6}, Aa{2}"},
		{"titlecase digraph", "ǅoe, Jane", "Aa{2}, Aa{3}"},
		{"single characters", "A1b", "A9a"},
		{"ASCII symbols kept", "a+b<c", "a+a<a"},
		{"other characters dotted, tab included", "日本\t語", ".{4}"},
		{"emoji dotted", "a😀😀b", "a.{2}a"},
		{"spaces not encoded", "a   b", "a   a"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Shape(New(tt.in)); got != tt.want {
				t.Errorf("Shape() = %q, want %q", got, tt.want)
			}
		})
	}
	if got := Shape(Value{}); got != "" {
		t.Errorf("Shape(zero) = %q, want empty", got)
	}
}

// TestShapeHidesLetters checks that a silhouette never holds a letter other
// than the class letters A and a, whatever the input.
func TestShapeHidesLetters(t *testing.T) {
	inputs := []string{
		"Doe, Jane", "Roe, Richard", "Example, Pat", "jane.doe@example.com",
		"Éxample, Pät", "ǅoe, ǈane", "Ωmega, Δelta", "日本語の名前", "Doe, Jane Ann Marie",
		"x{2}y", "ⅫⅪ", "ﬁne", "ß", "ŉ", "ᾈ",
	}
	for _, in := range inputs {
		got := Shape(New(in))
		for _, r := range got {
			if unicode.IsLetter(r) && r != 'A' && r != 'a' {
				t.Errorf("silhouette of a %d-byte input holds the letter %q", len(in), r)
			}
		}
		if strings.Contains(in, "Jane") && strings.Contains(got, "Jane") {
			t.Error("silhouette holds the input")
		}
	}
}
