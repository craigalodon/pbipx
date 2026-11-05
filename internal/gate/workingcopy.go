package gate

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// Working copies (ADR-0007; behavior.md, "The gate", working copies).
//
// A working copy is a folder `pbipx stage` made outside every repository for
// Power BI Desktop, which writes a data cache of real data into it. The
// copies are registered in the approval store (approval.WorkingCopies), and
// the gate denies every call that touches a path under one, from any
// working directory, adopted repository or not, with no scan: the cache is
// opaque, and a scan would pass it (principle 8).
//
// What counts as touching is what the adoption finder counts as touching an
// adopted repository (spots, in adopt.go): the paths a call names, placed
// as the finder places them (symlinks, .., ~, $HOME, globs, case, Git Bash
// forms), the working directory, and a directory above a working copy that
// a search or a reading command goes under. A call that touches something
// the finder cannot place touches every working copy. A listing is not
// excepted: ls of a working copy denies like cat.

// hintWorkingCopy ends the refusal of a working copy.
const hintWorkingCopy = "Leave it out of the call: a person works there in Power BI Desktop."

// whyWorkingCopy says what a working copy holds.
const whyWorkingCopy = "which holds a copy of real data that cannot be vetted. "

// workingCopyDeny is the deny for a call that touches a registered working
// copy, nil when it touches none. A registry that cannot be read denies
// every call, as a store that cannot be read does; an entry whose folder is
// gone is skipped.
func (f *finder) workingCopyDeny() *Decision {
	if f.store == nil {
		return nil
	}
	entries, err := f.store.WorkingCopies()
	if err != nil {
		d := deny(ReasonInternalError, "the registry of Power BI Desktop working copies cannot be read; the call is denied")
		return &d
	}
	var roots []string
	for _, w := range entries {
		if _, err := os.Lstat(w.Path); errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			continue
		}
		for _, p := range []string{w.Path, resolve(w.Path)} {
			if !containsPath(roots, p, f.fold) {
				roots = append(roots, p)
			}
		}
	}
	if len(roots) == 0 {
		return nil
	}
	reach := deny(ReasonWorkingCopy, "the call could reach a Power BI Desktop working copy, "+whyWorkingCopy+hintWorkingCopy)
	spots, unknown := f.spots()
	for _, sp := range spots {
		if sp.prose {
			continue
		}
		for _, a := range []string{sp.path, resolve(sp.path)} {
			for _, root := range roots {
				if rel, in := relTo(root, a, f.fold); in {
					return inWorkingCopy(rel)
				}
				if _, holds := relTo(a, root, f.fold); holds && sp.container && !sp.bare {
					return &reach
				}
			}
		}
	}
	if unknown {
		return &reach
	}
	// A line that spells a working copy's path anywhere, where the words
	// above did not reach it (a script handed to an interpreter, a string
	// built into a command), is taken to reach it.
	if f.ev.Tool == Shell || f.ev.Tool == PowerShell {
		for _, root := range roots {
			if namesPath(f.ev.Command, root, f.fold) {
				return &reach
			}
		}
	}
	return nil
}

// inWorkingCopy is the deny for a path rel below a working copy ("." for
// the folder itself). It prints the path inside the working copy, each
// segment as a finding prints a name (displayPath), and never where the
// working copy is, nor anything it holds.
func inWorkingCopy(rel string) *Decision {
	var d Decision
	if rel == "." {
		d = deny(ReasonWorkingCopy, "<working copy> is a Power BI Desktop working copy, "+whyWorkingCopy+hintWorkingCopy)
	} else {
		d = deny(ReasonWorkingCopy, "<working copy>/%s is in a Power BI Desktop working copy, "+whyWorkingCopy+hintWorkingCopy, displayPath(rel))
	}
	return &d
}

// namesPath reports whether line spells the path name as a whole word
// (namesWord's boundaries), in the case it is written in unless fold.
func namesPath(line, name string, fold bool) bool {
	if fold {
		return namesWord(line, name)
	}
	for i := 0; ; {
		j := strings.Index(line[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		if (start == 0 || !wordByte(line[start-1])) && (end == len(line) || !wordByte(line[end])) {
			return true
		}
		i = start + 1
	}
}

// containsPath reports whether paths holds p, as spelled or, with fold, in
// another case.
func containsPath(paths []string, p string, fold bool) bool {
	for _, q := range paths {
		if samePath(q, p, fold) {
			return true
		}
	}
	return false
}
