package gate

import (
	"slices"
	"strings"
)

// ghFileOptions are the gh options whose value names a file gh reads: the
// body of an issue or a pull request, a release's notes, the input of gh
// api, and its fields (-F key=@file reads one).
var ghFileOptions = []string{"-F", "--field", "-f", "--raw-field", "--body-file", "--input", "--notes-file", "--file"}

// ghFileValue reports whether word wi of v, a gh command, is the value of
// an option that names a file gh reads (ghFileOptions), given apart or
// attached. gh prints none of its other operands, so a title or a body it
// is given as a string (--body "$(cat <<'EOF' ...)") is no file.
func ghFileValue(v view, wi int) bool {
	prev := ""
	for i, w := range v.words {
		if w.redirect {
			continue
		}
		if i == wi {
			break
		}
		prev = w.text
	}
	if slices.Contains(ghFileOptions, prev) {
		return true
	}
	text := v.words[wi].text
	for _, o := range ghFileOptions {
		if strings.HasPrefix(text, o+"=") || len(o) == 2 && len(text) > 2 && strings.HasPrefix(text, o) && !strings.HasPrefix(text, "--") {
			return true
		}
	}
	return false
}
