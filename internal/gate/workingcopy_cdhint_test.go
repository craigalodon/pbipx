package gate_test

import (
	"strings"
	"testing"
)

// TestWidenedCdHint checks that a deny whose line holds a cd judged from the
// folder before it as well says so, in fixed words, and that no other deny does.
func TestWidenedCdHint(t *testing.T) {
	m := newWCMachine(t)
	const hint = "A cd joined to the next command with && is judged from its target alone"
	h := m.ctl.home
	for name, tc := range map[string]struct {
		line string
		want bool
	}{
		"a cd then a read":        {"cd nosuch; cat work/copy/model.txt", true},
		"a cd after ||":           {"true || cd nosuch && cat work/copy/model.txt", true},
		"a subshell cd":           {"(cd nosuch) && cat work/copy/model.txt", true},
		"a cd with && and a deny": {"cd nosuch && cat ~/work/copy/model.txt", false},
		"no cd":                   {"cat ~/work/copy/model.txt", false},
		"a cd with nothing after": {"cat ~/work/copy/model.txt; cd nosuch", false},
	} {
		t.Run(name, func(t *testing.T) {
			d := m.decide(wcShell(tc.line), h)
			if d.Allow {
				t.Fatalf("allowed: %s", tc.line)
			}
			if got := strings.Contains(d.Message, hint); got != tc.want {
				t.Errorf("hint present = %v, want %v: %s", got, tc.want, d.Message)
			}
			if strings.Contains(d.Message, "nosuch") {
				t.Errorf("the message holds source text: %s", d.Message)
			}
		})
	}
}
