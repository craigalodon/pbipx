package gate

import (
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/scan"
)

// interpreterNamesSettings reports whether the line runs an interpreter
// and any line read (the command line, its nested lines, the here-documents
// in them) names a Claude Code settings file: a word of the code text that
// is .claude/settings.json or .claude/settings.local.json at the end of a
// path, in either slash and any case. The path is read as written; a name
// built at run time (d=.claude; ... $d/settings.json) is not seen.
func (w *walker) interpreterNamesSettings() bool {
	if !slices.ContainsFunc(w.views, func(v view) bool { return isInterpreter(v.name) }) {
		return false
	}
	return slices.ContainsFunc(w.lines, textNamesSettings)
}

// textNamesSettings reports whether a word of text, split at blanks,
// quotes and punctuation, is a settings file's path.
func textNamesSettings(text string) bool {
	text = strings.ReplaceAll(text, `\`, "/")
	words := strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune(" \t\r\n'\"`;,()[]{}=<>|&+", r) })
	return slices.ContainsFunc(words, scan.IsAgentSettings)
}

// neverReadsOperands are the programs known not to read their operands as
// files, so that an operand the gate cannot tell (an expansion, a
// runner's placeholder) is no read of a protected file: they print their
// own arguments, make, move, link, remove or change files without reading
// their content, only list, test or stat them, change the shell's own
// state, or are judged by a rule of their own (git, gh, hub, pbipx, the
// shells). Any other program given such a word is a reader of it: a
// content printer outside the readers table (shuf, expand, ptx, nl, od,
// fmt, python3 -m json.tool, perl -pe, node -p, curl -d @file) reads it
// as well as cat does. A tree scan decides then; a clean tree allows.
//
// Only a program that never reads the content of a file an operand names
// belongs here: cp and tar (a copy reads its sources), the readers and
// the searches, and the interpreters do not.
var neverReadsOperands = map[string]bool{
	// Print their own arguments.
	"echo": true, "printf": true, "true": true, "false": true, ":": true,
	// Make, remove, move, link and change files without reading them.
	"mkdir": true, "rmdir": true, "touch": true, "rm": true, "mv": true, "ln": true, "chmod": true,
	"chown": true, "chgrp": true, "mkfifo": true, "unlink": true,
	// Only list, test, name or stat files.
	"ls": true, "dir": true, "vdir": true, "stat": true, "du": true, "df": true, "tree": true, "test": true,
	"[": true, "[[": true, "basename": true, "dirname": true, "realpath": true, "readlink": true, "file": true,
	"which": true, "whereis": true, "type": true,
	// Time, processes and the machine.
	"sleep": true, "kill": true, "pkill": true, "pgrep": true, "killall": true, "ps": true, "date": true,
	"pwd": true, "whoami": true, "id": true, "uname": true, "hostname": true, "uptime": true, "wait": true,
	"jobs": true, "fg": true, "bg": true, "disown": true, "exit": true, "return": true,
	// The shell's own state.
	"cd": true, "pushd": true, "popd": true, "chdir": true, "export": true, "unset": true, "set": true,
	"shopt": true, "declare": true, "typeset": true, "local": true, "readonly": true, "shift": true,
	"read": true, "alias": true, "unalias": true, "hash": true, "trap": true, "complete": true,
	"compgen": true, "getopts": true, "let": true, "mapfile": true, "readarray": true,
	// Judged by a rule of their own.
	"git": true, "gh": true, "hub": true, "pbipx": true,
	// Build, package and language tools: their operands are targets,
	// packages and subcommands.
	"make": true, "go": true, "npm": true, "pnpm": true, "yarn": true, "cargo": true, "rustup": true,
	"pip": true, "pip3": true, "pipx": true, "gem": true, "apt": true, "apt-get": true, "apt-cache": true,
	"brew": true, "dnf": true, "yum": true, "pacman": true, "apk": true, "snap": true, "man": true,
	"info": true, "help": true, "apropos": true, "whatis": true,
}

// readsOperands reports whether program name reads an operand it is
// given as a file, as far as the gate knows: it is not in
// neverReadsOperands, and does not run its operands as commands or
// scripts (shells, eval, source are judged where they run them).
func readsOperands(name string) bool {
	return !neverReadsOperands[name] && !shells[name] && name != "eval" && name != "exec" && name != "builtin" &&
		name != "command"
}

// operandWords are the indexes in v.words of the words after v's program,
// the program's own operands: the options, values and wrappers in front
// of it, assignments and redirections are not.
func operandWords(v view) map[int]bool {
	nargs := 0
	for _, w := range v.words {
		if !w.redirect {
			nargs++
		}
	}
	firstOperand := nargs - len(v.rest)
	out := map[int]bool{}
	k := 0
	for wi, w := range v.words {
		if w.redirect {
			continue
		}
		if k >= firstOperand {
			out[wi] = true
		}
		k++
	}
	return out
}
