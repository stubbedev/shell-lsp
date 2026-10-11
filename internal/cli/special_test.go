//go:build unix

package cli

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCommandsFinishBesideSpecialConfigFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.sh": "#!/bin/sh\necho hi\n"})
	for _, name := range []string{".editorconfig", ".shellcheckrc"} {
		if err := syscall.Mkfifo(filepath.Join(dir, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"check", "a.sh"}, {"fmt", "-l", "a.sh"}} {
		done := make(chan result, 1)
		go func() { done <- run(t, dir, "", args...) }()
		select {
		case r := <-done:
			if r.status == 0 {
				t.Errorf("%v succeeded beside unreadable config files: %+v", args, r)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%v blocked on a FIFO config file", args)
		}
	}
}
