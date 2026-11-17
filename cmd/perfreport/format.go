package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/corpus"
	"github.com/stubbedev/shell-lsp/internal/host/nix/format"
	"github.com/stubbedev/shell-lsp/internal/parallel"
)

const (
	formatExamples = 20
	nixfmtTimeout  = time.Minute
	nixfmtRejected = 1
)

type formatVerdict uint8

const (
	formatAgree formatVerdict = iota
	formatBothRejected
	formatDiffer
)

type formatOutcome struct {
	verdict formatVerdict
	err     error
}

func nixFile(name string) bool { return filepath.Ext(name) == ".nix" }

func surveyFormat(ctx context.Context, repo corpus.Repo, dir string) (formatSurvey, error) {
	reference, err := toolVersion(ctx, nixfmt)
	if err != nil {
		return formatSurvey{}, err
	}
	files, err := listFiles(dir, nixFile)
	if err != nil {
		return formatSurvey{}, err
	}
	if len(files) == 0 {
		return formatSurvey{}, fmt.Errorf("%s has no .nix files", repo.Name)
	}
	outcomes := make([]formatOutcome, len(files))
	parallel.For(runtime.NumCPU(), len(files), func(i int) {
		outcomes[i] = compareFormat(ctx, files[i])
	})
	survey := formatSurvey{Project: projectOf(repo), Files: len(files), Reference: reference, Examples: []string{}}
	var errs []error
	for i, o := range outcomes {
		switch {
		case o.err != nil:
			errs = append(errs, o.err)
		case o.verdict == formatBothRejected:
			survey.BothRejected++
		case o.verdict == formatDiffer:
			survey.Differences++
			if len(survey.Examples) < formatExamples {
				rel, err := filepath.Rel(dir, files[i])
				if err != nil {
					return formatSurvey{}, fmt.Errorf("naming %s inside %s: %w", files[i], dir, err)
				}
				survey.Examples = append(survey.Examples, filepath.ToSlash(rel))
			}
		}
	}
	return survey, errors.Join(errs...)
}

func compareFormat(ctx context.Context, path string) formatOutcome {
	src, err := os.ReadFile(path) //nolint:gosec // G304: a file listed under the pinned checkout
	if err != nil {
		return formatOutcome{err: fmt.Errorf("reading %s: %w", path, err)}
	}
	theirs, theirsOK, err := referenceFormat(ctx, src)
	if err != nil {
		return formatOutcome{err: fmt.Errorf("formatting %s with %s: %w", path, nixfmt, err)}
	}
	ours, oursErr := format.Format(string(src))
	oursOK := oursErr == nil
	switch {
	case !oursOK && !theirsOK:
		return formatOutcome{verdict: formatBothRejected}
	case oursOK && theirsOK && ours == theirs:
		return formatOutcome{verdict: formatAgree}
	}
	return formatOutcome{verdict: formatDiffer}
}

func referenceFormat(ctx context.Context, src []byte) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, nixfmtTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, nixfmt, "-q", "-")
	cmd.Stdin = bytes.NewReader(src)
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), true, nil
	case ctx.Err() != nil:
		return "", false, fmt.Errorf("gave no answer within %s: %w", nixfmtTimeout, ctx.Err())
	case errors.As(err, &exit) && exit.ExitCode() == nixfmtRejected:
		return "", false, nil
	}
	return "", false, fmt.Errorf("%w: %s", err, stderr.String())
}
