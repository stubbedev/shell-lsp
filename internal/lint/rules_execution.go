package lint

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var executionFamily = family{
	codes: map[Code]meta{
		2032: {severity: SeverityInfo},
		2033: {severity: SeverityWarning},
		2083: {severity: SeverityError},
		2084: {severity: SeverityError},
		2087: {severity: SeverityWarning},
		2091: {severity: SeverityWarning},
		2092: {severity: SeverityWarning},
		2093: {severity: SeverityWarning},
		2116: {severity: SeverityStyle},
	},
	commands: []commandRule{
		{match: matchBase, names: []string{"ssh"}, check: checkRemoteHeredoc},
	},
	rules: []rule{
		{node: checkExpansionAsCommand},
		{node: checkSpacedDotSlash},
		{node: checkFunctionViaExternal},
		{node: checkExecNotLast},
		{node: checkEchoSubstitution},
	},
}

func checkExpansionAsCommand(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	last := len(call.Args) - 1
	word := call.Args[last]
	if len(word.Parts) != 1 || slices.ContainsFunc(call.Args[:last], func(w *syntax.Word) bool { return w.Lit() != "time" }) {
		return
	}
	part := word.Parts[0]
	span, ok := p.span(part)
	if !ok {
		return
	}
	switch part := part.(type) {
	case *syntax.CmdSubst:
		switch {
		case part.TempFile, part.ReplyVar:
		case part.Backquotes:
			p.report(2092, span, "The output of this backtick substitution is run as a command; drop the backticks to run the command itself, or capture its output in a variable.")
		default:
			p.report(2091, span, "The output of "+p.text[span.Start:span.End]+" is run as a command; drop the $( ) to run the command itself, or capture its output in a variable.")
		}
	case *syntax.ArithmExp:
		p.report(2084, span, "The result of "+p.text[span.Start:span.End]+" is run as a command; use (( )) to only evaluate it, or assign it to a variable.")
	}
}

func checkSpacedDotSlash(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 || call.Args[0].Lit() != "./" {
		return
	}
	if span, ok := p.span(call); ok {
		p.report(2083, span, "There is a space after ./, so this runs the current directory instead of "+p.nodeText(call.Args[1])+"; remove the space.")
	}
}

var externalRunners = setOf("chroot", "screen", "sudo", "doas", "run0", "xargs", "tmux")

func checkFunctionViaExternal(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	word := p.effectiveCommandWord(call)
	if word == nil {
		return
	}
	name, ok := parser.Literal(word)
	if !ok {
		return
	}
	runner := path.Base(name)
	args := call.Args[slices.Index(call.Args, word)+1:]
	candidate := runnerOperand(runner, args)
	if candidate == nil {
		return
	}
	target := candidate.Lit()
	if target == "" || strings.ContainsRune(target, '\\') {
		return
	}
	definition, ok := p.shellOnlyCommand(target)
	if !ok {
		return
	}
	at, ok := p.span(candidate)
	if !ok {
		return
	}
	line := strconv.Itoa(p.tree.Line(at.Start))
	p.report(2032, definition, target+" is defined by this script, so "+runner+" (used on line "+line+") cannot run it.")
	p.report(2033, at, runner+" runs external programs and cannot see the shell function or alias "+target+"; put it in a separate script or call it through sh -c.")
}

func runnerOperand(runner string, args []*syntax.Word) *syntax.Word {
	switch {
	case externalRunners[runner]:
		for _, a := range args {
			if lit, ok := parser.Literal(a); !ok || !strings.HasPrefix(lit, "-") {
				return a
			}
		}
	case runner == "ssh":
		i := 0
		for i < len(args) && strings.HasPrefix(args[i].Lit(), "-") {
			i++
		}
		if i+1 < len(args) {
			return args[i+1]
		}
	case runner == "find":
		for i, a := range args {
			if findExecActions[a.Lit()] && i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return nil
}

func (p *pass) shellOnlyCommand(name string) (parser.Span, bool) {
	if fn, ok := p.tree.FunctionNamed(name); ok {
		return fn.Span, true
	}
	if def, ok := p.alias(name); ok {
		return p.span(def.word)
	}
	return parser.Span{}, false
}

func checkRemoteHeredoc(p *pass, _ command) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return
	}
	for _, r := range stmt.Redirs {
		if (r.Op != syntax.Hdoc && r.Op != syntax.DashHdoc) || r.Hdoc == nil || !expandsLocally(r.Hdoc) {
			continue
		}
		delimiter, quoted := parser.HeredocDelimiter(r.Word)
		if quoted {
			continue
		}
		if span, ok := p.span(r.Word); ok {
			p.report(2087, span, "The here-document is expanded locally before ssh sends it; quote the delimiter ('"+delimiter+"') to expand on the remote side, or escape the expansions meant for it.")
		}
	}
}

func expandsLocally(body *syntax.Word) bool {
	return slices.ContainsFunc(body.Parts, func(part syntax.WordPart) bool {
		switch part.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp:
			return true
		}
		return false
	})
}

var cleanupCommands = setOf(":", "echo", "exit", "printf", "return")

func checkExecNotLast(p *pass, n syntax.Node) {
	var list []*syntax.Stmt
	inLoop := false
	switch n := n.(type) {
	case *syntax.File:
		list = n.Stmts
	case *syntax.Block:
		list = n.Stmts
	case *syntax.IfClause:
		list = n.Then
	case *syntax.WhileClause:
		list, inLoop = n.Do, true
	case *syntax.ForClause:
		list, inLoop = n.Do, true
	default:
		return
	}
	for len(list) > 0 && isCleanupStmt(list[len(list)-1]) {
		list = list[:len(list)-1]
	}
	if !inLoop && len(list) > 0 {
		list = list[:len(list)-1]
	}
	for _, stmt := range list {
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 || call.Args[0].Lit() != "exec" || p.honoursExecfail() {
			continue
		}
		if span, ok := p.span(call); ok {
			p.report(2093, span, "exec replaces the shell, so the commands after it only run if exec fails; remove exec, or handle that failure explicitly.")
		}
	}
}

func isCleanupStmt(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	return ok && (len(call.Args) == 0 || cleanupCommands[call.Args[0].Lit()])
}

func (p *pass) honoursExecfail() bool {
	return p.dialect == dialect.Bash && p.options().execfail
}

func checkEchoSubstitution(p *pass, n syntax.Node) {
	cs, ok := n.(*syntax.CmdSubst)
	if !ok || cs.TempFile || cs.ReplyVar || len(cs.Stmts) != 1 {
		return
	}
	stmt := cs.Stmts[0]
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || stmt.Negated || stmt.Background || stmt.Coprocess || len(call.Args) < 2 || call.Args[0].Lit() != "echo" {
		return
	}
	args := call.Args[1:]
	var first strings.Builder
	flattenLiteral(&first, args[0].Parts)
	if strings.HasPrefix(first.String(), "-") || slices.ContainsFunc(args, expandsToMany) {
		return
	}
	if len(args) == 1 && soleCommandOutput(args[0]) {
		return
	}
	if span, ok := p.span(cs); ok {
		p.report(2116, span, "This echo only passes its arguments through; use them directly instead of capturing echo's output.")
	}
}

func expandsToMany(w *syntax.Word) bool {
	return isGlob(w) || hasBraceExpansion(w)
}
