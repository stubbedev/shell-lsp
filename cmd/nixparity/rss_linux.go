//go:build linux

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

const procDir = "/proc"

func sampleTreeRSS(group int) (int64, error) {
	total, err := currentTreeRSS(group)
	if err != nil {
		return 0, err
	}
	status, err := readProcFile(strconv.Itoa(group), "status")
	if err != nil {
		return 0, err
	}
	leaderPeak, err := parsePeakRSS(status)
	if err != nil {
		return 0, err
	}
	return max(total, leaderPeak), nil
}

func currentTreeRSS(group int) (int64, error) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return 0, fmt.Errorf("listing %s: %w", procDir, err)
	}
	page := int64(os.Getpagesize())
	var total int64
	for _, entry := range entries {
		if !scan.All(entry.Name(), scan.Digit) {
			continue
		}
		raw, err := readProcFile(entry.Name(), "stat")
		if err != nil {
			return 0, err
		}
		if raw == "" {
			continue
		}
		stat, err := parseProcStat(raw)
		if err != nil {
			return 0, err
		}
		if stat.group == group {
			total += stat.rssPages * page
		}
	}
	return total, nil
}

func readProcFile(pid, name string) (string, error) {
	path := filepath.Join(procDir, pid, name)
	raw, err := os.ReadFile(path) //nolint:gosec
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ESRCH):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(raw), nil
}
