//go:build windows

package proctree

import (
	"syscall"
	"time"
)

func processGone(pid int, within time.Duration) bool {
	process, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer syscall.CloseHandle(process)
	event, err := syscall.WaitForSingleObject(process, uint32(within/time.Millisecond))
	return err == nil && event == syscall.WAIT_OBJECT_0
}
