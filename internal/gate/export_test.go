package gate

import (
	"errors"
	"path/filepath"
	"time"
)

// Printed is printed, for the differential test's escape comparison.
func Printed(name string, args []string, mayEscape bool) (string, bool) {
	return printed(name, args, mayEscape)
}

// SetFoldCase sets whether paths are compared without regard to case, for
// tests that run either way on any platform, and returns a function that
// restores the setting.
func SetFoldCase(fold bool) (restore func()) {
	old := foldCase
	foldCase = fold
	return func() { foldCase = old }
}

// SetMSYS turns the reading of Git Bash paths that are not drive paths
// (/tmp/x) on or off, as on Windows, on any platform, with place standing
// for Git Bash's cygpath -w: ok false is a run that fails, and a nil place
// is a cygpath that is not found. It returns a
// function that restores the mode.
func SetMSYS(on bool, place func(arg string) (placed string, ok bool)) (restore func()) {
	old := msys
	msys = msysMode{
		on: on,
		find: func(*msysSession) (string, bool) {
			if place == nil {
				return "", false
			}
			return "cygpath.exe", true
		},
		run: func(_, arg string, _ time.Duration) ([]byte, error) {
			p, ok := place(arg)
			if !ok {
				return nil, errors.New("cygpath failed")
			}
			return []byte(filepath.ToSlash(p) + "\n"), nil
		},
		perRun:  2 * time.Second,
		total:   10 * time.Second,
		maxRuns: 32,
	}
	return func() { msys = old }
}
