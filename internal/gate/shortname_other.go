//go:build !windows

package gate

// shortPathName has no short names to give off Windows.
func shortPathName(string) (string, bool) { return "", false }
