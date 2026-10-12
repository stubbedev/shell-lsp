package lint

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

const (
	codeGoogleShebang    = nativeBase + 4001
	codeGoogleFileHeader = nativeBase + 4002
	codeGoogleTodo       = nativeBase + 4003
	codeGoogleFuncHeader = nativeBase + 4004
	shortFunctionLines   = 10
	googleShebang        = "#!/bin/bash"
)

var googleFamily = family{
	codes: map[Code]meta{
		codeGoogleShebang:    {severity: SeverityStyle, optional: optionalGoogleShebang},
		codeGoogleFileHeader: {severity: SeverityStyle, optional: optionalGoogleFileHeader},
		codeGoogleTodo:       {severity: SeverityStyle, optional: optionalGoogleTodo},
		codeGoogleFuncHeader: {severity: SeverityStyle, optional: optionalGoogleFunctionComments},
	},
	finish: []func(*pass){checkGoogleShebang, checkGoogleFileHeader, checkGoogleTodos, checkGoogleFunctionHeaders},
}

var (
	todoMarker = regexp.MustCompile(`\bTODO\b|^\s*((?i:todo))\b`)
	ownedTodo  = regexp.MustCompile(`^TODO\([^()\s][^()]*\)`)
)

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

func checkGoogleFileHeader(p *pass) {
	if p.bats || len(p.tree.File.Stmts) == 0 {
		return
	}
	for start := 0; start < len(p.text); start = textline.Next(p.text, start) {
		end := textline.End(p.text, start)
		line := strings.TrimSpace(p.text[start:end])
		body, comment := strings.CutPrefix(line, "#")
		body = strings.TrimSpace(body)
		switch {
		case start == 0 && (strings.HasPrefix(line, "#!") || dialect.CompletionFile(p.text)):
		case line == "" || comment && (body == "" || strings.HasPrefix(body, "shellcheck ")):
		case comment:
			return
		default:
			p.report(codeGoogleFileHeader, parser.Span{Start: start, End: end}, "The Google Shell Style Guide asks every file to open with a comment that describes its contents; add one above this line.")
			return
		}
		if end == len(p.text) {
			return
		}
	}
}

func checkGoogleTodos(p *pass) {
	if p.bats {
		return
	}
	for _, c := range p.tree.Comments {
		text := p.text[c.Span.Start:c.Span.End]
		body, ok := strings.CutPrefix(text, "#")
		if !ok || strings.HasPrefix(strings.TrimSpace(body), "shellcheck ") {
			continue
		}
		at := todoMarker.FindStringSubmatchIndex(body)
		if at == nil {
			continue
		}
		start := at[0]
		if at[2] >= 0 {
			start = at[2]
		}
		if start > 0 && strings.ContainsRune("'\"`", rune(body[start-1])) || ownedTodo.MatchString(body[start:]) {
			continue
		}
		offset := c.Span.Start + len("#") + start
		p.report(codeGoogleTodo, parser.Span{Start: offset, End: offset + len("TODO")}, "The Google Shell Style Guide writes TODO comments as TODO(owner): message, naming who can tell more about the work.")
	}
}

func checkGoogleFunctionHeaders(p *pass) {
	if p.bats {
		return
	}
	_, executable := dialect.Interpreter(p.text)
	for _, fn := range p.tree.Functions {
		if fn.Doc != "" {
			continue
		}
		if !executable {
			p.report(codeGoogleFuncHeader, fn.NameSpan, "The Google Shell Style Guide asks for a header comment above every function of a library, describing what "+fn.Name+" does and how to call it.")
			continue
		}
		if lines := p.tree.Line(fn.Span.End-1) - p.tree.Line(fn.Span.Start) + 1; lines > shortFunctionLines {
			p.report(codeGoogleFuncHeader, fn.NameSpan, fn.Name+" is "+strconv.Itoa(lines)+" lines long, so the Google Shell Style Guide asks for a header comment above it describing what it does and how to call it.")
		}
	}
}
