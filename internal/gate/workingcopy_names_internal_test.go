package gate

import "testing"

// TestNamesCache pins the name backstop, including the case rule: a folder
// and file of that name, in the case written unless the file system folds.
func TestNamesCache(t *testing.T) {
	for _, tc := range []struct {
		path string
		fold bool
		want bool
	}{
		{"/a/.pbi/cache.abf", false, true},
		{"/a/.PBI/CACHE.ABF", false, false},
		{"/a/.PBI/CACHE.ABF", true, true},
		{"/a/.pbi/cache.abf.bak", true, false},
		{"/a/x.pbi/cache.abf", true, false},
		{"/a/.pbi/other", true, false},
	} {
		if got := namesCache(tc.path, tc.fold); got != tc.want {
			t.Errorf("namesCache(%q, %v) = %v, want %v", tc.path, tc.fold, got, tc.want)
		}
	}
}
