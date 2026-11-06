package gate

import "strings"

// hintWidenedCd ends a deny whose line holds a cd that was judged in the
// folder before it as well. It is fixed words: no source text.
const hintWidenedCd = "A cd joined to the next command with && is judged from its target alone; any other cd is judged from the folder before it too."

// holdsWidenedCd reports whether line holds a cd, pushd or chdir that is not
// directly followed by && (or follows ||) and has a command after it: the
// kind mayHaveFailed widens.
func holdsWidenedCd(line string) bool {
	cmds := splitCommands(line)
	for i, c := range cmds {
		if i+1 >= len(cmds) || len(c.words) == 0 {
			continue
		}
		name := ""
		for _, w := range c.words {
			if w.redirect || strings.Contains(w.text, "=") && !strings.HasPrefix(w.text, "-") && name == "" {
				continue
			}
			name = w.text
			break
		}
		if (name == "cd" || name == "pushd" || name == "chdir") && (!c.andNext || c.prevOp == '|') {
			return true
		}
	}
	return false
}
