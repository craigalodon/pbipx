package gate

// Printed is printed, for the differential test's escape comparison.
func Printed(name string, args []string) (string, bool) { return printed(name, args) }

// SetFoldCase sets whether paths are compared without regard to case, for
// tests that run either way on any platform, and returns a function that
// restores the setting.
func SetFoldCase(fold bool) (restore func()) {
	old := foldCase
	foldCase = fold
	return func() { foldCase = old }
}
