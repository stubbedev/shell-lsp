package lint

import (
	"path"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const (
	codeGoogleShebang = nativeBase + 4001
	googleShebang     = "#!/bin/bash"
)

var googleFamily = family{
	codes: map[Code]meta{
		codeGoogleShebang: {severity: SeverityStyle, optional: optionalGoogleShebang},
	},
	finish: []func(*pass){checkGoogleShebang},
}

func checkGoogleShebang(p *pass) {
	if p.bats {
		return
	}
	first, _, _ := strings.Cut(p.text, "\n")
	first = strings.TrimSuffix(first, "\r")
	rest, ok := strings.CutPrefix(first, "#!")
	if !ok {
		return
	}
	fields := strings.Fields(rest)
	if len(fields) > 0 && fields[0] == "/bin/bash" {
		return
	}
	span := parser.Span{Start: 0, End: len(first)}
	var fixes []Fix
	if len(fields) == 1 && path.Base(fields[0]) == "bash" || len(fields) == 2 && path.Base(fields[0]) == "env" && fields[1] == "bash" {
		fixes = append(fixes, reviewFix("Use "+googleShebang, textedit.Edit{Start: span.Start, End: span.End, NewText: googleShebang}))
	}
	p.report(codeGoogleShebang, span, "The Google Shell Style Guide allows only bash for executables, started with "+googleShebang+"; this script starts with "+first+".", fixes...)
}
