package lint

import (
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	codeGoogleShebang    = nativeBase + 4001
	codeGoogleFileHeader = nativeBase + 4002
	codeGoogleTodo       = nativeBase + 4003
	codeGoogleFuncHeader = nativeBase + 4004
	codeGoogleSections   = nativeBase + 4005
	codeGoogleLength     = nativeBase + 4006
	codeGoogleMain       = nativeBase + 4007
	mainFunction         = "main"
	mainCall             = `main "$@"`
	longScriptLines      = 100
	shortFunctionLines   = 10
	googleShebang        = "#!/bin/bash"
)

var googleFamily = family{
	codes: map[Code]meta{
		codeGoogleShebang:    {severity: SeverityStyle, optional: optionalGoogleShebang},
		codeGoogleFileHeader: {severity: SeverityStyle, optional: optionalGoogleFileHeader},
		codeGoogleTodo:       {severity: SeverityStyle, optional: optionalGoogleTodo},
		codeGoogleFuncHeader: {severity: SeverityStyle, optional: optionalGoogleFunctionComments},
		codeGoogleSections:   {severity: SeverityStyle, optional: optionalGoogleCommentSections},
		codeGoogleLength:     {severity: SeverityStyle, optional: optionalGoogleScriptLength},
		codeGoogleMain:       {severity: SeverityStyle, optional: optionalGoogleMain},
	},
	finish: []func(*pass){
		checkGoogleShebang, checkGoogleFileHeader, checkGoogleTodos, checkGoogleFunctionHeaders, checkGoogleCommentSections,
		checkGoogleScriptLength, checkGoogleMain,
	},
}

func checkGoogleShebang(p *pass) {
	if p.bats {
		return
	}
	first, _, _ := strings.Cut(p.text, "\n")
	first = strings.TrimSuffix(first, "\r")
	if !strings.HasPrefix(first, "#!") {
		return
	}
	shebang, _ := dialect.ParseShebang(first)
	interpreter, arguments := shebang.Interpreter, shebang.Arguments
	if interpreter == "/bin/bash" {
		return
	}
	span := parser.Span{Start: 0, End: len(first)}
	var fixes []Fix
	if len(arguments) == 0 && path.Base(interpreter) == "bash" || len(arguments) == 1 && path.Base(interpreter) == "env" && arguments[0] == "bash" {
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
		start, ok := todoMarker(body)
		if !ok || start > 0 && strings.ContainsRune("'\"`", rune(body[start-1])) || ownedTodo(body[start:]) {
			continue
		}
		offset := c.Span.Start + len("#") + start
		p.report(codeGoogleTodo, parser.Span{Start: offset, End: offset + len("TODO")}, "The Google Shell Style Guide writes TODO comments as TODO(owner): message, naming who can tell more about the work.")
	}
}

func todoMarker(body string) (int, bool) {
	lead := len(body) - len(strings.TrimLeftFunc(body, unicode.IsSpace))
	for start := 0; start < len(body); {
		if !parser.IsNameChar(body[start]) {
			start++
			continue
		}
		end := start + scan.While(body[start:], parser.IsNameChar)
		if word := body[start:end]; word == "TODO" || start == lead && strings.EqualFold(word, "todo") {
			return start, true
		}
		start = end
	}
	return 0, false
}

func ownedTodo(marker string) bool {
	owner, ok := strings.CutPrefix(marker, "TODO(")
	end := strings.IndexAny(owner, "()")
	first, _ := utf8.DecodeRuneInString(owner)
	return ok && end > 0 && owner[end] == ')' && !unicode.IsSpace(first)
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

var (
	argumentCommands    = setOf("shift", "getopts")
	positionalNames     = setOf("@", "*", "#")
	standardStreamFiles = setOf("/dev/stdout", "/dev/stderr")
)

type commentSection struct {
	name string
	used bool
}

func checkGoogleCommentSections(p *pass) {
	if p.bats || !p.active(codeGoogleSections) {
		return
	}
	globals := map[string]bool{}
	for _, def := range p.tree.Definitions {
		if !def.Local && def.Kind != parser.DefCommandEnv && !p.parameters.Has(def.Name) {
			globals[def.Name] = true
		}
	}
	for _, fn := range p.tree.Functions {
		if fn.Doc == "" || fn.Decl == nil {
			continue
		}
		var missing []string
		for _, section := range []commentSection{
			{name: "Globals", used: p.usesGlobals(fn, globals)},
			{name: "Arguments", used: usesArguments(fn)},
			{name: "Outputs", used: writesOutput(fn)},
			{name: "Returns", used: returnsValue(fn)},
		} {
			if section.used && !documents(fn.Doc, section.name) {
				missing = append(missing, section.name+":")
			}
		}
		if len(missing) == 0 {
			continue
		}
		noun := "section"
		if len(missing) > 1 {
			noun = "sections"
		}
		p.report(codeGoogleSections, fn.NameSpan, "The header comment of "+fn.Name+" has no "+strings.Join(missing, ", ")+" "+noun+"; the Google Shell Style Guide lists the globals a function uses, the arguments it takes, what it writes and the values it returns.")
	}
}

func documents(doc, section string) bool {
	for line := range strings.Lines(doc) {
		if strings.HasPrefix(strings.TrimSpace(line), section+":") {
			return true
		}
	}
	return false
}

func (p *pass) usesGlobals(fn parser.Function, globals map[string]bool) bool {
	global := func(name string) bool {
		_, local := p.tree.Binding(name, fn.Body)
		return globals[name] && !local
	}
	for _, ref := range p.tree.References {
		if ref.Within == fn.Body && global(ref.Name) {
			return true
		}
	}
	for _, def := range p.tree.Definitions {
		if def.Within == fn.Body && !def.Local && def.Kind != parser.DefCommandEnv && global(def.Name) {
			return true
		}
	}
	return false
}

func walkOwnBody(fn parser.Function, visit func(n syntax.Node) bool) {
	syntax.Walk(fn.Decl.Body, func(n syntax.Node) bool {
		if inner, ok := n.(*syntax.FuncDecl); ok && inner != fn.Decl {
			return false
		}
		return n == nil || visit(n)
	})
}

func usesArguments(fn parser.Function) bool {
	found := false
	walkOwnBody(fn, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.ParamExp:
			found = found || n.Param != nil && (positionalNames[n.Param.Value] || parser.IsDigits(n.Param.Value))
		case *syntax.CallExpr:
			name, _ := parser.CallName(n)
			found = found || argumentCommands[name]
		}
		return !found
	})
	return found
}

func returnsValue(fn parser.Function) bool {
	found := false
	walkOwnBody(fn, func(n syntax.Node) bool {
		if call, ok := n.(*syntax.CallExpr); ok {
			name, _ := parser.CallName(call)
			found = found || name == "return" && len(call.Args) > 1
		}
		return !found
	})
	return found
}

func writesOutput(fn parser.Function) bool {
	found := false
	walkOwnBody(fn, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			return false
		case *syntax.Stmt:
			call, ok := n.Cmd.(*syntax.CallExpr)
			if !ok {
				return true
			}
			name, _ := parser.CallName(call)
			found = found || outputCommands[name] && !assignsOutput(call) && !stdoutToFile(n.Redirs)
		}
		return !found
	})
	return found
}

func assignsOutput(call *syntax.CallExpr) bool {
	return len(call.Args) > 1 && parser.LiteralIs(call.Args[1], "-v")
}

func stdoutToFile(redirs []*syntax.Redirect) bool {
	for _, r := range redirs {
		switch r.Op {
		case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll, syntax.RdrClob:
			if (r.N == nil || r.N.Value == "1") && !literalIn(r.Word, standardStreamFiles) {
				return true
			}
		}
	}
	return false
}

func checkGoogleScriptLength(p *pass) {
	if p.bats || p.text == "" {
		return
	}
	lines := p.tree.Line(len(p.text) - 1)
	if lines <= longScriptLines {
		return
	}
	end := textline.End(p.text, 0)
	p.report(codeGoogleLength, parser.Span{Start: 0, End: end}, "This script is "+strconv.Itoa(lines)+" lines long; the Google Shell Style Guide asks for scripts over "+strconv.Itoa(longScriptLines)+" lines to be rewritten in a more structured language.")
}

func checkGoogleMain(p *pass) {
	if !p.active(codeGoogleMain) || !p.executedScript() || len(p.tree.File.Stmts) == 0 ||
		!slices.ContainsFunc(p.tree.Functions, func(fn parser.Function) bool { return fn.Name != mainFunction }) {
		return
	}
	last := p.tree.File.Stmts[len(p.tree.File.Stmts)-1]
	if _, hasMain := p.tree.FunctionNamed(mainFunction); !hasMain {
		p.reportNode(codeGoogleMain, last, "This script defines functions, so the Google Shell Style Guide wants its program in a function called main, run by "+mainCall+" as the last line.")
		return
	}
	if !callsMain(last) {
		p.reportNode(codeGoogleMain, last, "The Google Shell Style Guide ends a script that has a main function with the call "+mainCall+".")
	}
}

func callsMain(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || stmt.Negated || stmt.Background || len(stmt.Redirs) > 0 || len(call.Assigns) > 0 || len(call.Args) != 2 || !parser.LiteralIs(call.Args[0], mainFunction) {
		return false
	}
	dq, ok := parser.SolePart(call.Args[1]).(*syntax.ParamExp)
	quoted := len(call.Args[1].Parts) == 1 && isDoubleQuoted(call.Args[1].Parts[0])
	return ok && quoted && parser.IsPlainParam(dq) && dq.Param.Value == "@"
}
