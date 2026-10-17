package hostrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/proctree"
)

const maxOutput = 64 << 10

type Result struct {
	Code      int
	Output    string
	Truncated bool
}

type capped struct {
	buf       bytes.Buffer
	truncated bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := maxOutput - c.buf.Len()
	if len(p) > room {
		c.truncated = true
		c.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func Run(ctx context.Context, argv []string, dir string) (Result, error) {
	if len(argv) == 0 {
		panic("hostrun: nothing to run")
	}
	program, err := exec.LookPath(argv[0])
	if err != nil {
		return Result{}, fmt.Errorf("running %s: %w", strings.Join(argv, " "), err)
	}
	var out capped
	cmd := exec.CommandContext(ctx, program, argv[1:]...) //nolint:gosec // G204: the program is the host language's own tool and every argument is passed as argv, never through a shell
	cmd.Dir = dir
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = proctree.WaitDelay
	release, err := proctree.Start(cmd)
	if err != nil {
		return Result{}, fmt.Errorf("running %s: %w", strings.Join(argv, " "), err)
	}
	err = cmd.Wait()
	release()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Result{}, fmt.Errorf("running %s: %w", strings.Join(argv, " "), ctxErr)
	}
	result := Result{Output: out.buf.String(), Truncated: out.truncated}
	if err == nil || errors.Is(err, exec.ErrWaitDelay) {
		return result, nil
	}
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return Result{}, fmt.Errorf("running %s: %w", strings.Join(argv, " "), err)
	}
	result.Code = exitErr.ExitCode()
	return result, nil
}
