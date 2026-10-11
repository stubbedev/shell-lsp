package regularfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const (
	pipeAccessInbound = 0x00000001
	pipeTypeByte      = 0x00000000
	pipeWait          = 0x00000000
)

func namedPipe(t *testing.T) string {
	t.Helper()

	name := fmt.Sprintf(`\\.\pipe\shell-lsp-regularfile-%d`, os.Getpid())
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	create := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateNamedPipeW")
	h, _, callErr := create.Call(uintptr(unsafe.Pointer(p)), pipeAccessInbound, pipeTypeByte|pipeWait, 1, 0, 4096, 0, 0)
	if syscall.Handle(h) == syscall.InvalidHandle {
		t.Fatalf("creating %s: %v", name, callErr)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(syscall.Handle(h)) })
	return name
}

func TestReadRefusesWindowsDevices(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	devices := []string{
		namedPipe(t), "NUL", "nul", "CON", "CONIN$", "COM1", "LPT1", "AUX", "PRN",
		filepath.Join(dir, "NUL"), filepath.Join(dir, "con.sh"), `\\.\C:`, `\\?\GLOBALROOT\Device\Null`,
	}
	for _, path := range devices {
		done := make(chan error, 1)
		go func() {
			_, _, err := Read(path, 1<<20)
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrNotRegular) {
				t.Errorf("Read(%s) error = %v, want ErrNotRegular", path, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Read(%s) blocked", path)
		}
	}
}

func TestReadAcceptsLongPathPrefixes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.sh")
	if err := os.WriteFile(path, []byte("echo hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, _, err := Read(`\\?\`+path, 1<<20)
	if err != nil || string(data) != "echo hi\n" {
		t.Errorf("Read(long path) = %q, %v", data, err)
	}
}
