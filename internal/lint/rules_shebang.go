package lint

import (
	"regexp"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var shebangFamily = family{
	codes: map[Code]meta{
		2096: {severity: SeverityError},
		2148: {severity: SeverityError},
		2187: {severity: SeverityWarning},
		2239: {severity: SeverityError},
		2246: {severity: SeverityError},
	},
	rules: []rule{{finish: checkShebang}},
}

var envSplitsArguments = regexp.MustCompile(`env +(-S|--split-string)`)

func checkShebang(p *pass) {
	first, _, _ := strings.Cut(p.text, "\n")
	line := parser.Span{Start: 0, End: len(first)}
	command := ""
	if shebang, ok := strings.CutPrefix(first, "#!"); ok {
		command = strings.TrimLeft(shebang, " \t")
	}
	if command == "" {
		if p.dialectGuessed {
			p.report(2148, line, "Nothing names the shell this script targets; add a shebang such as #!/bin/sh or #!/usr/bin/env bash, or a '# shellcheck shell=' directive.")
		}
		return
	}
	fields := strings.Fields(command)
	if len(fields) > 2 && !envSplitsArguments.MatchString(command) {
		p.report(2096, line, "The kernel passes everything after "+fields[0]+" as one argument, so "+strings.Join(fields[1:], " ")+" arrives as a single word; use 'env -S' or set options in the script.")
	}
	if p.settings.shell {
		return
	}
	if strings.HasSuffix(fields[0], "/") {
		p.report(2246, line, "The interpreter "+fields[0]+" ends in a slash, so it names a directory rather than a shell.")
	}
	if !strings.HasPrefix(command, "/") {
		p.report(2239, line, "The interpreter "+fields[0]+" is not an absolute path, so the kernel cannot run this script directly; use e.g. #!/bin/sh or #!/usr/bin/env "+fields[0]+".")
	}
	if name, ok := dialect.Interpreter(p.text); ok && name == "ash" && !p.shellChosen {
		p.report(2187, line, "ash has no specification of its own, so this script is checked as dash; add '# shellcheck shell=dash' to say so explicitly.")
	}
}
