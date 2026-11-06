package gate

import (
	"slices"
	"strings"
	"testing"
)

// TestCopyArgs checks that copyArgs tells the destination from the sources by
// position, whichever way the target directory option is spelled.
func TestCopyArgs(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		dests, src []int
		ok         bool
	}{
		{"cp", "a b", []int{1}, []int{0}, true},
		{"cp", "-r a b c", []int{3}, []int{1, 2}, true},
		{"cp", "-t d a b", []int{1}, []int{2, 3}, true},
		{"cp", "-rt d a", []int{1}, nil, true},
		{"cp", "-vrt d a b", []int{1}, nil, true},
		{"cp", "-nt d a", []int{1}, nil, true},
		{"cp", "-rtd a", []int{0}, nil, true},
		{"cp", "--target d a", []int{1}, nil, true},
		{"cp", "--target-dir d a", []int{1}, nil, true},
		{"cp", "--target-dir=d a", []int{0}, nil, true},
		{"cp", "--target-directory d a", []int{1}, nil, true},
		{"cp", "-S .bak a b", []int{3}, []int{2}, true},
		{"cp", "--suffix .bak a b", []int{3}, []int{2}, true},
		{"cp", "--suffix=.bak a b", []int{2}, []int{1}, true},
		{"cp", "-- -a b", []int{2}, []int{1}, true},
		{"cp", "a", nil, []int{0}, true},
		{"cp", "--no a b", nil, nil, false},
		{"cp", "-Q a b", nil, nil, false},
		{"cp", "-rQ a b", nil, nil, false},
		{"cp", "--frob a b", nil, nil, false},
		{"cp", "--archive=x a b", nil, nil, false},
		{"cp", "a -t", nil, nil, false},
		{"cp", "a -S", nil, nil, false},
		{"mv", "-vt d a", []int{1}, nil, true},
		{"ln", "-s a b", []int{2}, []int{1}, true},
		{"install", "-d a b", []int{1, 2}, nil, true},
		{"install", "-Dt d a", []int{1}, nil, true},
		{"install", "-m 644 a b", []int{3}, []int{2}, true},
		{"install", "-m644 a b", []int{2}, []int{1}, true},
		{"rsync", "-av a b", []int{2}, []int{1}, true},
		{"rsync", "-e ssh a b", []int{3}, []int{2}, true},
		{"rsync", "--zzz a b", nil, nil, false},
		{"tee", "-a a b", []int{1, 2}, nil, true},
		{"tee", "--bogus a", nil, nil, false},
	} {
		var words []xword
		for _, w := range strings.Fields(tc.line) {
			words = append(words, xword{text: w, known: true})
		}
		dests, srcs, ok := copyArgs(tc.name, words)
		if ok != tc.ok {
			t.Errorf("%s %s: ok = %v, want %v", tc.name, tc.line, ok, tc.ok)
			continue
		}
		var idx []int
		for _, d := range dests {
			idx = append(idx, d.idx)
		}
		if !slices.Equal(idx, tc.dests) && (len(idx) != 0 || len(tc.dests) != 0) {
			t.Errorf("%s %s: dests = %v, want %v", tc.name, tc.line, idx, tc.dests)
		}
		if tc.src != nil && !slices.Equal(srcs, tc.src) {
			t.Errorf("%s %s: sources = %v, want %v", tc.name, tc.line, srcs, tc.src)
		}
	}
}
