//go:build unix

package gitrepo

import "syscall"

const searchAccess = 0x1

func searchable(path string) bool { return syscall.Access(path, searchAccess) == nil }
