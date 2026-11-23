package dockerfile

import (
	"slices"
	"strings"
	"testing"
)

func projectedSetLines(text string) []string {
	m, _ := Project(text)
	lines := []string{}
	for line := range strings.SplitSeq(m.Text(), "\n") {
		if strings.HasPrefix(line, "set ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestShellFlagsBecomeOptionsOfEveryRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		want       []string
	}{
		{
			name:       "heredoc run under a strict bash shell",
			dockerfile: "FROM alpine:3\nSHELL [\"/bin/bash\", \"-euo\", \"pipefail\", \"-c\"]\nRUN <<EOF2\ncd /x\nls\ncd ..\nEOF2\n",
			want:       []string{"set -eu -o pipefail"},
		},
		{
			name:       "shell form run under separate flags",
			dockerfile: "FROM alpine:3\nSHELL [\"/bin/bash\", \"-e\", \"-u\", \"-o\", \"pipefail\", \"-c\"]\nRUN cd /x && ls\n",
			want:       []string{"set -e -u -o pipefail"},
		},
		{
			name:       "flags glued to -c",
			dockerfile: "FROM alpine:3\nSHELL [\"/bin/sh\", \"-ec\"]\nRUN cd /x\n",
			want:       []string{"set -e"},
		},
		{
			name:       "a shell without flags sets nothing",
			dockerfile: "FROM alpine:3\nSHELL [\"/bin/sh\", \"-c\"]\nRUN cd /x\n",
			want:       []string{},
		},
		{
			name:       "the default shell sets nothing",
			dockerfile: "FROM alpine:3\nRUN cd /x\n",
			want:       []string{},
		},
		{
			name:       "long options are skipped",
			dockerfile: "FROM alpine:3\nSHELL [\"/bin/bash\", \"--login\", \"-e\", \"-c\"]\nRUN cd /x\n",
			want:       []string{"set -e"},
		},
		{
			name:       "a child stage inherits the options of its parent",
			dockerfile: "FROM alpine:3 AS base\nSHELL [\"/bin/bash\", \"-eu\", \"-c\"]\nRUN cd /x\nFROM base\nRUN cd /y\n",
			want:       []string{"set -eu", "set -eu"},
		},
		{
			name:       "an unrelated stage keeps the default shell",
			dockerfile: "FROM alpine:3 AS base\nSHELL [\"/bin/bash\", \"-eu\", \"-c\"]\nRUN cd /x\nFROM alpine:3\nRUN cd /y\n",
			want:       []string{"set -eu"},
		},
		{
			name:       "a later shell replaces the earlier options",
			dockerfile: "FROM alpine:3\nSHELL [\"/bin/bash\", \"-e\", \"-c\"]\nRUN cd /x\nSHELL [\"/bin/bash\", \"-c\"]\nRUN cd /y\n",
			want:       []string{"set -e"},
		},
		{
			name:       "a heredoc shebang replaces the shell and its options",
			dockerfile: "FROM alpine:3\nSHELL [\"/bin/bash\", \"-euo\", \"pipefail\", \"-c\"]\nRUN <<EOF\n#!/bin/sh -e\ncd /x\nEOF\n",
			want:       []string{"set -e"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := projectedSetLines(tt.dockerfile); !slices.Equal(got, tt.want) {
				t.Errorf("set lines of %q = %q, want %q", tt.dockerfile, got, tt.want)
			}
		})
	}
}
