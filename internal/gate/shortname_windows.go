package gate

import "syscall"

// shortPathName is the 8.3 short name of the existing path p, as Windows
// gives it; ok is false when it has none or the call fails.
func shortPathName(p string) (string, bool) {
	in, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return "", false
	}
	return syscall.UTF16ToString(buf[:n]), true
}
