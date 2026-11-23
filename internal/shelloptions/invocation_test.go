package shelloptions

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestParseFlagsSetCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flags []string
		want  string
	}{
		{flags: []string{"-euo", "pipefail", "-c"}, want: "set -eu -o pipefail\n"},
		{flags: []string{"-e", "-o", "pipefail", "-c"}, want: "set -e -o pipefail\n"},
		{flags: []string{"-ec"}, want: "set -e\n"},
		{flags: []string{"-c"}, want: ""},
		{flags: nil, want: ""},
		{flags: []string{"--login", "-e", "-c"}, want: "set -e\n"},
		{flags: []string{"-o"}, want: ""},
		{flags: []string{"-c", "script", "-e"}, want: ""},
		{flags: []string{"-e", "--", "-u"}, want: "set -e\n"},
	}
	for _, tt := range tests {
		if got := ParseFlags(tt.flags).SetCommand(); got != tt.want {
			t.Errorf("ParseFlags(%q).SetCommand() = %q, want %q", tt.flags, got, tt.want)
		}
	}
}

func TestInvokeTakesTheOptionsOfTheInterpreter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		argv    []string
		dialect dialect.Dialect
		set     string
	}{
		{argv: []string{"/bin/bash", "-e"}, dialect: dialect.Bash, set: "set -e\n"},
		{argv: []string{"/usr/bin/env", "bash", "-e", "-c"}, dialect: dialect.Bash, set: "set -e\n"},
		{argv: []string{"/usr/bin/env", "-S", "bash", "-e"}, dialect: dialect.Bash, set: "set -e\n"},
		{argv: []string{"/usr/bin/env", "-S", "bash -euo pipefail", "-c"}, dialect: dialect.Bash, set: "set -eu -o pipefail\n"},
		{argv: []string{"/usr/bin/env", "-i", "PATH=/bin", "sh", "-c"}, dialect: dialect.Posix, set: ""},
	}
	for _, tt := range tests {
		got, ok := Invoke(tt.argv)
		if !ok {
			t.Errorf("Invoke(%q) found no interpreter", tt.argv)
			continue
		}
		if d, known := got.Dialect(); !known || d != tt.dialect {
			t.Errorf("Invoke(%q).Dialect() = %v, %v; want %v", tt.argv, d, known, tt.dialect)
		}
		if set := got.Flags.SetCommand(); set != tt.set {
			t.Errorf("Invoke(%q) sets %q, want %q", tt.argv, set, tt.set)
		}
	}
}

func TestPipefailComesFromTheNamedOption(t *testing.T) {
	t.Parallel()

	for words, want := range map[string]bool{
		"-o pipefail -c": true, "-euo pipefail -c": true, "-eo PIPE_FAIL": true,
		"-c": false, "-e -c": false, "-c -o pipefail": true, "-c script -o pipefail": false, "+o pipefail": false, "-o": false,
	} {
		if got := ParseFlags(strings.Fields(words)).Pipefail(); got != want {
			t.Errorf("ParseFlags(%q).Pipefail() = %v, want %v", words, got, want)
		}
	}
}

func TestEnvShebangFlagsSetErrexit(t *testing.T) {
	t.Parallel()

	for _, script := range []string{"#!/usr/bin/env -S bash -e\ncd /x\n", "#!/bin/bash -e\ncd /x\n"} {
		tree := parser.Parse(script, dialect.Bash)
		if !Scan(tree, nil).At(len(script) - 1).Errexit {
			t.Errorf("%q does not run with errexit", script)
		}
		if !StrictModeOf(tree).Errexit {
			t.Errorf("the strict mode of %q has no errexit", script)
		}
	}
}
