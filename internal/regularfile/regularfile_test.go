//go:build unix

package regularfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadReturnsRegularFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.sh")
	if err := os.WriteFile(path, []byte("echo hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, info, err := Read(path, 8)
	if err != nil || string(data) != "echo hi\n" || info.Size() != 8 {
		t.Errorf("Read = %q, %v, %v", data, info, err)
	}
}

func TestReadRejectsFilesOverTheLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "big.sh")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 9)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(path, 8); !errors.Is(err, ErrTooLarge) {
		t.Errorf("error = %v, want ErrTooLarge", err)
	}
}

func TestReadRejectsSpecialFilesWithoutReadingThem(t *testing.T) {
	t.Parallel()

	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fifo, "/dev/zero", "/dev/stdin", os.TempDir()} {
		done := make(chan error, 1)
		go func() {
			_, _, err := Read(path, 1<<20)
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrNotRegular) && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("Read(%s) error = %v, want ErrNotRegular", path, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Read(%s) blocked", path)
		}
	}
}
