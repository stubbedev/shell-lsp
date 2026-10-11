package bats

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

type Failure struct {
	Test    string
	Line    int
	Message string
}

func Failures(report Report, file, dir string, testLine func(name string) (int, bool)) []Failure {
	return failures(platform.Native, report, file, dir, testLine)
}

func failures(f platform.Flavour, report Report, file, dir string, testLine func(name string) (int, bool)) []Failure {
	file = f.Fold(f.Clean(file))
	var out []Failure
	for _, r := range report.Results {
		if r.Status != Failed {
			continue
		}
		out = append(out, Failure{Test: r.Name, Line: failureLine(f, r, file, dir, testLine), Message: failureMessage(r)})
	}
	return out
}

func failureLine(f platform.Flavour, r Result, file, dir string, testLine func(string) (int, bool)) int {
	for _, frame := range r.Frames {
		if resolve(f, frame.File, dir) == file {
			return frame.Line
		}
	}
	if line, ok := testLine(r.Name); ok {
		return line
	}
	return 1
}

func resolve(f platform.Flavour, path, dir string) string {
	path = f.FromMSYS(path)
	if f.IsAbs(path) {
		return f.Fold(f.Clean(path))
	}
	return f.Fold(f.Join(dir, path))
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
