package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type HadolintFinding struct {
	Code   string `json:"code"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Level  string `json:"level"`
}

func Hadolint(ctx context.Context, dockerfile, config string, env []string) ([]HadolintFinding, error) {
	args := []string{"--no-fail", "-f", "json"}
	if config != "" {
		args = append(args, "--config", config)
	}
	cmd := exec.CommandContext(ctx, "hadolint", append(args, dockerfile)...) //nolint:gosec // G204: hadolint only reads the files it is given
	cmd.Dir = filepath.Dir(dockerfile)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running hadolint on %s: %w: %s", dockerfile, err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		return nil, fmt.Errorf("hadolint printed nothing for %s, which it does when it crashes: %s", dockerfile, stderr.String())
	}
	var out []HadolintFinding
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("reading what hadolint says about %s: %w", dockerfile, err)
	}
	return out, nil
}

func HadolintAccepts(ctx context.Context, config string, env []string) (bool, error) {
	dir, err := os.MkdirTemp("", "hadolint-config-")
	if err != nil {
		return false, fmt.Errorf("making a directory to judge %s in: %w", config, err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	dockerfile := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o600); err != nil {
		return false, fmt.Errorf("writing the Dockerfile to judge %s with: %w", config, err)
	}
	cmd := exec.CommandContext(ctx, "hadolint", "--no-fail", "-f", "json", "--config", config, dockerfile) //nolint:gosec // G204: hadolint only reads the files it is given
	cmd.Dir = dir
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("running hadolint with %s: %w: %s", config, err, stderr.String())
	}
	return stderr.Len() == 0, nil
}
