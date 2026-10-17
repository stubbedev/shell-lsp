package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

type DockerfileHeredoc struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Expand  bool   `json:"expand"`
	Chomp   bool   `json:"chomp"`
}

type DockerfileInstruction struct {
	Name     string                 `json:"name"`
	Start    int                    `json:"start"`
	End      int                    `json:"end"`
	Flags    []string               `json:"flags"`
	Args     []string               `json:"args"`
	JSON     bool                   `json:"json"`
	Heredocs []DockerfileHeredoc    `json:"heredocs"`
	Inner    *DockerfileInstruction `json:"inner,omitempty"`
}

type DockerfileFinding struct {
	Rule    string `json:"rule"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type DockerfileReport struct {
	Error        string                  `json:"error,omitempty"`
	Line         int                     `json:"line,omitempty"`
	Escape       string                  `json:"escape"`
	Instructions []DockerfileInstruction `json:"instructions"`
	Warnings     []DockerfileFinding     `json:"warnings"`
	Lint         []DockerfileFinding     `json:"lint"`
}

func Dockerfile(ctx context.Context, path string, env []string) (DockerfileReport, error) {
	cmd := exec.CommandContext(ctx, "dockerfile-oracle", path) //nolint:gosec // G204: the oracle only reads the file it is given
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return DockerfileReport{}, fmt.Errorf("running dockerfile-oracle on %s: %w: %s", path, err, stderr.String())
	}
	var r DockerfileReport
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		return DockerfileReport{}, fmt.Errorf("reading what dockerfile-oracle says about %s: %w", path, err)
	}
	return r, nil
}

func (r DockerfileReport) Verdict() Verdict {
	if r.Error == "" {
		return Verdict{Accepted: true}
	}
	return Verdict{Line: r.Line, Column: 1, Message: r.Error}
}

func (r DockerfileReport) Checks() []DockerfileFinding {
	var out []DockerfileFinding
	for _, l := range append(slices.Clone(r.Warnings), r.Lint...) {
		if l.Rule != "" && !strings.Contains(l.Rule, "://") {
			out = append(out, l)
		}
	}
	return out
}
