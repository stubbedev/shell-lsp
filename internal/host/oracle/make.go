package oracle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	MakeMemoryKiB = 1 << 20
	MakeTimeout   = 60 * time.Second
)

func BoundedMake(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", append([]string{"-c", "ulimit -v " + strconv.Itoa(MakeMemoryKiB) + ` && exec make "$@"`, "make"}, args...)...) //nolint:gosec // G204: the arguments reach make as argv, never as shell text
}

func Exhausted(ctx context.Context, stderr string, err error) bool {
	var exit *exec.ExitError
	failed := errors.As(err, &exit)
	signaled := failed && exit.ExitCode() < 0
	silent := failed && exit.ExitCode() > 1 && strings.TrimSpace(stderr) == ""
	return ctx.Err() != nil || signaled || silent || strings.Contains(stderr, "memory exhausted") || strings.Contains(stderr, "Cannot allocate memory")
}

func MakeFailure(stderr, file string) (Verdict, bool) {
	name := regexp.QuoteMeta(file)
	fatal := regexp.MustCompile(`(?m)^` + name + `:(\d+): \*\*\* (.*?)\.  Stop\.$`)
	if m := fatal.FindStringSubmatch(stderr); m != nil {
		line, _ := strconv.Atoi(m[1])
		return Verdict{Line: line, Column: 1, Message: m[2]}, true
	}
	missing := regexp.MustCompile(`(?m)^` + name + `:(\d+): (\S+: No such file or directory)$`)
	if m := missing.FindStringSubmatch(stderr); m != nil {
		line, _ := strconv.Atoi(m[1])
		return Verdict{Line: line, Column: 1, Message: m[2]}, true
	}
	return Verdict{}, false
}

func callsError(text string, line int) bool {
	lines := strings.Split(text, "\n")
	return line >= 1 && line <= len(lines) && strings.Contains(lines[line-1], "$(error ")
}

const maxStubs = 8

var builtAfterReading = []string{"*** No rule to make target", "*** No targets."}

var missingInclude = regexp.MustCompile(`(?m)^\S+:\d+: (\S+): No such file or directory$`)

const stubbingMake = `printf '%%s\n' "$SHELL_LSP_STUBS" | while IFS= read -r f; do if [ -n "$f" ]; then mkdir -p -- "$(dirname -- "$f")" && : > "$f"; fi; done; ulimit -v ` + "%d" + ` && exec make "$@"`

func Make(ctx context.Context, makefile, root, text string, env []string) Verdict {
	ctx, cancel := context.WithTimeout(ctx, MakeTimeout)
	defer cancel()
	dir, base := filepath.Dir(makefile), filepath.Base(makefile)
	var stubs []string
	for {
		out, err := sandboxedMake(ctx, dir, base, root, stubs, env)
		if Exhausted(ctx, out, err) {
			return Verdict{Inconclusive: true, Message: fmt.Sprintf("make ran out of its %d MiB or %s", MakeMemoryKiB>>10, MakeTimeout)}
		}
		if m := missingInclude.FindStringSubmatch(out); m != nil {
			stub := m[1]
			if !filepath.IsAbs(stub) {
				stub = filepath.Join(dir, stub)
			}
			if len(stubs) == maxStubs || slices.Contains(stubs, stub) || !strings.HasPrefix(stub, root+string(filepath.Separator)) {
				return Verdict{Inconclusive: true, Message: "an included file is generated or missing: " + m[1]}
			}
			stubs = append(stubs, stub)
			continue
		}
		if v, ok := MakeFailure(out, base); ok {
			if callsError(text, v.Line) {
				return Verdict{Inconclusive: true, Message: "the makefile stops itself with $(error): " + v.Message}
			}
			return v
		}
		var exit *exec.ExitError
		if err == nil || errors.As(err, &exit) && exit.ExitCode() == 1 || slices.ContainsFunc(builtAfterReading, func(m string) bool { return strings.Contains(out, m) }) {
			return Verdict{Accepted: true}
		}
		message, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
		return Verdict{Inconclusive: true, Message: "make stopped outside this file: " + message}
	}
}

func sandboxedMake(ctx context.Context, dir, base, root string, stubs, env []string) (string, error) {
	sandbox := []string{"--ro-bind", "/", "/", "--overlay-src", root, "--tmp-overlay", root, "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "--unshare-all", "--die-with-parent", "--new-session", "--chdir", dir, "--", "sh", "-c", fmt.Sprintf(stubbingMake, MakeMemoryKiB), "make", "-pqr", "--no-print-directory", "-f", base}
	cmd := exec.CommandContext(ctx, "bwrap", sandbox...) //nolint:gosec // G204: paths reach the sandbox as argv and environment, never as shell text
	cmd.Env = append(slices.Clone(env), "SHELL_LSP_STUBS="+strings.Join(stubs, "\n"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}
