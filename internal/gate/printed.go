package gate

import "strings"

// printed is the text echo or printf, given args, prints, as a shell
// reading it from a pipe sees it. echo joins its words with spaces and
// ends the line, its escapes read with -e; printf repeats its format over
// its arguments, with %s and %b (whose argument's escapes are read), %%
// and the format's own escapes. ok is false for anything the gate cannot
// read exactly (another conversion, printf -v which prints nothing, or an
// escape outside its tables): the text cannot be told, and the caller
// takes the script as built at run time.
func printed(name string, args []string) (string, bool) {
	if name == "echo" {
		escapes, newline := false, true
		for len(args) > 0 && len(args[0]) > 1 && args[0][0] == '-' && strings.Trim(args[0][1:], "neE") == "" {
			for _, c := range args[0][1:] {
				switch c {
				case 'n':
					newline = false
				case 'e':
					escapes = true
				case 'E':
					escapes = false
				}
			}
			args = args[1:]
		}
		text := strings.Join(args, " ")
		if escapes {
			t, ok := unescape(text, ebEscapes)
			if !ok {
				return "", false
			}
			text = t
		}
		if newline {
			text += "\n"
		}
		return text, true
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", false
	}
	format, rest := args[0], args[1:]
	var b strings.Builder
	for {
		used := 0
		for i := 0; i < len(format); i++ {
			c := format[i]
			switch {
			case c == '\\' && i+1 < len(format):
				rep, ok := fmtEscapes[format[i:i+2]]
				if !ok {
					return "", false
				}
				b.WriteString(rep)
				i++
			case c == '%' && i+1 < len(format):
				i++
				arg := ""
				if format[i] != '%' {
					if used < len(rest) {
						arg = rest[used]
					}
					used++
				}
				switch format[i] {
				case '%':
					b.WriteByte('%')
				case 's':
					b.WriteString(arg)
				case 'b':
					t, ok := unescape(arg, ebEscapes)
					if !ok {
						return "", false
					}
					b.WriteString(t)
				default:
					return "", false
				}
			default:
				b.WriteByte(c)
			}
		}
		if used == 0 || used >= len(rest) {
			break
		}
		rest = rest[used:]
	}
	return b.String(), true
}

// fmtEscapes are the backslash escapes printf's format string reads, as
// bash's printf does: the simple letter escapes, and \" and \', which the
// format reads (an echo -e or %b argument keeps those two as written).
var fmtEscapes = map[string]string{
	`\\`: `\`, `\a`: "\a", `\b`: "\b", `\f`: "\f", `\n`: "\n", `\r`: "\r", `\t`: "\t", `\v`: "\v",
	`\"`: `"`, `\'`: "'",
}

// ebEscapes are the backslash escapes echo -e and a printf %b argument
// read as a single letter. \x, \0NNN, \u, \U, \c and \e are not here: the
// gate does not decode them, so a script that uses one is taken to be
// built at run time rather than read wrong.
var ebEscapes = map[string]string{
	`\\`: `\`, `\a`: "\a", `\b`: "\b", `\f`: "\f", `\n`: "\n", `\r`: "\r", `\t`: "\t", `\v`: "\v",
}

// unescape reads s with the escape table esc. ok is false when a
// backslash starts an escape the table does not hold exactly: the text
// cannot be told as bash reads it.
func unescape(s string, esc map[string]string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		rep, ok := esc[s[i:i+2]]
		if !ok {
			return "", false
		}
		b.WriteString(rep)
		i++
	}
	return b.String(), true
}
