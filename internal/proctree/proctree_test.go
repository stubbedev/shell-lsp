//go:build unix || windows

package proctree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	treeRoleEnv    = "SHELL_LSP_PROCTREE_ROLE"
	treePidFileEnv = "SHELL_LSP_PROCTREE_PIDFILE"
	treeLifetime   = time.Minute
	treeDeadline   = 20 * time.Second
)

func TestProcessTreeHelper(t *testing.T) {
	switch os.Getenv(treeRoleEnv) {
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestProcessTreeHelper$")
		child.Env = append(os.Environ(), treeRoleEnv+"=child")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv(treePidFileEnv), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(treeLifetime)
	case "child":
		time.Sleep(treeLifetime)
	default:
		t.Skip("runs only as a process started by TestCancellingKillsTheWholeTree")
	}
}

func TestCancellingKillsTheWholeTree(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessTreeHelper$")
	cmd.Env = append(os.Environ(), treeRoleEnv+"=parent", treePidFileEnv+"="+pidFile)
	cmd.WaitDelay = WaitDelay
	release, err := Start(cmd)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer release()
	child := waitForPid(t, pidFile)
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Error("Wait() = nil after cancelling a process that sleeps for a minute")
	}
	if !processGone(child, treeDeadline) {
		t.Errorf("process %d, started by the cancelled process, is still running", child)
	}
}

func waitForPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(treeDeadline)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatalf("reading the child's pid: %v", err)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the parent process never wrote %s", path)
	return 0
}
