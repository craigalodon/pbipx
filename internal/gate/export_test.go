package gate

import (
	"path/filepath"
)

// SetFoldCase sets whether paths are compared without regard to case, for
// tests that run either way on any platform, and returns a function that
// restores the setting.
func SetFoldCase(fold bool) (restore func()) {
	old := foldCase
	foldCase = fold
	return func() { foldCase = old }
}

// SetMSYS turns on the reading of Git Bash paths, as on Windows, on any
// platform: drive letter l's root is the directory drives/l (lower case),
// and an MSYS2 path off the drives is placed by mount (nil: none is). It
// returns a function that restores the mode.
func SetMSYS(drives string, mount func(string) (string, bool)) (restore func()) {
	old := msys
	if mount == nil {
		mount = func(string) (string, bool) { return "", false }
	}
	msys = msysMode{
		on: true,
		drive: func(l byte) string {
			return filepath.Join(drives, string(l|0x20)) + string(filepath.Separator)
		},
		mount: mount,
	}
	return func() { msys = old }
}

// Cygpath is the cygpath the gate would run, "" when there is none.
func Cygpath() string { return findCygpath() }

// SetHostMSYS restores the platform's own reading of Git Bash paths (on
// for Windows) for the test, and returns a function that restores the mode.
func SetHostMSYS() (restore func()) {
	old := msys
	msys = hostMSYS()
	return func() { msys = old }
}
