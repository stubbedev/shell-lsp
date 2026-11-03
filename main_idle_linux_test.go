//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	idleWindow   = 3 * time.Second
	idleMaxTicks = 10
	exitPatience = 10 * time.Second
)

func cpuTicks(t *testing.T, pid int) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	fields := strings.Fields(text[strings.LastIndexByte(text, ')')+2:])
	user, err := strconv.Atoi(fields[11])
	if err != nil {
		t.Fatal(err)
	}
	system, err := strconv.Atoi(fields[12])
	if err != nil {
		t.Fatal(err)
	}
	return user + system
}

func frame(body string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func TestAnIdleServerIgnoresABusyWriterAndExitsWhenStdinCloses(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary and watches it for seconds")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "shell-lsp")
	if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	root := filepath.Join(dir, "root")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "a.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":"file://` + root + `","capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file://` + script + `","languageId":"shellscript","version":1,"text":"#!/bin/sh\necho hi\n"}}}`,
	} {
		if _, err := stdin.Write([]byte(frame(body))); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(idleWindow)

	before := cpuTicks(t, cmd.Process.Pid)
	logFile, err := os.Create(filepath.Join(root, "busy.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	for deadline := time.Now().Add(idleWindow); time.Now().Before(deadline); {
		if _, err := logFile.WriteString("a line of a busy log\n"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if spent := cpuTicks(t, cmd.Process.Pid) - before; spent > idleMaxTicks {
		t.Errorf("a server with no messages spent %d CPU ticks next to a busy writer, over %d", spent, idleMaxTicks)
	}

	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(exitPatience):
		_ = cmd.Process.Kill()
		t.Errorf("the server was still running %s after its stdin closed", exitPatience)
		<-exited
	}
}
