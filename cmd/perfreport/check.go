package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/stubbedev/shell-lsp/internal/corpus"
)

const findingsExit = 1

func shellScript(name string) bool {
	switch filepath.Ext(name) {
	case ".sh", ".bash":
		return true
	}
	return false
}

func measureCheck(ctx context.Context, ours string, repo corpus.Repo, dir string, repeats int) (checkTiming, error) {
	reference, err := toolVersion(ctx, shellcheck)
	if err != nil {
		return checkTiming{}, err
	}
	files, err := listFiles(dir, shellScript)
	if err != nil {
		return checkTiming{}, err
	}
	if len(files) == 0 {
		return checkTiming{}, fmt.Errorf("%s has no *.sh or *.bash files", repo.Name)
	}
	runs := []func() *exec.Cmd{
		func() *exec.Cmd { return exec.CommandContext(ctx, ours, append([]string{"check"}, files...)...) }, //nolint:gosec // G204: no shell is involved; the program was built by this command and the arguments are file paths
		func() *exec.Cmd { return exec.CommandContext(ctx, shellcheck, files...) },                         //nolint:gosec // G204: no shell is involved and the arguments are file paths
	}
	samples := make([][]timing, len(runs))
	for round := range repeats + 1 {
		for i, run := range runs {
			t, err := runQuiet(run(), findingsExit)
			if err != nil {
				return checkTiming{}, fmt.Errorf("checking the scripts of %s: %w", repo.Name, err)
			}
			if round > 0 {
				samples[i] = append(samples[i], t)
			}
		}
	}
	return checkTiming{
		Project:   projectOf(repo),
		Files:     len(files),
		Ours:      medianTiming(samples[0]),
		Reference: reference,
		Theirs:    medianTiming(samples[1]),
	}, nil
}

func medianTiming(samples []timing) timing {
	wall, cpu := make([]int64, len(samples)), make([]int64, len(samples))
	for i, s := range samples {
		wall[i], cpu[i] = s.WallMicros, s.CPUMicros
	}
	return timing{WallMicros: median(wall), CPUMicros: median(cpu)}
}
