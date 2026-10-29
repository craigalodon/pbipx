// Package setup holds what `pbipx init` writes and changes: the PBIP layout
// it detects, the starting policy it writes, the Claude Code settings it
// merges the gate's hook into, and the .gitignore lines it checks.
//
// Nothing here reads a report or model file. Detection lists directory and
// file names only, which are schema (principle 1).
package setup

import (
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PBIP names: a report folder, a semantic-model folder and a project file.
const (
	reportSuffix = ".Report"
	modelSuffix  = ".SemanticModel"
	pbipSuffix   = ".pbip"
)

// Layout is the PBIP layout of a repository.
type Layout struct {
	// Roots are the protected roots: repository-relative directories, with
	// forward slashes, that hold PBIP folders or project files. A PBIP folder
	// at the repository root is a root itself. None is nested in another.
	Roots []string
	// TopLevelPBIP is set when a .pbip file sits at the repository root,
	// where no protected root can hold it; the policy scans it as a report.
	TopLevelPBIP bool
	// Skipped counts the directories whose names cannot be written as a
	// protected root: glob or control characters, a quote, a backslash or
	// invalid UTF-8.
	Skipped int
}

// Detect finds the PBIP layout under root, the repository's work tree. It
// skips hidden directories (.git, Desktop's .pbi and the like) and does not
// descend into PBIP folders: their parent is all it needs.
func Detect(root string) (Layout, error) {
	found := map[string]bool{}
	var l Layout
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := d.Name()
		parent := path.Dir(rel)
		switch {
		case d.IsDir() && strings.HasPrefix(name, "."):
			return fs.SkipDir
		case d.IsDir() && (strings.HasSuffix(name, reportSuffix) || strings.HasSuffix(name, modelSuffix)):
			if parent == "." {
				found[rel] = true
			} else {
				found[parent] = true
			}
			return fs.SkipDir
		case d.Type().IsRegular() && strings.HasSuffix(name, pbipSuffix):
			if parent == "." {
				l.TopLevelPBIP = true
			} else {
				found[parent] = true
			}
		}
		return nil
	})
	if err != nil {
		return Layout{}, err
	}
	var roots []string
	for r := range found {
		if writable(r) {
			roots = append(roots, r)
		} else {
			l.Skipped++
		}
	}
	slices.Sort(roots)
	for _, r := range roots {
		if !nestedIn(r, l.Roots) {
			l.Roots = append(l.Roots, r)
		}
	}
	return l, nil
}

// writable reports whether a directory path can be written into the policy
// as a protected root and as the plain prefix of a glob: valid UTF-8, no
// control character, and none of the characters a glob, a TOML basic string
// or the policy's path rules give a meaning.
func writable(p string) bool {
	if !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) || strings.ContainsRune(`*?[]\"`, r) {
			return false
		}
	}
	return true
}

// nestedIn reports whether p is below one of roots. roots is sorted, and
// shorter prefixes come first, so an ancestor is always already there.
func nestedIn(p string, roots []string) bool {
	for _, r := range roots {
		if strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}
