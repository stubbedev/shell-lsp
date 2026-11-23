//go:build unix

package platform

import (
	"io/fs"
	"syscall"
)

func DeviceOf(path string) (uint64, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return 0, &fs.PathError{Op: "stat", Path: path, Err: err}
	}
	return uint64(stat.Dev), nil //nolint:unconvert // Dev is 32 bits wide on some architectures
}
