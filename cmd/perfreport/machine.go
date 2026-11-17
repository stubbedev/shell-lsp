package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

const (
	cpuInfoFile = "/proc/cpuinfo"
	memInfoFile = "/proc/meminfo"
	kibibyte    = 1 << 10
)

func describeMachine(runner string) (machine, error) {
	cpuInfo, err := os.ReadFile(cpuInfoFile)
	if err != nil {
		return machine{}, fmt.Errorf("reading the processor model: %w", err)
	}
	cpu, err := procField(string(cpuInfo), "model name")
	if err != nil {
		return machine{}, fmt.Errorf("%s: %w", cpuInfoFile, err)
	}
	memInfo, err := os.ReadFile(memInfoFile)
	if err != nil {
		return machine{}, fmt.Errorf("reading the memory size: %w", err)
	}
	total, err := procField(string(memInfo), "MemTotal")
	if err != nil {
		return machine{}, fmt.Errorf("%s: %w", memInfoFile, err)
	}
	kib, err := strconv.ParseUint(strings.TrimSuffix(total, " kB"), 10, 64)
	if err != nil {
		return machine{}, fmt.Errorf("%s: reading MemTotal %q: %w", memInfoFile, total, err)
	}
	return machine{
		Runner:      runner,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Cores:       runtime.NumCPU(),
		CPU:         cpu,
		MemoryBytes: kib * kibibyte,
	}, nil
}

func procField(text, key string) (string, error) {
	for line := range strings.Lines(text) {
		name, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(name) == key {
			return strings.TrimSpace(value), nil
		}
	}
	return "", fmt.Errorf("no %s field", key)
}
