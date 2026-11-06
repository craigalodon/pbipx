package gate

import "testing"

// TestAnsiCNumericEscapes pins how $'...' decodes numeric escapes, as bash does:
// \x takes two hex digits, three octal digits keep their low byte, and \u and
// \U past Unicode's range cannot spell a path.
func TestAnsiCNumericEscapes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`\x41'`, "A"},
		{`\xff'`, "\xff"},
		{`\x4142'`, "A42"},
		{`\101'`, "A"},
		{`\377'`, "\xff"},
		{`\777'`, "\xff"},
		{`\501'`, "A"},
		{`A'`, "A"},
		{`\U00000041'`, "A"},
		{`\U0010FFFF'`, "\U0010FFFF"},
		{`\U00110000'`, "�"},
		{`\UFFFFFFFF'`, "�"},
		{`\x'`, `\x`},
	} {
		got, _, ok := ansiC([]rune(tc.in), 0)
		if !ok || got != tc.want {
			t.Errorf("ansiC(%q) = %q, %v; want %q", tc.in, got, ok, tc.want)
		}
	}
}
