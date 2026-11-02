package nixeval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/proctree"
)

const (
	nixProgram      = "nix"
	experimental    = "nix-command flakes"
	maxStdout       = 128 << 20
	maxStderr       = 64 << 10
	commandNameArgs = 2
)

type NixNotFoundError struct {
	Err error
}

func (e *NixNotFoundError) Error() string {
	return fmt.Sprintf("the nix binary is not available: %v", e.Err)
}

func (e *NixNotFoundError) Unwrap() error {
	return e.Err
}

type CommandError struct {
	Command string
	Stderr  string
	Err     error
}

func (e *CommandError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s: %v", e.Command, e.Err)
	}
	return fmt.Sprintf("%s: %v: %s", e.Command, e.Err, e.Stderr)
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

type runner func(ctx context.Context, dir string, args []string) ([]byte, error)

func hardened(command ...string) []string {
	return append(command,
		"--quiet",
		"--extra-experimental-features", experimental,
		"--option", "pure-eval", "true",
		"--option", "allow-import-from-derivation", "false",
		"--option", "accept-flake-config", "false",
		"--option", "allow-unsafe-native-code-during-evaluation", "false",
		"--no-update-lock-file",
	)
}

func metadataArgs() []string {
	return hardened("flake", "metadata", ".", "--json")
}

func showArgs(ref string) []string {
	return hardened("flake", "show", ref, "--json")
}

func evalArgs(expr string) []string {
	return hardened("eval", "--json", "--expr", expr)
}

func systemArgs() []string {
	return []string{"config", "show", "system", "--quiet", "--extra-experimental-features", experimental}
}

func commandName(args []string) string {
	words := []string{nixProgram}
	for _, arg := range args[:min(commandNameArgs, len(args))] {
		if strings.HasPrefix(arg, "-") {
			break
		}
		words = append(words, arg)
	}
	return strings.Join(words, " ")
}

type headBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow func()
	exceeded bool
}

func (w *headBuffer) Write(p []byte) (int, error) {
	if w.exceeded {
		return len(p), nil
	}
	if w.buf.Len()+len(p) > w.limit {
		w.exceeded = true
		w.buf.Reset()
		w.overflow()
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

type tailBuffer struct {
	buf   []byte
	limit int
}

func (w *tailBuffer) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if len(w.buf) > 2*w.limit {
		w.buf = append(w.buf[:0], w.buf[len(w.buf)-w.limit:]...)
	}
	return len(p), nil
}

func (w *tailBuffer) String() string {
	return strings.TrimSpace(string(w.buf[max(len(w.buf)-w.limit, 0):]))
}

func execRunner(program string) runner {
	return func(ctx context.Context, dir string, args []string) ([]byte, error) {
		return runProgram(ctx, program, dir, args)
	}
}

func lookProgram(program string) (string, error) {
	path, err := exec.LookPath(program)
	if errors.Is(err, exec.ErrNotFound) {
		return "", &NixNotFoundError{Err: err}
	}
	return path, err
}

func nixIdentity(program string) func() (string, error) {
	return func() (string, error) {
		path, err := lookProgram(program)
		if err != nil {
			return "", fmt.Errorf("finding %s: %w", program, err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", path, err)
		}
		return resolved, nil
	}
}

func runProgram(ctx context.Context, program, dir string, args []string) ([]byte, error) {
	name := commandName(args)
	path, err := lookProgram(program)
	if _, missing := errors.AsType[*NixNotFoundError](err); missing {
		return nil, err
	}
	if err != nil {
		return nil, &CommandError{Command: name, Err: err}
	}
	killable, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := headBuffer{limit: maxStdout, overflow: cancel}
	stderr := tailBuffer{limit: maxStderr}
	cmd := exec.CommandContext(killable, path, args...) //nolint:gosec // G204: the program is nix found on PATH and every argument is passed as argv, never through a shell
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = proctree.WaitDelay
	release, err := proctree.Start(cmd)
	if err != nil {
		return nil, &CommandError{Command: name, Err: err}
	}
	err = cmd.Wait()
	release()
	switch {
	case stdout.exceeded:
		return nil, &CommandError{Command: name, Stderr: stderr.String(), Err: fmt.Errorf("output exceeded %d bytes", maxStdout)}
	case ctx.Err() != nil:
		return nil, &CommandError{Command: name, Stderr: stderr.String(), Err: ctx.Err()}
	case err == nil, errors.Is(err, exec.ErrWaitDelay):
		return stdout.buf.Bytes(), nil
	}
	return nil, &CommandError{Command: name, Stderr: stderr.String(), Err: err}
}
