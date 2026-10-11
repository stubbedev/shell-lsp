package lint

import (
	"regexp"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const byteOrderMark = "\xef\xbb\xbf"

var shebangFamily = family{
	codes: map[Code]meta{
		1082: {severity: SeverityError},
		1084: {severity: SeverityError},
		1104: {severity: SeverityError},
		1113: {severity: SeverityError},
		1114: {severity: SeverityError},
		1115: {severity: SeverityError},
		1128: {severity: SeverityError},
		2096: {severity: SeverityError},
		2148: {severity: SeverityError},
		2187: {severity: SeverityWarning},
		2239: {severity: SeverityError},
		2246: {severity: SeverityError},
	},
	rules: []rule{{finish: checkShebangShape}, {finish: checkShebang}},
}

var envSplitsArguments = regexp.MustCompile(`env +(-S|--split-string)`)

func checkShebang(p *pass) {
	first, _, _ := strings.Cut(p.text, "\n")
	line := parser.Span{Start: 0, End: len(first)}
	command := ""
	if shebang, ok := strings.CutPrefix(first, "#!"); ok {
		command = strings.TrimLeft(shebang, " \t")
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		if p.dialectGuessed {
			p.report(2148, line, "Nothing names the shell this script targets; add a shebang such as #!/bin/sh or #!/usr/bin/env bash, or a '# shellcheck shell=' directive.")
		}
		return
	}
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

func checkShebangShape(p *pass) {
	start := 0
	if strings.HasPrefix(p.text, byteOrderMark) {
		start = len(byteOrderMark)
	}
	if start > 0 || p.normalized.ByteOrderMark {
		p.report(1082, parser.Span{Start: 0, End: start}, "The file starts with a UTF-8 byte order mark, which makes the shell misread the first line; save the file without it.")
	}
	for first := true; start < len(p.text); first = false {
		end := strings.IndexByte(p.text[start:], '\n')
		if end < 0 {
			end = len(p.text)
		} else {
			end += start
		}
		line := p.text[start:end]
		if p.checkShebangLine(start, line, first) {
			if !first {
				p.report(1128, parser.Span{Start: start, End: end}, "The shebang only works on the very first line of the file; move it there and remove what comes before it.")
			}
			return
		}
		if trimmed := strings.TrimLeft(line, " \t\r"); trimmed != "" && trimmed[0] != '#' {
			return
		}
		start = end + 1
	}
}

func (p *pass) checkShebangLine(start int, line string, first bool) bool {
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	rest := line[indent:]
	switch {
	case strings.HasPrefix(rest, "#"):
		gap := len(rest) - 1 - len(strings.TrimLeft(rest[1:], " \t"))
		switch after := rest[1+gap:]; {
		case strings.HasPrefix(after, "!"):
			if indent > 0 {
				p.report(1114, parser.Span{Start: start, End: start + indent}, "Remove the whitespace before #!; the shebang must start at the beginning of the line.",
					reviewFix("Remove the whitespace before #!", textedit.Edit{Start: start, End: start + indent}))
			}
			if gap > 0 {
				p.report(1115, parser.Span{Start: start + indent + 1, End: start + indent + 1 + gap}, "Remove the whitespace between # and !; the shebang must start with #! exactly.",
					reviewFix("Remove the whitespace between # and !", textedit.Edit{Start: start + indent + 1, End: start + indent + 1 + gap}))
			}
			return true
		case first && indent == 0 && strings.HasPrefix(after, "/"):
			p.report(1113, parser.Span{Start: start + 1, End: start + 1 + gap}, "This looks like a shebang missing its !; write #!"+strings.TrimSpace(after)+".",
				reviewFix("Add the !", textedit.Edit{Start: start + 1, End: start + 1 + gap, NewText: "!"}))
			return true
		}
	case indent == 0 && strings.HasPrefix(rest, "!#"):
		p.report(1084, parser.Span{Start: start, End: start + 2}, "The shebang is written !# instead of #!; swap the two characters.",
			reviewFix("Swap !# to #!", textedit.Edit{Start: start, End: start + 2, NewText: "#!"}))
		return true
	case indent == 0 && strings.HasPrefix(rest, "!") && strings.HasPrefix(strings.TrimLeft(rest[1:], " \t"), "/"):
		p.report(1104, parser.Span{Start: start, End: start + 1}, "This looks like a shebang missing its #; start the line with #!.")
		return true
	}
	return false
}
