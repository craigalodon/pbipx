package setup

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestWindowsHome(t *testing.T) {
	for home, want := range map[string]string{
		"/c/Users/me":    `C:\Users\me`,
		"/d":             `D:\`,
		"/c/":            `C:\`,
		`C:\Users\me`:    `C:\Users\me`,
		"C:/Users/me":    "C:/Users/me",
		"/home/me":       "/home/me",
		"/cd/x":          "/cd/x",
		"":               "",
		"relative/c/one": "relative/c/one",
	} {
		if got := windowsHome(home); got != want {
			t.Errorf("windowsHome(%q) = %q, want %q", home, got, want)
		}
	}
}

// TestUserSettingsDirsGitBashHome checks that a HOME Git Bash set in its
// own form puts that home's .claude among the user settings directories.
func TestUserSettingsDirsGitBashHome(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("HOME is read in Git Bash form on Windows only")
	}
	home := t.TempDir()
	vol := filepath.VolumeName(home)
	if len(vol) != 2 || vol[1] != ':' {
		t.Skip("the temporary directory is not on a drive")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "/"+strings.ToLower(vol[:1])+filepath.ToSlash(home[2:]))
	want := filepath.Join(strings.ToUpper(vol[:1])+home[1:], ".claude")
	if dirs := UserSettingsDirs(); !slices.Contains(dirs, want) {
		t.Errorf("UserSettingsDirs() = %q, want it to hold %q", dirs, want)
	}
}
