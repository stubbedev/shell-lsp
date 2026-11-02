//go:build unix

package nixeval

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunProgramReportsAMissingBinaryWithItsOwnType(t *testing.T) {
	t.Parallel()

	_, err := execRunner("shell-lsp-no-such-nix")(t.Context(), t.TempDir(), []string{"eval"})
	notFound, ok := errors.AsType[*NixNotFoundError](err)
	if !ok {
		t.Fatalf("error = %v, want *NixNotFoundError", err)
	}
	if !errors.Is(notFound, exec.ErrNotFound) {
		t.Errorf("error %v does not wrap the lookup failure", err)
	}
}

func TestRunProgramKeepsStdoutApartFromStderr(t *testing.T) {
	t.Parallel()

	got, err := runProgram(t.Context(), "sh", t.TempDir(), []string{"-c", "echo out; echo noise >&2"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "out\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestRunProgramWrapsStderrOnFailure(t *testing.T) {
	t.Parallel()

	_, err := runProgram(t.Context(), "sh", t.TempDir(), []string{"-c", "echo boom >&2; exit 3"})
	failure, ok := errors.AsType[*CommandError](err)
	if !ok {
		t.Fatalf("error = %v, want *CommandError", err)
	}
	if failure.Stderr != "boom" || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, stderr %q, want boom", err, failure.Stderr)
	}
	if _, isExit := errors.AsType[*exec.ExitError](err); !isExit {
		t.Errorf("error %v does not wrap the exit status", err)
	}
}

func TestRunProgramKillsTheProcessTreeOnDeadline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err := runProgram(ctx, "sh", dir, []string{"-c", "sleep 60 & echo $! > pid; wait"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the deadline", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d outlived the deadline", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHeadBufferStopsAtItsLimit(t *testing.T) {
	t.Parallel()

	overflowed := 0
	w := headBuffer{limit: 4, overflow: func() { overflowed++ }}
	for _, chunk := range []string{"abc", "def", "ghi"} {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
	}
	if !w.exceeded || overflowed != 1 || w.buf.Len() != 0 {
		t.Errorf("exceeded %v, overflowed %d times, kept %d bytes", w.exceeded, overflowed, w.buf.Len())
	}
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	t.Parallel()

	w := tailBuffer{limit: 4}
	for _, chunk := range []string{"abcdef", "ghijkl", "mnop", "qr\n"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
		if len(w.buf) > 2*w.limit {
			t.Fatalf("holds %d bytes over twice its limit", len(w.buf))
		}
	}
	if got := w.String(); got != "pqr" {
		t.Errorf("String() = %q, want pqr", got)
	}
}

func TestCommandNameOmitsArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{metadataArgs(), "nix flake metadata"},
		{prefetchArgs(), "nix flake prefetch"},
		{showArgs("path:/x"), "nix flake show"},
		{evalArgs("secret"), "nix eval"},
		{systemArgs(), "nix config show"},
	}
	for _, tt := range tests {
		if got := commandName(tt.args); got != tt.want {
			t.Errorf("commandName(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestPrefetchNeedsNoGlobalRegistryToFindTheWorkingDirectory(t *testing.T) {
	t.Parallel()

	if run := []string{"--option", "flake-registry", ""}; !containsRun(prefetchArgs(), run) {
		t.Errorf("%q lacks %q, so a machine that cannot reach the registry cannot resolve a local flake", prefetchArgs(), run)
	}
}
