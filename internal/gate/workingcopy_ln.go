package gate

import (
	"path/filepath"
	"strings"
)

// Symbolic links (behavior.md, "Working copies").
//
// The text of a symbolic link is read when the link is followed, against the
// folder the link is in, not against the working directory of the command
// that made it. So `ln -s a/stage1 H/L`, run from elsewhere, makes H/L a
// path into H/a/stage1, and a read of H/L/x afterwards names no copy. The
// text is placed against the link's folder as well.

// symbolicLn reports whether v, an ln command, may make symbolic links:
// -s in a cluster, --symbolic or an abbreviation of it. Overapproximate: a
// letter s in the value of -t counts.
func symbolicLn(v view) bool {
	for _, a := range v.rest {
		switch {
		case a == "--":
			return false
		case strings.HasPrefix(a, "--"):
			name, _, _ := strings.Cut(a, "=")
			if abbreviates(name, "--symbolic") {
				return true
			}
		case strings.HasPrefix(a, "-") && strings.Contains(a, "s"):
			return true
		}
	}
	return false
}

// lnSpots are the spots the text of the symbolic links v makes can name: each
// source's text placed against each destination's folder and against the
// destination itself (-t, or an existing directory). A word that cannot be
// told counts as touching every copy. Where the options are not understood
// (copyArgs), any two words may be a source and a destination.
func lnSpots(s *setup, v view) []spot {
	if v.name != "ln" || !symbolicLn(v) {
		return nil
	}
	all := []spot{{only: true, all: true}}
	words := s.restWords(v)
	var srcs, dsts []int
	var skips []int // the length of the option's name attached to a destination
	dests, sources, ok := copyArgs("ln", words)
	if ok {
		srcs = sources
		for _, d := range dests {
			dsts = append(dsts, d.idx)
			skips = append(skips, d.skip)
		}
	} else {
		for i, xw := range words {
			if xw.redirect {
				continue
			}
			if xw.known && strings.HasPrefix(xw.text, "-") {
				continue
			}
			srcs, dsts, skips = append(srcs, i), append(dsts, i), append(skips, 0)
		}
	}
	var out []spot
	for _, si := range srcs {
		sw := words[si]
		if !sw.known {
			return all
		}
		if isAbs(sw.text) || fullPath(sw.text) {
			continue // placed already, the same against any folder
		}
		for k, di := range dsts {
			if di == si {
				continue
			}
			dw := words[di]
			dw.text = dw.text[min(skips[k], len(dw.text)):]
			dw.pattern, dw.glob = dw.text, false
			abss, unk := s.wordSpellings(v, dw)
			if unk {
				return all
			}
			for _, a := range abss {
				for _, dir := range []string{filepath.Dir(a), a} {
					for _, p := range s.spellingsAt(dir, sw.text) {
						out = append(out, spot{path: p, container: true, only: true})
					}
				}
			}
		}
	}
	return out
}
