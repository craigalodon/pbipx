//go:build windows

package gate

import (
	"io/fs"
	"syscall"
)

// isLink reports whether resolvePath follows the entry: a symlink, or a
// reparse point such as a junction (Lstat reports ModeIrregular for it).
// One os.Readlink cannot read is then taken as a plain entry.
func isLink(info fs.FileInfo) bool {
	return info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0
}

// longName is the existing path p with its last component in its long
// form: a short (8.3) name such as "RUNNER~1" is expanded. p is returned
// unchanged when Windows cannot say.
func longName(p string) string {
	in, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 260)
	for range 3 {
		n, err := syscall.GetLongPathName(in, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			return p
		}
		if int(n) < len(buf) {
			return syscall.UTF16ToString(buf[:n])
		}
		// Too small: n is the size needed, the final NUL included.
		buf = make([]uint16, n)
	}
	return p
}
