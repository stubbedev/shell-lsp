package shelltest

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type Shell struct {
	Name      string
	Args      []string
	Version   string
	available func() bool
}

func shell(name, version string, args ...string) Shell {
	return Shell{Name: name, Args: args, Version: version, available: sync.OnceValue(func() bool {
		out, _ := exec.Command(name, "--version").CombinedOutput()
		return strings.Contains(string(out), version)
	})}
}

var (
	Bash = shell("bash", "version 5.3", "--norc", "--noprofile", "-c")
	Zsh  = shell("zsh", "zsh 5.9", "-f", "-c")
	Ksh  = shell("ksh", "93u+m", "-c")
)

func (s Shell) Require(t *testing.T) {
	t.Helper()
	if s.available() {
		return
	}
	message := s.Name + " with " + s.Version + " is not installed"
	if os.Getenv("REQUIRE_SHELLS") == "1" {
		t.Fatal(message)
	}
	t.Skip(message)
}

func (s Shell) Run(t *testing.T, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Name, append(s.Args, script)...) //nolint:gosec // the shells are fixed and the scripts are test cases
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %q: %v\n%s", s.Name, script, err, out)
	}
	return string(out)
}
