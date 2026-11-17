package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/corpus"
)

const (
	oursName      = "shell-lsp"
	nixparityName = "nixparity"
	shellcheck    = "shellcheck"
	nixfmt        = "nixfmt"
)

type binaries struct {
	ours, nixparity string
}

func measureAll(ctx context.Context, o runOptions) (report, error) {
	repos, err := o.Fetch(ctx)
	if err != nil {
		return report{}, err
	}
	nixpkgs, err := o.Find(o.nixpkgs)
	if err != nil {
		return report{}, err
	}
	m, err := describeMachine(o.runner)
	if err != nil {
		return report{}, err
	}
	tmp, err := os.MkdirTemp("", "perfreport-")
	if err != nil {
		return report{}, fmt.Errorf("creating a directory for the builds: %w", err)
	}
	defer os.RemoveAll(tmp) //nolint:errcheck
	bins, err := build(ctx, tmp)
	if err != nil {
		return report{}, err
	}
	version, err := oursVersion(ctx, bins.ours)
	if err != nil {
		return report{}, err
	}
	r := report{
		Version: version,
		Date:    time.Now().UTC().Format(time.DateOnly),
		Machine: m,
		Go:      runtime.Version(),
		Repeats: o.repeats,
	}
	nixpkgsDir := nixpkgs.Dir(o.Cache)
	if r.Keystrokes, err = measureKeystrokes(ctx, o.repeats); err != nil {
		return report{}, err
	}
	if r.Check, err = measureCheck(ctx, bins.ours, nixpkgs, nixpkgsDir, o.repeats); err != nil {
		return report{}, err
	}
	if r.Memory, err = measureMemory(ctx, bins, repos, o.Cache, nixpkgsDir, o.references, tmp); err != nil {
		return report{}, err
	}
	if r.Format, err = surveyFormat(ctx, nixpkgs, nixpkgsDir); err != nil {
		return report{}, err
	}
	return r, nil
}

func build(ctx context.Context, dir string) (binaries, error) {
	bins := binaries{ours: filepath.Join(dir, oursName), nixparity: filepath.Join(dir, nixparityName)}
	for _, target := range []struct{ out, pkg string }{{bins.ours, "."}, {bins.nixparity, "./cmd/nixparity"}} {
		cmd := exec.CommandContext(ctx, "go", "build", "-o", target.out, target.pkg) //nolint:gosec // G204: no shell is involved and every argument is a constant or a path under a directory from os.MkdirTemp
		if out, err := cmd.CombinedOutput(); err != nil {
			return binaries{}, fmt.Errorf("building %s: %w\n%s", target.pkg, err, out)
		}
	}
	return bins, nil
}

func oursVersion(ctx context.Context, bin string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("asking %s for its version: %w", oursName, err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimPrefix(line, oursName+" "), nil
}

func toolVersion(ctx context.Context, program string) (tool, error) {
	out, err := exec.CommandContext(ctx, program, "--version").Output()
	if err != nil {
		return tool{}, fmt.Errorf("asking %s for its version: %w", program, err)
	}
	version, err := parseToolVersion(program, string(out))
	if err != nil {
		return tool{}, err
	}
	return tool{Name: program, Version: version}, nil
}

func parseToolVersion(program, out string) (string, error) {
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 2 && fields[0] == "version:":
			return fields[1], nil
		case len(fields) == 2 && fields[0] == program:
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("%s --version printed no version: %q", program, out)
}

func median(values []int64) int64 {
	sorted := slices.Sorted(slices.Values(values))
	return sorted[len(sorted)/2]
}

func listFiles(root string, keep func(name string) bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() && keep(entry.Name()) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", root, err)
	}
	slices.Sort(out)
	return out, nil
}

func projectOf(r corpus.Repo) project {
	return project{Name: r.Name, Commit: r.Commit}
}

func runQuiet(cmd *exec.Cmd, accepted ...int) (timing, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start)
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || !slices.Contains(accepted, exit.ExitCode())) {
		return timing{}, fmt.Errorf("running %s: %w\n%s", cmd.Path, err, stderr.Bytes())
	}
	cpu := cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()
	return timing{WallMicros: wall.Microseconds(), CPUMicros: cpu.Microseconds()}, nil
}
