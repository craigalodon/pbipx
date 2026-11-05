package gate

import (
	"slices"
	"testing"
)

func TestPathForms(t *testing.T) {
	if got := pathForms("/home/u/copy", false); !slices.Equal(got, []string{"/home/u/copy"}) {
		t.Errorf("elsewhere: %q", got)
	}
	want := []string{`C:\Users\u\copy`, "C:/Users/u/copy", "/c/Users/u/copy"}
	if got := pathForms(`C:\Users\u\copy`, true); !slices.Equal(got, want) {
		t.Errorf("Windows: %q, want %q", got, want)
	}
	// A line that spells any form of the path as a whole word names it.
	for _, form := range want {
		if !namesPath("python -c \"open('"+form+"/f')\"", form, true) {
			t.Errorf("a line spelling %q does not name it", form)
		}
	}
	if namesPath("cat C:/Users/u/copy-other/f", "C:/Users/u/copy", true) {
		t.Error("a sibling whose name starts with the path's is named")
	}
}
