package gate

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/scan"
)

// PowerShell and LSP calls (behavior.md, "The gate", PowerShell and LSP).
//
// The gate has no parser for PowerShell. A PowerShell call is therefore
// judged as if it touched every protected root of each adopted repository
// it reaches: its working directory, and every word of its line that looks
// like a path (it holds a slash or a backslash, or names an entry of the
// working directory). Within one, the protected tree is scanned, after a
// word naming a file that controls the gate (agent-settings) or local
// state (local-state) has denied.

// maxPSWords bounds the words of a PowerShell line the gate places: each
// costs file system calls, and a hook that runs past Claude Code's timeout
// lets the call through.
const maxPSWords = 20000

// psWords splits a PowerShell line into words, as far as the gate reads
// it: quotes ('...', "..." and the typographic ones) group, a backtick
// escapes the next character, and whitespace and ; | & ( ) { } , < > =
// separate. A parameter given as -Name:value also yields its value. ok is
// false past maxPSWords words.
func psWords(line string) (words []string, ok bool) {
	var cur strings.Builder
	have := false
	flush := func() {
		if have {
			words = append(words, cur.String())
		}
		cur.Reset()
		have = false
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '`':
			if i+1 < len(rs) {
				i++
				if rs[i] != '\n' && rs[i] != '\r' {
					cur.WriteRune(rs[i])
					have = true
				}
			}
		case psSingle(r):
			have = true
			for i++; i < len(rs); i++ {
				if psSingle(rs[i]) {
					if i+1 < len(rs) && psSingle(rs[i+1]) {
						i++
						cur.WriteRune(rs[i])
						continue
					}
					break
				}
				cur.WriteRune(rs[i])
			}
		case psDouble(r):
			have = true
			for i++; i < len(rs); i++ {
				if rs[i] == '`' && i+1 < len(rs) {
					i++
					cur.WriteRune(rs[i])
					continue
				}
				if psDouble(rs[i]) {
					break
				}
				cur.WriteRune(rs[i])
			}
		case unicode.IsSpace(r) || strings.ContainsRune(";|&(){},<>=", r):
			flush()
		default:
			cur.WriteRune(r)
			have = true
		}
		if len(words) > maxPSWords {
			return nil, false
		}
	}
	flush()
	var out []string
	for _, w := range words {
		out = append(out, w)
		if strings.HasPrefix(w, "-") {
			if _, v, cut := strings.Cut(w, ":"); cut && v != "" {
				out = append(out, v)
			}
		}
	}
	if len(out) > maxPSWords {
		return nil, false
	}
	return out, true
}

func psSingle(r rune) bool { return strings.ContainsRune("'‘’‚‛", r) }
func psDouble(r rune) bool { return strings.ContainsRune("\"“”„", r) }

// psVariants are the paths a PowerShell word can stand for: as written,
// and where the system does not take a backslash as a separator, with
// backslashes as slashes, since PowerShell does. A leading "~" is the
// gate's home directory.
func psVariants(w, home string) []string {
	out := []string{expandHome(w, home)}
	if filepath.Separator != '\\' && strings.Contains(w, `\`) {
		out = append(out, expandHome(strings.ReplaceAll(w, `\`, "/"), home))
	}
	return out
}

// psPathLike reports whether a PowerShell word, against the working
// directory cwd, looks like a path: it holds a slash or a backslash, is
// "~", or names an entry of cwd.
func psPathLike(cwd, w string) bool {
	if w == "" {
		return false
	}
	if strings.ContainsAny(w, `/\`) || w == "~" {
		return true
	}
	_, err := os.Lstat(filepath.Join(cwd, w))
	return err == nil
}

// probePowerShell are the paths a PowerShell line touches, and whether it
// touches one that cannot be placed: the working directory, which a
// command can read under, and every word that looks like a path; a word
// that looks like a path and holds an expansion ($) cannot be placed, nor
// can a line with too many words.
func probePowerShell(cwd, home, line string) (spots []spot, unknown bool) {
	spots = append(spots, spot{path: cwd, container: true})
	words, ok := psWords(line)
	if !ok {
		return spots, true
	}
	seen := map[string]bool{}
	for _, w := range words {
		if seen[w] || !psPathLike(cwd, w) {
			continue
		}
		seen[w] = true
		if strings.Contains(w, "$") {
			unknown = true
		}
		for _, v := range psVariants(w, home) {
			for _, a := range spellings(cwd, v) {
				spots = append(spots, spot{path: a, container: true})
			}
		}
	}
	return spots, unknown
}

// psControl is the deny for a PowerShell line that names a file outside
// the repository that controls the gate, or a user-level Claude Code
// settings directory, or that runs 'pbipx policy approve'. The gate
// cannot tell a PowerShell read from a write, so naming one is enough.
func psControl(cwd, home string, words []string, ctl controls) (Decision, bool) {
	seen := map[string]bool{}
	for i, w := range words {
		if strings.EqualFold(w, "policy") && i+1 < len(words) && strings.EqualFold(words[i+1], "approve") {
			return deny(ReasonAgentSettings, "the command may run 'pbipx policy approve'; "+
				"approving the policy is a person's step at a terminal, and an agent may not approve the policy that binds it"), true
		}
		// A word is placed once: each placing costs file system calls.
		if seen[w] {
			continue
		}
		seen[w] = true
		for _, v := range psVariants(w, home) {
			sp := spellings(cwd, v)
			what, ok := ctl.target(sp)
			if !ok {
				what, ok = ctl.dir(sp)
			}
			if ok {
				return deny(ReasonAgentSettings, "the PowerShell command names a path %s; %s", what, controlWhy), true
			}
		}
	}
	return Decision{}, false
}

// powershell decides a PowerShell call made in or reaching the setup's
// repository (behavior.md, "The gate", PowerShell): a word naming a Claude
// Code settings file or a .claude directory in the repository, or a file
// outside it that controls the gate, denies with agent-settings; a word
// naming local state with local-state; a git command that could print
// content git history holds with git-content (psGit); and otherwise the
// protected tree is scanned, as if the command touched every root.
func (s *setup) powershell(line string) Decision {
	words, ok := psWords(line)
	if !ok {
		return deny(ReasonInternalError, "the PowerShell command holds more than %d words, too many to judge", maxPSWords)
	}
	if d, ok := psControl(s.cwd, s.home, words, loadControls()); ok {
		return d
	}
	seen := map[string]bool{}
	var local string
	for _, w := range words {
		if seen[w] || w == "" {
			continue
		}
		seen[w] = true
		for _, v := range psVariants(w, s.home) {
			for _, rel := range s.rels(v) {
				if scan.IsAgentSettings(rel) || strings.EqualFold(path.Base(rel), ".claude") {
					return deny(ReasonAgentSettings, "the PowerShell command names a Claude Code settings file "+
						"or a .claude directory; %s", agentSettingsWhy)
				}
				if local == "" {
					if class, _ := s.policy.Classify(rel); class == policy.LocalState || s.policy.LocalStateDir(rel) || s.nestedLocalState(rel) {
						local = rel
					}
				}
			}
		}
	}
	// git printing content git history holds, as for Bash.
	if d, ok := s.psGit(line); ok {
		return d
	}
	if local != "" {
		return deny(ReasonLocalState, "the PowerShell command names %s, Power BI Desktop local state, which is never read",
			displayPath(local))
	}
	return s.tree(trigger{"the gate cannot read a PowerShell command, so it takes it to touch every protected root",
		"Use the Read tool for a single file, or run the command from outside the protected folders."})
}

// lsp decides an LSP call (behavior.md, "The gate", LSP): each path it
// names is judged as a Read, or as a Grep when it is a directory; a call
// that names none is taken to touch the working directory's whole
// project, and scans the protected tree.
func (s *setup) lsp(paths []string) Decision {
	if len(paths) == 0 {
		return s.tree(trigger{"a code intelligence (LSP) call names no file, so the gate takes it to touch the whole project",
			"Name the file to look up, outside the protected folders."})
	}
	for _, p := range paths {
		d := s.read(p)
		if info, err := os.Stat(rawJoin(s.cwd, expandHome(p, s.home))); err == nil && info.IsDir() {
			d = s.search(p)
		}
		if !d.Allow {
			return d
		}
	}
	return allow()
}
