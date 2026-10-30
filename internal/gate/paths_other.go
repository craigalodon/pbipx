//go:build !windows

package gate

import "io/fs"

// isLink reports whether resolvePath follows the entry: a symlink.
func isLink(info fs.FileInfo) bool { return info.Mode()&fs.ModeSymlink != 0 }

// longName is p: only Windows has short names.
func longName(p string) string { return p }
