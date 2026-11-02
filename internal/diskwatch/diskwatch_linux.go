//go:build linux

package diskwatch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"unsafe"
)

const (
	inotifyHeaderSize = 16
	watchMask         = syscall.IN_MODIFY | syscall.IN_ATTRIB | syscall.IN_CLOSE_WRITE | syscall.IN_CREATE | syscall.IN_DELETE |
		syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF | syscall.IN_ONLYDIR
	linkedMask = syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO
	lostMask   = syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF | syscall.IN_IGNORED | syscall.IN_UNMOUNT

	magicNFS    = 0x6969
	magicSMB    = 0x517B
	magicCIFS   = 0xFF534D42
	magicSMB2   = 0xFE534D42
	magicFUSE   = 0x65735546
	magic9P     = 0x01021997
	magicAFS    = 0x5346414F
	magicCeph   = 0x00C36400
	magicLustre = 0x0BD00BD0
	magicGPFS   = 0x47504653
)

type inotifySource struct {
	fd      int
	devices map[uint64]bool
}

func New() (*Watcher, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("creating an inotify instance: %w", err)
	}
	return newWatcher(&inotifySource{fd: fd, devices: map[uint64]bool{}}), nil
}

func (s *inotifySource) add(dir string) (int32, error) {
	wd, err := syscall.InotifyAddWatch(s.fd, dir, watchMask)
	if err != nil {
		return 0, fmt.Errorf("watching %s: %w", dir, err)
	}
	return int32(wd), nil //nolint:gosec // the kernel hands out watch descriptors as 32-bit integers
}

func (s *inotifySource) remove(wd int32) {
	_, _ = syscall.InotifyRmWatch(s.fd, uint32(wd)) //nolint:gosec // the descriptor came from InotifyAddWatch, which never returns a negative one
}

func (s *inotifySource) queued() (int, error) {
	var pending int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), uintptr(syscall.TIOCINQ), uintptr(unsafe.Pointer(&pending))) //nolint:gosec // the kernel writes the queue length through this pointer during the call
	if errno != 0 {
		return 0, fmt.Errorf("asking how many inotify events wait: %w", errno)
	}
	return int(pending), nil
}

func (s *inotifySource) read(buffer []byte, handle func(kernelEvent)) error {
	remaining, err := s.queued()
	if err != nil {
		return err
	}
	for remaining > 0 {
		n, err := syscall.Read(s.fd, buffer)
		switch {
		case errors.Is(err, syscall.EAGAIN):
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case err != nil:
			return fmt.Errorf("reading inotify events: %w", err)
		case n < inotifyHeaderSize:
			return fmt.Errorf("reading inotify events: a short read of %d bytes", n)
		}
		decodeEvents(buffer[:n], handle)
		remaining -= n
	}
	return nil
}

func decodeEvents(raw []byte, handle func(kernelEvent)) {
	for len(raw) >= inotifyHeaderSize {
		mask := binary.NativeEndian.Uint32(raw[4:])
		length := binary.NativeEndian.Uint32(raw[12:])
		end := inotifyHeaderSize + int(length)
		handle(kernelEvent{
			wd:   int32(binary.NativeEndian.Uint32(raw)), //nolint:gosec // the wire format stores the signed descriptor in these four bytes
			mask: classify(mask),
			name: nameBefore(raw[inotifyHeaderSize:end]),
		})
		raw = raw[end:]
	}
}

func classify(mask uint32) eventMask {
	var out eventMask
	if mask&syscall.IN_Q_OVERFLOW != 0 {
		out |= eventOverflow
	}
	if mask&lostMask != 0 {
		out |= eventLost
	}
	if mask&linkedMask != 0 {
		out |= eventLinked
	}
	return out
}

func (s *inotifySource) close() { _ = syscall.Close(s.fd) }

func (s *inotifySource) remote(dir string, device uint64) bool {
	if known, seen := s.devices[device]; seen {
		return known
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return true
	}
	remote := false
	switch uint64(stat.Type) { //nolint:gosec // filesystem magic numbers are 32-bit identifiers
	case magicNFS, magicSMB, magicCIFS, magicSMB2, magicFUSE, magic9P, magicAFS, magicCeph, magicLustre, magicGPFS:
		remote = true
	}
	s.devices[device] = remote
	return remote
}

func (s *inotifySource) inspect(path string) (pathKind, error) {
	var stat syscall.Stat_t
	if err := syscall.Lstat(path, &stat); err != nil {
		return pathKind{}, err
	}
	return pathKind{symlink: stat.Mode&syscall.S_IFMT == syscall.S_IFLNK, device: uint64(stat.Dev)}, nil //nolint:unconvert // Dev is 32 bits wide on some architectures
}

func isAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func nameBefore(raw []byte) []byte {
	name, _, _ := bytes.Cut(raw, []byte{0})
	return name
}
