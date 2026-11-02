package main

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	bytesPerShellCheck = 4 << 30
	kibibyte           = 1 << 10
)

func referenceWorkers() int {
	available, known := availableMemory()
	if !known {
		return runtime.NumCPU()
	}
	return max(1, min(runtime.NumCPU(), int(available/bytesPerShellCheck)))
}

func availableMemory() (uint64, bool) {
	available, known := memAvailable()
	limit, limited := cgroupRoom()
	switch {
	case known && limited:
		return min(available, limit), true
	case limited:
		return limit, true
	}
	return available, known
}

func memAvailable() (uint64, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) < 2 || fields[0] != "MemAvailable:" {
			continue
		}
		kib, err := strconv.ParseUint(fields[1], 10, 64)
		return kib * kibibyte, err == nil
	}
	return 0, false
}

func cgroupRoom() (uint64, bool) {
	membership, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return 0, false
	}
	_, path, found := strings.Cut(strings.TrimSpace(string(membership)), "::")
	if !found {
		return 0, false
	}
	var room uint64
	limited := false
	for dir := filepath.Join("/sys/fs/cgroup", path); strings.HasPrefix(dir, "/sys/fs/cgroup"); dir = filepath.Dir(dir) {
		raw, err := os.ReadFile(filepath.Join(dir, "memory.max"))
		if err != nil {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil {
			continue
		}
		if !limited || value < room {
			room, limited = value, true
		}
	}
	return room, limited
}
