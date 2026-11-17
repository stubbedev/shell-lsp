package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

type report struct {
	Version    string       `json:"version"`
	Date       string       `json:"date"`
	Machine    machine      `json:"machine"`
	Go         string       `json:"go"`
	Repeats    int          `json:"repeats"`
	Keystrokes []keystroke  `json:"keystrokes"`
	Check      checkTiming  `json:"check"`
	Memory     []memoryRun  `json:"memory"`
	Format     formatSurvey `json:"format"`
}

type machine struct {
	Runner      string `json:"runner"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Cores       int    `json:"cores"`
	CPU         string `json:"cpu"`
	MemoryBytes uint64 `json:"memoryBytes"`
}

type tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type project struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

type keystroke struct {
	Name        string `json:"name"`
	MedianNanos int64  `json:"medianNanos"`
}

type timing struct {
	WallMicros int64 `json:"wallMicros"`
	CPUMicros  int64 `json:"cpuMicros"`
}

type checkTiming struct {
	Project   project `json:"project"`
	Files     int     `json:"files"`
	Ours      timing  `json:"ours"`
	Reference tool    `json:"reference"`
	Theirs    timing  `json:"theirs"`
}

type rss struct {
	Server    tool  `json:"server"`
	PeakBytes int64 `json:"peakBytes"`
	IdleBytes int64 `json:"idleBytes"`
	Restarts  int   `json:"restarts"`
}

type memoryRun struct {
	Project project `json:"project"`
	Files   int     `json:"files"`
	Ours    rss     `json:"ours"`
	Theirs  rss     `json:"theirs"`
}

type formatSurvey struct {
	Project      project  `json:"project"`
	Files        int      `json:"files"`
	Reference    tool     `json:"reference"`
	Differences  int      `json:"differences"`
	BothRejected int      `json:"bothRejected"`
	Examples     []string `json:"examples"`
}

func encodeReport(r report) ([]byte, error) {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the report: %w", err)
	}
	return append(raw, '\n'), nil
}

func decodeReport(raw []byte) (report, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var r report
	if err := decoder.Decode(&r); err != nil {
		return report{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return report{}, errors.New("more than one JSON value")
	}
	return r, nil
}

func readReport(path string) (report, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: the report path is the command's argument
	if err != nil {
		return report{}, fmt.Errorf("reading the performance report: %w", err)
	}
	r, err := decodeReport(raw)
	if err != nil {
		return report{}, fmt.Errorf("decoding the performance report %s: %w", path, err)
	}
	return r, nil
}
