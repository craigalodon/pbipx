package gate

import (
	"slices"
	"strings"
)

// Quiet words (behavior.md, "Working copies").
//
// A word that names a folder ABOVE a working copy denies when the call
// could read under it. A word is quiet, and does not, only when the program
// and the position of the word are proven to read nothing under it.
// The rule is a short allowlist, and anything the gate does not fully
// understand is not quiet: an unknown program, an option it cannot parse,
// a word it cannot tell. Words at or under a copy deny whatever the program.

// wcQuietPrograms are the programs whose operands are all quiet: they print
// their own arguments, test or name a path, change the shell's own state, or
// make, remove or change the mode of a file without opening anything below
// it. It is deliberately smaller than neverReadsOperands, which answers a
// different question (is this operand read as a file, for an operand the
// gate cannot tell): mv and ln are not here, for their source is a move or
// a link of the folder itself, and git, hub and pbipx are decided by
// subcommand below. Listers (ls, du, stat, tree, find) are quiet as
// listings, not here (lister).
var wcQuietPrograms = map[string]bool{
	// Print their own arguments.
	"echo": true, "printf": true, "true": true, "false": true, ":": true,
	// Test or name a path.
	"test": true, "[": true, "[[": true, "basename": true, "dirname": true, "realpath": true,
	"readlink": true, "which": true, "whereis": true, "type": true,
	// The shell's own state.
	"cd": true, "pushd": true, "popd": true, "chdir": true, "export": true, "unset": true, "set": true,
	"declare": true, "typeset": true, "local": true, "readonly": true, "alias": true, "unalias": true,
	// Make, remove or change files without opening what is below.
	"mkdir": true, "rmdir": true, "touch": true, "rm": true, "chmod": true, "chown": true, "chgrp": true,
	"mkfifo": true, "unlink": true,
	// Metadata only (the documented listings).
	"ls": true, "dir": true, "vdir": true, "stat": true, "du": true, "df": true, "tree": true,
}

// openers are programs that open a path in a window or a viewer: they name
// a folder and a person looks at it, nothing is printed to the agent.
// Only the form with no option is quiet (openerQuiet).
var openers = map[string]bool{"code": true, "cursor": true, "open": true, "explorer": true, "explorer.exe": true}

// quietWords are the indexes in v.words of words that name a place and
// read nothing under it, so a working copy below one is not reached.
func quietWords(s *setup, v view) map[int]bool {
	q := map[int]bool{}
	var args []int // indexes of the words that are not redirections
	for i, xw := range v.words {
		if !xw.redirect {
			args = append(args, i)
		}
	}
	if len(v.rest) > len(args) {
		return q
	}
	rest := args[len(args)-len(v.rest):]
	allRest := func() {
		for _, i := range rest {
			q[i] = true
		}
	}
	switch {
	case wcQuietPrograms[v.name]:
		allRest()
		return q
	case v.name == "git" || v.name == "hub":
		if gitQuiet(v) {
			allRest()
		}
		return q
	case v.name == "pbipx":
		if pbipxQuiet(v) {
			allRest()
		}
		return q
	case openers[v.name]:
		if openerQuiet(v) {
			allRest()
		}
		return q
	}
	// A destination, by its position among the words and never by its text:
	// the words of cp, mv, ln, install, rsync and tee are read with their
	// options (copyArgs), and a command with one the gate does not fully
	// understand has no quiet word. A source is never quiet: mv and ln move
	// or link the folder itself, cp and rsync read it.
	// None when a runner feeds the program or find -exec runs it: its words
	// are not all in the line.
	if _, copier := copyOptSpecs[v.name]; copier && !v.execd && feedOf(v.argv, v.argvKnown).runner == "" {
		dests, _, ok := copyArgs(v.name, s.restWords(v))
		if !ok {
			return q
		}
		for _, d := range dests {
			q[rest[d.idx]] = true
		}
	}
	for i, xw := range v.words {
		if xw.redirect && !xw.here && (!xw.input || xw.rw) {
			// An output redirection, or <>.
			q[i] = true
		}
	}
	var flags []string // options whose next word is a directory to work in
	var prefixes []string
	switch v.name {
	case "tar":
		// The directory tar works in is quiet only when tar is proven to
		// extract (tarArgs): any other operation, or an option not
		// understood, reads it.
		if extract, dirs, ok := tarArgs(s.restWords(v)); ok && extract {
			for _, d := range dirs {
				q[rest[d]] = true
			}
		}
	case "unzip":
		flags = []string{"-d"}
	case "wget":
		flags, prefixes = []string{"-P", "--directory-prefix"}, []string{"--directory-prefix="}
	}
	for j, a := range v.rest {
		if slices.Contains(flags, a) && j+1 < len(rest) {
			q[rest[j+1]] = true
		}
		for _, pre := range prefixes {
			if strings.HasPrefix(a, pre) {
				q[rest[j]] = true
			}
		}
	}
	return q
}

// openerQuiet reports whether v, an opener, is given operands only, every
// one known: code --diff, --goto, --merge and - (a stdin), open -a, -f and
// -t, and the options that take a file's content as an argument can put
// content before the agent or run something. An opened editor's search is
// not the agent's, but a form with an option is not proven, so code with
// one denies above a copy; that is a small cost.
func openerQuiet(v view) bool {
	if !allKnown(v.restKnown, len(v.rest)) {
		return false
	}
	return !slices.ContainsFunc(v.rest, func(a string) bool { return strings.HasPrefix(a, "-") })
}

// pbipxQuiet reports whether v, a pbipx command, is a subcommand that reads
// no path it is given: version and help. Every other subcommand (scan,
// scan-text, explain, init, policy, baseline) reads a folder or the
// repository around the working directory, so the words of one above a
// copy, and the working directory itself, are not quiet.
func pbipxQuiet(v view) bool {
	if len(v.rest) == 0 || !allKnown(v.restKnown, len(v.rest)) {
		return false
	}
	return v.rest[0] == "version" || v.rest[0] == "help"
}

func allKnown(known []bool, n int) bool {
	return len(known) == n && !slices.Contains(known, false)
}

// gitQuiet reports whether v, a git or hub command, is known to open no
// file below the folders it names or runs in: a read-only subcommand
// from the allowlist, with every word known and no option that points git
// elsewhere, runs a command or turns on a patch.
func gitQuiet(v view) bool {
	if !v.cwd.known() || !allKnown(v.restKnown, len(v.rest)) {
		return false
	}
	inv, ok := parseGit(v.rest, "")
	if !ok || inv.unsafe || inv.ambiguous || inv.dashHidden || inv.elsewhere || !gitBuiltins[inv.sub] {
		return false
	}
	return gitQuietSub(inv)
}

// gitPatchLong are the long options (and, by prefix, their abbreviations)
// that make log or show print a patch, or read files beyond the repository.
var gitPatchLong = []string{
	"--patch", "--patch-with-stat", "--patch-with-raw", "--no-index", "--output", "--cc", "--combined",
	"--diff-merges", "--full-diff", "--textconv", "--ext-diff", "--pretty-patch",
}

// gitNoPatchShow are the options that stop git show from printing a patch.
var gitNoPatchShow = []string{
	"--no-patch", "--stat", "--name-only", "--name-status", "--shortstat", "--numstat", "--summary", "--raw",
	"--quiet", "--dirstat",
}

// gitQuietSub is the allowlist: the subcommands that only read refs and the
// object store, or the status of the work tree without its content.
func gitQuietSub(inv gitInvocation) bool {
	switch inv.sub {
	case "status":
		return verbosity(inv.args, "") == 0
	case "log", "show":
		for _, a := range inv.args {
			if name, _, _ := strings.Cut(a, "="); strings.HasPrefix(a, "--") && len(name) > 2 {
				for _, f := range gitPatchLong {
					if strings.HasPrefix(f, name) {
						return false
					}
				}
			}
			if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a[1:], "pucLSGO") {
				// A patch (-p, -u, -c, -L), a pickaxe or an order file,
				// or a value with such a letter: unproven.
				return false
			}
		}
		if inv.sub == "show" {
			return slices.ContainsFunc(inv.args, func(a string) bool {
				if a == "-s" {
					return true
				}
				name, _, _ := strings.Cut(a, "=")
				return strings.HasPrefix(a, "--") && slices.Contains(gitNoPatchShow, name)
			})
		}
		return true
	case "branch", "tag", "remote", "symbolic-ref", "reflog", "worktree":
		return gitReadOnly(inv)
	case "rev-parse", "rev-list", "describe", "for-each-ref", "show-ref", "merge-base", "name-rev",
		"count-objects", "var", "version", "check-ref-format":
		return !hasFlag(inv.args, []string{"--output", "--no-index"})
	}
	return false
}

// gitSpots are the extra spots a git or hub command that is not quiet
// touches, for working copies: its working directory, the directory -C and
// the like move it to, and every word placed against that directory too
// (git places a relative path against it, the finder against the working
// directory of the call), with the value of each --option=value.
func gitSpots(s *setup, v view) []spot {
	var out []spot
	add := func(paths []string) {
		for _, a := range paths {
			out = append(out, spot{path: a, container: true, only: true})
		}
	}
	var args []int
	for i, xw := range v.words {
		if !xw.redirect {
			args = append(args, i)
		}
	}
	if len(v.rest) > len(args) {
		return nil
	}
	rest := args[len(args)-len(v.rest):]
	for _, d := range v.cwd.dirs {
		inv, _ := parseGit(v.rest, d)
		dirs := []string{d}
		if inv.dir != d {
			dirs = append(dirs, inv.dir)
		}
		for _, dir := range dirs {
			add([]string{dir})
			for k, w := range v.rest {
				if v.words[rest[k]].message {
					continue
				}
				if val, ok := strings.CutPrefix(w, "--"); ok {
					if _, v2, has := strings.Cut(val, "="); has {
						w = v2
					} else {
						continue
					}
				} else if strings.HasPrefix(w, "-") {
					continue
				}
				add(s.spellingsAt(dir, w))
			}
		}
	}
	return out
}
