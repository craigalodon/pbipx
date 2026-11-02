package gate

import "strings"

// printed is the text echo or printf, given args, prints, as a shell
// reading it from a pipe sees it. echo joins its words with spaces and
// ends the line, its escapes read with -e; printf repeats its format over
// its arguments, with %s and %b (whose argument's escapes are read), %%
// and the format's own escapes. ok is false for anything else (another
// conversion, printf -v, which prints nothing to the pipe): the text
// cannot be told.
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
			text = unescape(text)
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
				b.WriteString(unescape(format[i : i+2]))
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
					b.WriteString(unescape(arg))
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

// unescape reads the backslash escapes echo -e and printf's format read:
// \n, \t, \\ and the like; any other is kept as written.
func unescape(s string) string {
	r := strings.NewReplacer(`\\`, `\`, `\n`, "\n", `\t`, "\t", `\r`, "\r", `\a`, "\a", `\b`, "\b",
		`\f`, "\f", `\v`, "\v", `\'`, "'", `\"`, `"`)
	return r.Replace(s)
}
