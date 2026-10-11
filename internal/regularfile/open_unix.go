//go:build unix

package regularfile

import "syscall"

const openFlags = syscall.O_NONBLOCK
