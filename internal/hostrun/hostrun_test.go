//go:build unix

package hostrun

import (
	"context"
	"strings"
	"testing"
)

func TestRunReportsOutputAndStatus(t *testing.T) {
	t.Parallel()

	got, err := Run(t.Context(), []string{"sh", "-c", "echo out; echo err >&2; exit 3"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != 3 || !strings.Contains(got.Output, "out\n") || !strings.Contains(got.Output, "err\n") {
		t.Errorf("Run() = %+v, want status 3 with both streams", got)
	}
}

func TestRunCapsOutput(t *testing.T) {
	t.Parallel()

	got, err := Run(t.Context(), []string{"sh", "-c", "head -c 200000 /dev/zero"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Output) != maxOutput {
		t.Errorf("Run() kept %d bytes, truncated %v; want %d and true", len(got.Output), got.Truncated, maxOutput)
	}
}

func TestRunNamesAMissingProgram(t *testing.T) {
	t.Parallel()

	if _, err := Run(t.Context(), []string{"shell-lsp-no-such-program"}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "shell-lsp-no-such-program") {
		t.Errorf("Run() error = %v, want one naming the program", err)
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Run(ctx, []string{"sleep", "30"}, t.TempDir()); err == nil {
		t.Error("Run() = nil error after cancelling")
	}
}
