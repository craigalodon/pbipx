//go:build windows

package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/gate"
)

// shortPath is the 8.3 name of an existing path, or p when the volume makes
// none.
func shortPath(t *testing.T, p string) string {
	t.Helper()
	in, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return syscall.UTF16ToString(buf[:n])
}

// driveBash is a drive path as bash writes it: C:\a\b is /c/a/b.
func driveBash(p string) string {
	s := filepath.ToSlash(p)
	return "/" + strings.ToLower(s[:1]) + s[2:]
}

// TestWorkingCopyWindowsSpellings proves that the spellings a Windows user
// has for a working copy deny: forward slashes, a quoted backslash path,
// bash's /c/ form, the other case of the drive letter, and the 8.3 short
// name a runner's temporary folder has (RUNNER~1). One working copy is under
// the home folder as the machine makes it, the other under %TEMP%, as
// os.MkdirTemp makes it, short names and all.
func TestWorkingCopyWindowsSpellings(t *testing.T) {
	m := newWCMachine(t)
	tmp, err := os.MkdirTemp("", "pbipx-wc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	tmpCopy := filepath.Join(tmp, "copy")
	write(t, tmpCopy, "f.txt", "plain\n")
	if err := m.store.AddWorkingCopy(approval.WorkingCopy{Path: tmpCopy}); err != nil {
		t.Fatal(err)
	}
	wc := gate.ReasonWorkingCopy
	var cases []wcCase
	for name, dir := range map[string]string{"home": m.wc, "temp": tmpCopy} {
		file := filepath.Join(dir, "x.txt")
		if name == "temp" {
			file = filepath.Join(dir, "f.txt")
		}
		short := shortPath(t, file)
		lowerDrive := strings.ToLower(file[:1]) + file[1:]
		upperDrive := strings.ToUpper(file[:1]) + file[1:]
		for form, cmd := range map[string]string{
			"forward slashes":              "cat " + filepath.ToSlash(file),
			"forward slashes, short name":  "cat " + filepath.ToSlash(short),
			"single-quoted backslashes":    "cat '" + file + "'",
			"double-quoted backslashes":    `cat "` + file + `"`,
			"doubled backslashes":          "cat " + strings.ReplaceAll(file, `\`, `\\`),
			"bash drive form":              "cat " + driveBash(file),
			"bash drive form, short name":  "cat " + driveBash(short),
			"lower-case drive letter":      "cat " + filepath.ToSlash(lowerDrive),
			"upper-case drive letter":      "cat " + filepath.ToSlash(upperDrive),
			"redirect, forward slashes":    "echo hi > " + filepath.ToSlash(file),
			"cd, forward slashes":          "cd " + filepath.ToSlash(dir) + " && ls",
			"cd, bash drive form":          "cd " + driveBash(dir) + " && ls",
			"ls, short name":               "ls " + filepath.ToSlash(shortPath(t, dir)),
			"a path an interpreter opens":  `python -c "open('` + filepath.ToSlash(file) + `')"`,
			"a path an interpreter, bash":  `python -c "open('` + driveBash(file) + `')"`,
			"a glob over the last segment": "cat " + filepath.ToSlash(filepath.Dir(dir)) + "/cop*/" + filepath.Base(file),
		} {
			cases = append(cases, wcCase{name + ": " + form, "", wcShell(cmd), wc})
		}
		cases = append(cases,
			wcCase{name + ": read, backslashes", "", wcRead(file), wc},
			wcCase{name + ": read, forward slashes", "", wcRead(filepath.ToSlash(file)), wc},
			wcCase{name + ": read, short name", "", wcRead(short), wc},
			wcCase{name + ": read, lower-case drive", "", wcRead(lowerDrive), wc},
			wcCase{name + ": write, forward slashes", "", wcWrite(filepath.ToSlash(filepath.Join(dir, "new.json"))), wc},
			wcCase{name + ": search, short name", "", wcSearch(shortPath(t, dir)), wc},
			wcCase{name + ": PowerShell, backslashes", "", wcPS("Get-Content '" + file + "'"), wc},
			wcCase{name + ": PowerShell, short name", "", wcPS("Get-Content " + short), wc},
		)
	}
	m.run(t, cases)
}

// TestWorkingCopyWindowsTempThroughMount proves a working copy under %TEMP%
// is denied by the /tmp spelling bash gives it, through the seam for
// cygpath, which on Windows places /tmp where %TEMP% is.
func TestWorkingCopyWindowsTempThroughMount(t *testing.T) {
	m := newWCMachine(t)
	tmp, err := os.MkdirTemp("", "pbipx-wc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	tmpCopy := filepath.Join(tmp, "copy")
	write(t, tmpCopy, "f.txt", "plain\n")
	if err := m.store.AddWorkingCopy(approval.WorkingCopy{Path: tmpCopy}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gate.SetMSYS(true, func(arg string) (string, bool) {
		if arg == "/tmp" || strings.HasPrefix(arg, "/tmp/") {
			// cygpath -w prints the Windows path, short name or not.
			return filepath.Join(filepath.Dir(tmp), strings.TrimPrefix(arg, "/tmp/")), true
		}
		return "", false
	}))
	rel := filepath.ToSlash(filepath.Join(filepath.Base(tmp), "copy", "f.txt"))
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"cat through /tmp", "", wcShell("cat /tmp/" + rel), wc},
		{"read through /tmp", "", wcRead("/tmp/" + rel), wc},
		{"redirect through /tmp", "", wcShell("echo hi > /tmp/" + rel), wc},
		{"cd through /tmp", "", wcShell("cd /tmp/" + filepath.ToSlash(filepath.Join(filepath.Base(tmp), "copy")) + " && ls"), wc},
		{"a sibling through /tmp", "", wcShell("cat /tmp/" + filepath.Base(tmp) + "/other.txt"), ""},
	})
}
