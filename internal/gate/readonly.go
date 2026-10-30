package gate

import (
	"slices"
	"strings"
)

// A shell command that names a file controlling the gate (a Claude Code
// settings file, the approval store) is denied only when it could write
// that file (behavior.md, "The gate", Bash). Reading one is harmless: it
// holds configuration, not data, and denying a read of the settings only
// teaches people to turn the gate off.

// mayWrite reports whether v could write the file its word xw names: xw is
// a redirection's target (an input redirection is not told apart from an
// output one, so every one counts), an option (--output=FILE: only an
// operand of a reading program is read), or a word that is not one of the
// program's own arguments (an assignment, a wrapper's option such as
// time -o), or v's program is not one that only reads (readOnly).
func mayWrite(v view, xw xword) bool {
	if xw.redirect || strings.HasPrefix(xw.text, "-") || !readOnly(v) {
		return true
	}
	// The words before the program's arguments: the program's name, and
	// any wrapper and its options before it.
	var args []xword
	for _, w := range v.words {
		if !w.redirect {
			args = append(args, w)
		}
	}
	if len(args) < len(v.rest) {
		return true
	}
	return slices.Contains(args[:len(args)-len(v.rest)], xw)
}

// readOnlyWrappers run the command after them without changing what it
// writes. env (assignments), sudo and doas (sudo -e edits a file), time
// (time -o writes one) and exec -a are not among them.
var readOnlyWrappers = map[string]bool{
	"command": true, "builtin": true, "nohup": true, "nice": true, "stdbuf": true,
	"ionice": true, "setsid": true, "timeout": true, "xargs": true,
}

// readOnlyPrograms are programs that cannot write a file they name, given
// arguments the function accepts. awk is not one: its program can write
// to the file it reads (print > FILENAME), and the program is text the
// gate does not read.
var readOnlyPrograms = map[string]func(args []string) bool{
	"cat": anyArgs, "head": anyArgs, "tail": anyArgs, "more": anyArgs, "wc": anyArgs,
	"diff": anyArgs, "cmp": anyArgs, "stat": anyArgs, "ls": anyArgs,
	"grep": anyArgs, "egrep": anyArgs, "fgrep": anyArgs,
	// less -o and -O copy the input to a log file; +cmd runs commands,
	// such as s, which saves it.
	"less": func(args []string) bool {
		return !slices.ContainsFunc(args, func(a string) bool {
			return strings.HasPrefix(a, "+") || strings.HasPrefix(strings.ToLower(a), "--log-file") ||
				shortOption(a, "oO")
		})
	},
	// rg --pre runs a command on every file.
	"rg": func(args []string) bool {
		return !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--pre") })
	},
	"jq": func(args []string) bool {
		return !slices.ContainsFunc(args, func(a string) bool {
			return a == "--in-place" || shortOption(a, "i")
		})
	},
	// bat --pager runs a command.
	"bat": func(args []string) bool {
		return !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--pager") })
	},
	// file -C compiles a magic file, written beside it.
	"file": func(args []string) bool {
		return !slices.ContainsFunc(args, func(a string) bool { return a == "--compile" || shortOption(a, "C") })
	},
	"sed": sedReads,
}

func anyArgs([]string) bool { return true }

// readOnly reports whether v runs a program that only reads (with
// arguments it accepts), with no environment assignment, under no wrapper
// but readOnlyWrappers.
func readOnly(v view) bool {
	ok, known := readOnlyPrograms[v.name]
	if !known || slices.Contains(v.restKnown, false) {
		return false
	}
	for _, u := range v.under {
		if !readOnlyWrappers[u] {
			return false
		}
	}
	if slices.ContainsFunc(v.argv[:len(v.argv)-len(v.rest)], isAssignment) {
		return false
	}
	return ok(v.rest)
}

// shortOption reports whether a is a cluster of short options (one "-",
// then letters) holding one of letters.
func shortOption(a, letters string) bool {
	return len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a[1:], letters)
}

// sedReads reports whether sed with args only prints: no -i (in place), no
// -f (a script the gate does not read), and every script made only of
// commands that cannot write or run anything (sedScriptReads). A script is
// the value of each -e, or the first operand when there is none.
func sedReads(args []string) bool {
	var scripts []string
	explicit := false
	operand := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if operand == "" && i+1 < len(args) {
				operand = args[i+1]
			}
			i = len(args)
		case strings.HasPrefix(a, "--in-place") || strings.HasPrefix(a, "--file"):
			return false
		case a == "--expression" || a == "-e":
			if i+1 >= len(args) {
				return false
			}
			scripts = append(scripts, args[i+1])
			explicit = true
			i++
		case strings.HasPrefix(a, "--expression="):
			scripts = append(scripts, strings.TrimPrefix(a, "--expression="))
			explicit = true
		case strings.HasPrefix(a, "--"):
			// --quiet, --regexp-extended, --separate, --null-data and the
			// like; --line-length takes its value after "=".
		case shortOption(a, "if"):
			return false
		case len(a) > 1 && a[0] == '-':
			// A cluster such as -nE, or -e joined to its script (-es/a/b/).
			if j := strings.IndexByte(a, 'e'); j > 0 {
				switch {
				case j+1 < len(a):
					scripts = append(scripts, a[j+1:])
				case i+1 < len(args):
					scripts = append(scripts, args[i+1])
					i++
				default:
					return false
				}
				explicit = true
			}
			if j := strings.IndexByte(a, 'l'); j > 0 && j+1 == len(a) {
				i++ // -l N
			}
		default:
			if operand == "" {
				operand = a
			}
		}
	}
	if !explicit {
		if operand == "" {
			return false
		}
		scripts = append(scripts, operand)
	}
	for _, s := range scripts {
		if !sedScriptReads(s) {
			return false
		}
	}
	return true
}

// sedScriptReads reports whether a sed script is made only of commands
// that print, delete, quit or move text: each optionally after an address
// (a line number, $, or /regex/ and \cregexc, a range of them, and !), one
// of p P l = d D n N q Q g G h H x z, braces, or s///, y/// with flags among
// g p i I m M and digits. w, W, r, R, e, a, i, c, s///w and s///e are
// refused: they write, read in or run something, or need parsing this
// does not do.
func sedScriptReads(script string) bool {
	rs := []rune(script)
	i := 0
	skipBlank := func() {
		for i < len(rs) && strings.ContainsRune(" \t\n;", rs[i]) {
			i++
		}
	}
	// delimited skips a part closed by d, honouring backslash escapes.
	delimited := func(d rune) bool {
		for ; i < len(rs); i++ {
			switch rs[i] {
			case '\\':
				i++
			case d:
				i++
				return true
			case '\n':
				return false
			}
		}
		return false
	}
	address := func() bool {
		switch {
		case i < len(rs) && (rs[i] == '$' || (rs[i] >= '0' && rs[i] <= '9')):
			for i < len(rs) && (rs[i] == '$' || rs[i] == '~' || (rs[i] >= '0' && rs[i] <= '9')) {
				i++
			}
		case i < len(rs) && rs[i] == '/':
			i++
			if !delimited('/') {
				return false
			}
			for i < len(rs) && (rs[i] == 'I' || rs[i] == 'M') {
				i++
			}
		case i+1 < len(rs) && rs[i] == '\\':
			d := rs[i+1]
			i += 2
			if !delimited(d) {
				return false
			}
		}
		return true
	}
	for {
		skipBlank()
		if i >= len(rs) {
			return true
		}
		if !address() {
			return false
		}
		if i < len(rs) && rs[i] == ',' {
			i++
			if i < len(rs) && rs[i] == '+' {
				i++
			}
			if !address() {
				return false
			}
		}
		for i < len(rs) && (rs[i] == ' ' || rs[i] == '!') {
			i++
		}
		if i >= len(rs) {
			return false
		}
		c := rs[i]
		i++
		switch {
		case c == '{':
			continue
		case strings.ContainsRune("pPl=dDnNqQgGhHxz}", c):
			// A q or Q may take an exit code.
			for i < len(rs) && rs[i] >= '0' && rs[i] <= '9' {
				i++
			}
		case c == 's' || c == 'y':
			if i >= len(rs) || rs[i] == '\\' || rs[i] == '\n' {
				return false
			}
			d := rs[i]
			i++
			// The pattern, then the replacement, each closed by d.
			for range 2 {
				if !delimited(d) {
					return false
				}
			}
			if c == 's' {
				for i < len(rs) && strings.ContainsRune("gpiImM0123456789", rs[i]) {
					i++
				}
			}
		default:
			return false
		}
		for i < len(rs) && rs[i] == ' ' {
			i++
		}
		if i < len(rs) && !strings.ContainsRune(";\n}", rs[i]) {
			return false
		}
	}
}
