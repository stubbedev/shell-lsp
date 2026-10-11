package regularfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
