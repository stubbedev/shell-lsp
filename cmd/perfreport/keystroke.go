package main

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

const (
	keystrokeBenchmark = "BenchmarkKeystroke"
	keystrokePackage   = "./internal/server"
)

func measureKeystrokes(ctx context.Context, repeats int) ([]keystroke, error) {
	cmd := exec.CommandContext(ctx, "go", "test", "-run", "^$", "-bench", "^"+keystrokeBenchmark+"$", "-count", strconv.Itoa(repeats), keystrokePackage) //nolint:gosec // G204: no shell is involved and the arguments are constants and a number
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("running %s in %s: %w\n%s", keystrokeBenchmark, keystrokePackage, err, out)
	}
	return parseKeystrokes(string(out), repeats)
}

func parseKeystrokes(out string, repeats int) ([]keystroke, error) {
	samples := map[string][]int64{}
	var order []string
	for line := range strings.Lines(out) {
		name, nanos, ok, err := parseBenchmarkLine(line)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if _, seen := samples[name]; !seen {
			order = append(order, name)
		}
		samples[name] = append(samples[name], nanos)
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("%s printed no results:\n%s", keystrokeBenchmark, out)
	}
	results := make([]keystroke, 0, len(order))
	for _, name := range order {
		if got := len(samples[name]); got != repeats {
			return nil, fmt.Errorf("%s/%s ran %d times, want %d", keystrokeBenchmark, name, got, repeats)
		}
		results = append(results, keystroke{Name: name, MedianNanos: median(samples[name])})
	}
	return results, nil
}

func parseBenchmarkLine(line string) (string, int64, bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[3] != "ns/op" {
		return "", 0, false, nil
	}
	name, ok := strings.CutPrefix(fields[0], keystrokeBenchmark+"/")
	if !ok {
		return "", 0, false, nil
	}
	if i := strings.LastIndexByte(name, '-'); i >= 0 && digits(name[i+1:]) {
		name = name[:i]
	}
	nanos, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return "", 0, false, fmt.Errorf("reading the time of %q: %w", line, err)
	}
	return name, int64(nanos), true, nil
}

func digits(s string) bool {
	return s != "" && !slices.ContainsFunc([]byte(s), func(c byte) bool { return c < '0' || c > '9' })
}
