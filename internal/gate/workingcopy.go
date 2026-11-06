package gate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/craigalodon/pbipx/internal/approval"
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
		d := registryDeny(err)
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
	if len(roots) == 0 && len(entries) == 0 {
		return nil
	}
	reach := deny(ReasonWorkingCopy, "the call could reach a Power BI Desktop working copy, "+whyWorkingCopy+hintWorkingCopy)
	spots, unknown := f.spots()
	// The backstop for a copy the registry does not find (its folder was
	// moved: the entry is stale and skipped): while any entry exists, a path
	// that reads a file called cache.abf in a folder called .pbi is denied
	// by name, in any repository or none.
	namedCache := func() *Decision {
		for _, sp := range spots {
			if sp.reads && !sp.prose && namesCache(sp.path, f.fold) {
				d := deny(ReasonWorkingCopy, "the call reads a Power BI Desktop data cache (.pbi/cache.abf), "+whyWorkingCopy+hintWorkingCopy)
				return &d
			}
		}
		return nil
	}
	if len(roots) == 0 {
		return namedCache()
	}
	for _, sp := range spots {
		if sp.prose {
			continue
		}
		if sp.all {
			return &reach
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
	if d := namedCache(); d != nil {
		return d
	}
	if unknown {
		return &reach
	}
	// A line that spells a working copy's path anywhere, where the words
	// above did not reach it (a script handed to an interpreter, a string
	// built into a command), is taken to reach it.
	if f.ev.Tool == Shell || f.ev.Tool == PowerShell {
		for _, root := range roots {
			for _, form := range pathForms(root, windowsPaths, shortPathName) {
				if namesPath(f.ev.Command, form, f.fold) {
					return &reach
				}
			}
		}
	}
	return nil
}

// namesCache reports whether the absolute path p ends in .pbi/cache.abf: a
// file of that name in a folder of that name, in the case it is written in
// unless fold.
func namesCache(p string, fold bool) bool {
	p = filepath.ToSlash(p)
	if fold {
		p = strings.ToLower(p)
	}
	return strings.HasSuffix(p, "/.pbi/cache.abf")
}

// registryDeny is the deny for a registry that cannot be read. The message
// is fixed; the only thing it takes from the error is the name of an entry
// file, which is a hash of a path and names nothing.
func registryDeny(err error) Decision {
	const fix = "Ask a person to remove the entry file from the stages folder of the pbipx configuration directory " +
		"(`pbipx collect --discard` will do this once it exists)."
	var ee *approval.EntryError
	switch {
	case errors.As(err, &ee):
		return deny(ReasonInternalError, "the registry of Power BI Desktop working copies has an entry that cannot be used (the file %s); "+
			"every call is denied until it is fixed: remove that file. %s", displayPath(ee.Name), fix)
	case errors.Is(err, approval.ErrTooManyWorkingCopies):
		return deny(ReasonInternalError, "more than %d Power BI Desktop working copies are registered, more than the gate reads; "+
			"every call is denied until some are removed: remove entry files. %s", approval.MaxWorkingCopiesRead, fix)
	}
	return deny(ReasonInternalError, "the registry of Power BI Desktop working copies cannot be read; every call is denied "+
		"until the stages folder of the pbipx configuration directory can be read again.")
}

// attachedValue is the value attached to a short option in a word
// (-C/dir, -I/dir/x, -o/file), which the finder does not place.
func attachedValue(xw xword) (string, bool) {
	t := xw.text
	if xw.redirect || xw.message || !xw.known || xw.glob || len(t) < 3 || t[0] != '-' || t[1] == '-' || t[1] == '=' {
		return "", false
	}
	return t[2:], true
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

// pathForms are the ways a line can spell the absolute path root: as the
// operating system writes it, and on Windows with forward slashes
// ("C:/Users/x") and as bash writes a drive path ("/c/Users/x").
//
// On Windows it also yields the 8.3 short spellings a temporary folder is
// handed out with (RUNNER~1): for each folder above root (and root itself),
// short names it by its short name, joined to the rest of root as
// registered. A name short cannot give is left long, so a failure leaves
// the long forms. short is nil for none.
func pathForms(root string, windows bool, short func(string) (string, bool)) []string {
	if !windows {
		return []string{root}
	}
	natives := []string{root}
	if short != nil {
		for d := root; ; {
			i := strings.LastIndexByte(d, '\\')
			if i <= 2 { // the drive and its root
				break
			}
			if s, ok := short(d); ok && s != d {
				if v := s + root[len(d):]; !slices.Contains(natives, v) {
					natives = append(natives, v)
				}
			}
			d = d[:i]
		}
	}
	var forms []string
	for _, n := range natives {
		slashed := strings.ReplaceAll(n, `\`, "/")
		forms = append(forms, n, slashed)
		if len(slashed) >= 2 && slashed[1] == ':' && isASCIILetter(slashed[0]) {
			forms = append(forms, "/"+strings.ToLower(slashed[:1])+slashed[2:])
		}
	}
	return forms
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
