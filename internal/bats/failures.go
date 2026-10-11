package bats

import (
	"path/filepath"
	"strings"
)

type Failure struct {
	Test    string
	Line    int
	Message string
}

func Failures(report Report, file, dir string, testLine func(name string) (int, bool)) []Failure {
	var out []Failure
	for _, r := range report.Results {
		if r.Status != Failed {
			continue
		}
		out = append(out, Failure{Test: r.Name, Line: failureLine(r, file, dir, testLine), Message: failureMessage(r)})
	}
	return out
}

func failureLine(r Result, file, dir string, testLine func(string) (int, bool)) int {
	for _, frame := range r.Frames {
		if resolve(frame.File, dir) == file {
			return frame.Line
		}
	}
	if line, ok := testLine(r.Name); ok {
		return line
	}
	return 1
}

func resolve(path, dir string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}

func failureMessage(r Result) string {
	var sb strings.Builder
	sb.WriteString("test failed: ")
	sb.WriteString(r.Name)
	for _, line := range r.Output {
		sb.WriteByte('\n')
		sb.WriteString(line)
	}
	return sb.String()
}
