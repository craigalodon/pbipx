//go:build unix

package setup

import "syscall"

// NoFollow is the open flag that makes opening a symbolic link fail, so a
// file pbipx writes in a repository is never one a link points elsewhere.
const NoFollow = syscall.O_NOFOLLOW
