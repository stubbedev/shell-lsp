package bats

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	maxLine   = 64 << 10
	maxStderr = 8 << 10
	waitDelay = 2 * time.Second
)

type Invocation struct {
	Executable string
	Dir        string
	File       string
	Test       *string
}

func Filter(name string) string {
	return "^" + regexp.QuoteMeta(name) + "$"
}

func (inv Invocation) Args() []string {
	args := []string{"--tap"}
	if inv.Test != nil {
		args = append(args, "--filter", Filter(*inv.Test))
	}
	return append(args, inv.File)
}

func (inv Invocation) String() string {
	return strings.Join(append([]string{inv.Executable}, inv.Args()...), " ")
}

func Run(ctx context.Context, inv Invocation, progress func(Report)) (Report, error) {
	var parser Parser
	stdout := &lineWriter{line: func(line string) {
		if parser.Line(line) {
			progress(parser.Report())
		}
	}}
	stderr := &cappedBuffer{limit: maxStderr}
	cmd := exec.CommandContext(ctx, inv.Executable, inv.Args()...) //nolint:gosec // G204: the executable is the project's own bats runner and every argument is passed as argv, never through a shell
	cmd.Dir = inv.Dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	isolate(cmd)
	err := cmd.Run()
	stdout.flush()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Report{}, fmt.Errorf("running %s: %w", inv, ctxErr)
	}
	report := parser.Report()
	if err == nil || errors.Is(err, exec.ErrWaitDelay) {
		return report, nil
	}
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return Report{}, fmt.Errorf("running %s: %w", inv, err)
	}
	if report.Empty() {
		return Report{}, fmt.Errorf("%s exited with status %d without reporting any test: %s", inv, exitErr.ExitCode(), strings.TrimSpace(stderr.String()))
	}
	return report, nil
}

type lineWriter struct {
	pending []byte
	line    func(string)
}

func (w *lineWriter) Write(b []byte) (int, error) {
	n := len(b)
	for {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			w.buffer(b)
			return n, nil
		}
		w.buffer(b[:i])
		w.emit()
		b = b[i+1:]
	}
}

func (w *lineWriter) buffer(b []byte) {
	room := max(maxLine-len(w.pending), 0)
	w.pending = append(w.pending, b[:min(len(b), room)]...)
}

func (w *lineWriter) emit() {
	w.line(string(w.pending))
	w.pending = w.pending[:0]
}

func (w *lineWriter) flush() {
	if len(w.pending) > 0 {
		w.emit()
	}
}

type cappedBuffer struct {
	buf       []byte
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := max(b.limit-len(b.buf), 0)
	if len(p) > room {
		b.truncated = true
	}
	b.buf = append(b.buf, p[:min(len(p), room)]...)
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	if b.truncated {
		return string(b.buf) + " [stderr truncated]"
	}
	return string(b.buf)
}
