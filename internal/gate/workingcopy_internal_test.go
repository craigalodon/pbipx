package gate

import (
	"slices"
	"testing"
)

func TestPathForms(t *testing.T) {
	if got := pathForms("/home/u/copy", false, nil); !slices.Equal(got, []string{"/home/u/copy"}) {
		t.Errorf("elsewhere: %q", got)
	}
	want := []string{`C:\Users\u\copy`, "C:/Users/u/copy", "/c/Users/u/copy"}
	if got := pathForms(`C:\Users\u\copy`, true, nil); !slices.Equal(got, want) {
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

// TestPathFormsShortNames checks that on Windows a line can also spell a
// registered path with the 8.3 short names a temporary folder has: for each
// folder above the copy, its short name with the rest as registered, and
// all of it short.
func TestPathFormsShortNames(t *testing.T) {
	shorts := map[string]string{
		`C:\Users\runneradmin`: `C:\Users\RUNNER~1`,
		`C:\Users\runneradmin\AppData\Local\Temp\pbipx-wc-12345`:      `C:\Users\RUNNER~1\AppData\Local\Temp\PBIPX-~1`,
		`C:\Users\runneradmin\AppData\Local\Temp\pbipx-wc-12345\copy`: `C:\Users\RUNNER~1\AppData\Local\Temp\PBIPX-~1\copy`,
	}
	short := func(p string) (string, bool) { s, ok := shorts[p]; return s, ok }
	root := `C:\Users\runneradmin\AppData\Local\Temp\pbipx-wc-12345\copy`
	got := pathForms(root, true, short)
	for _, want := range []string{
		root,
		`C:/Users/runneradmin/AppData/Local/Temp/pbipx-wc-12345/copy`,
		`C:\Users\RUNNER~1\AppData\Local\Temp\pbipx-wc-12345\copy`,
		`C:/Users/RUNNER~1/AppData/Local/Temp/pbipx-wc-12345/copy`,
		`/c/Users/RUNNER~1/AppData/Local/Temp/pbipx-wc-12345/copy`,
		`C:/Users/RUNNER~1/AppData/Local/Temp/PBIPX-~1/copy`,
		`/c/Users/RUNNER~1/AppData/Local/Temp/PBIPX-~1/copy`,
	} {
		if !slices.Contains(got, want) {
			t.Errorf("no form %q in %q", want, got)
		}
	}
	// A name with no short form, or a failure, leaves the long forms.
	none := func(string) (string, bool) { return "", false }
	if got := pathForms(root, true, none); len(got) != 3 {
		t.Errorf("without short names: %q", got)
	}
	// Elsewhere there are no short names.
	if got := pathForms("/home/u/copy", false, short); len(got) != 1 {
		t.Errorf("not Windows: %q", got)
	}
	// A line with the short spelling names the copy, whole word only, in
	// any case.
	line := `python -c "open('c:/users/runner~1/AppData/Local/Temp/pbipx-wc-12345/copy/f')"`
	named := false
	for _, f := range got {
		named = named || namesPath(line, f, true)
	}
	if !named {
		t.Error("a line with the short spelling does not name the copy")
	}
	sibling := `cat C:/Users/RUNNER~1/AppData/Local/Temp/pbipx-wc-12345/copy-other/f`
	for _, f := range got {
		if namesPath(sibling, f, true) {
			t.Errorf("a sibling is named by %q", f)
		}
	}
}
