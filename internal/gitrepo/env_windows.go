package gitrepo

import (
	"errors"
	"syscall"
)

const errorNoneMapped syscall.Errno = 1332

func unmappedAccount(err error) bool { return errors.Is(err, errorNoneMapped) }
