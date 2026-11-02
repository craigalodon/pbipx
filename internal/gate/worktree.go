package gate

import (
	"slices"
	"strings"
)

// The working tree written, then printed by git (behavior.md, "The gate",
// rule 2): git's own reads are judged against the tree as it is before
// the line runs, and a command the gate does not know may rewrite files
// under a root without naming them (a formatter, a build, a script). A git
// command after it that prints the working tree's content would print
// what that command wrote.

// whyWorktreeAfterWrite is the trigger for such a line: the tree is then
// scanned as for Grep.
const whyWorktreeAfterWrite = "a command that may write files runs before a git command that prints the working tree, " +
	"which the gate judged as it was before"

// worktreeAfterWrite reports whether a git command in the line that
// prints the working tree's content (printsWorktree) runs after a command
// that may write files (writesNothing false): before it, or anywhere in
// an unordered line.
func (w *walker) worktreeAfterWrite() bool {
	unordered := w.loopy || w.anywhere != ""
	for _, v := range w.views {
		if !printsWorktree(v) {
			continue
		}
		for _, u := range w.views {
			if u.seq != v.seq && (unordered || u.seq < v.seq) && !writesNothing(u) {
				return true
			}
		}
	}
	return false
}

// printsWorktree reports whether v is a git command that can print the
// content of files in the working tree: git diff but --cached or a
// summary alone (--stat, --name-only and the like), with or without a
// revision, status -v -v, add, checkout, reset or restore with
// -p, a dry run of commit with -v and -a, -o, -i or paths, stash show
// (what a stash in the line records), difftool, mergetool, blame,
// annotate, grep but --cached, and any subcommand that is not git's own.
func printsWorktree(v view) bool {
	name, rest := program(v.argv)
	if name == "hub" {
		name = "git"
	}
	if name != "git" {
		return false
	}
	inv, ok := parseGit(rest, "")
	if !ok {
		return false
	}
	if !gitBuiltins[inv.sub] || inv.ambiguous {
		return true
	}
	switch inv.sub {
	case "diff":
		summary := hasFlag(inv.args, gitSummaryFlags) && !hasFlag(inv.args, gitPatchFlags)
		return !summary && !hasFlag(inv.args, []string{"--cached", "--staged"})
	case "status":
		return !shortFormat(inv.args, true) && verbosity(inv.args, "u") >= 2
	case "add", "stage":
		return hasShort(inv.args, 'p', "--patch", "") || hasShort(inv.args, 'i', "--interactive", "") ||
			hasShort(inv.args, 'e', "--edit", "")
	case "checkout", "reset":
		return hasShort(inv.args, 'p', "--patch", "bB")
	case "restore":
		return hasShort(inv.args, 'p', "--patch", "s")
	case "commit":
		dry := hasFlag(inv.args, []string{"--dry-run", "--short", "--porcelain", "--long"})
		verbose := hasShort(inv.args, 'v', "--verbose", "mFCcSt")
		tree := hasShort(inv.args, 'a', "--all", "mFCcSt") || hasShort(inv.args, 'o', "--only", "mFCcSt") ||
			hasShort(inv.args, 'i', "--include", "mFCcSt") || len(inv.specs) > 0
		return dry && verbose && tree
	case "stash":
		return slices.Contains(inv.args, "show")
	case "difftool", "mergetool", "blame", "annotate":
		return true
	case "grep":
		return !hasFlag(inv.args, []string{"--cached"})
	}
	return false
}

// writesNothing reports whether v is known to write no file: an
// assignment alone, git in a read-only form (gitReadOnly), or a program
// that only prints, lists, reads or searches (noWrites), with no output
// redirection but to /dev/null or a descriptor. Any other command may
// write files, as may a function or alias the line defines.
func writesNothing(v view) bool {
	for _, xw := range v.words {
		if xw.redirect && !xw.here && (!xw.input || xw.rw) &&
			!slices.Contains([]string{"/dev/null", "/dev/stdout", "/dev/stderr"}, xw.text) {
			return false
		}
	}
	if v.name == "" {
		return true
	}
	if v.name == "git" {
		_, rest := program(v.argv)
		inv, ok := parseGit(rest, "")
		return ok && gitBuiltins[inv.sub] && !inv.ambiguous && gitReadOnly(inv)
	}
	if !noWrites[v.name] {
		return false
	}
	has := func(opts ...string) bool {
		return slices.ContainsFunc(v.rest, func(a string) bool {
			return slices.ContainsFunc(opts, func(o string) bool { return a == o || strings.HasPrefix(a, o+"=") })
		})
	}
	switch v.name {
	case "find":
		return !has("-exec", "-execdir", "-ok", "-okdir", "-delete", "-fprint", "-fprint0", "-fprintf", "-fls")
	case "fd", "fdfind":
		return !has("-x", "--exec", "-X", "--exec-batch")
	case "sort":
		// -o FILE, alone or in a cluster, writes the output there.
		return !has("--output") && !hasShort(v.rest, 'o', "", "kStT")
	}
	return true
}

// noWrites are programs that write no file of their own: they print,
// list, read or search, or set the shell's state. Editors, sed, awk, tee,
// cp and the like write, and so does every program not listed.
var noWrites = map[string]bool{
	"echo": true, "printf": true, ":": true, "true": true, "false": true, "exit": true, "return": true,
	"sleep": true, "which": true, "whereis": true, "type": true, "man": true, "info": true, "help": true,
	"apropos": true, "whatis": true, "unset": true, "export": true, "declare": true, "typeset": true,
	"local": true, "readonly": true, "set": true, "shopt": true, "test": true, "[": true, "[[": true,
	"complete": true, "compgen": true, "read": true, "wait": true, "jobs": true, "shift": true,
	"pwd": true, "date": true, "basename": true, "dirname": true, "realpath": true, "readlink": true,
	"stat": true, "file": true, "du": true, "df": true, "cd": true, "pushd": true,
	"popd": true, "ls": true, "printenv": true, "id": true, "whoami": true, "uname": true,
	"hostname": true, "ps": true, "pgrep": true, "cat": true, "tac": true, "nl": true, "head": true,
	"tail": true, "less": true, "more": true, "most": true, "bat": true, "batcat": true, "zcat": true,
	"strings": true, "od": true, "hexdump": true, "hd": true, "base64": true,
	"base32": true, "cut": true, "wc": true, "paste": true, "join": true, "comm": true,
	"diff": true, "cmp": true, "grep": true, "egrep": true, "fgrep": true, "zgrep": true, "rg": true,
	"ag": true, "ack": true, "jq": true, "column": true, "fold": true, "fmt": true, "pr": true,
	"find": true, "fd": true, "fdfind": true, "sort": true,
}
