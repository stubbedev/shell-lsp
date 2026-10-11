//go:build unix

package regularfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

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
