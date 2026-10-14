package lint

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var controlFamily = family{
	codes: map[Code]meta{
		2095: {severity: SeverityWarning, summary: "A command inside a while read loop reads stdin and may consume the loop's input."},
		2103: {severity: SeverityInfo, summary: "Changing back with cd after commands can leave the script in the wrong directory if a cd fails."},
		2104: {severity: SeverityError, summary: "break or continue in a function does not leave the function."},
		2105: {severity: SeverityError, summary: "break or continue is used outside a loop."},
		2106: {severity: SeverityWarning, summary: "break or continue in a subshell only exits the subshell, not the surrounding loop."},
		2119: {severity: SeverityInfo, bug: true, summary: "A function reads positional parameters, but this call passes none."},
		2120: {severity: SeverityWarning, summary: "A function reads positional parameters, but no call passes any arguments."},
		2129: {severity: SeverityStyle, summary: "Several commands in a row append to the same file, opening it each time."},
		2164: {severity: SeverityWarning, summary: "The commands after a failed cd run in the wrong directory."},
		2165: {severity: SeverityWarning, summary: "A nested loop reuses the outer loop's variable."},
		2167: {severity: SeverityWarning, summary: "A nested loop overwrites the outer loop's variable."},
		2188: {severity: SeverityWarning, summary: "A redirection stands alone without a command."},
		2189: {severity: SeverityError, summary: "A pipeline element is only a redirection, with no command to feed."},
	},
	nodes: []nodeRule{
		onNodes(checkStatementLists, &syntax.File{}, &syntax.Block{}, &syntax.Subshell{}, &syntax.IfClause{}, &syntax.WhileClause{}, &syntax.ForClause{}, &syntax.RepeatClause{}, &syntax.CaseItem{}, &syntax.CmdSubst{}, &syntax.ProcSubst{}),
		onNode(checkLoopJump),
		onNode(checkUncheckedDirectoryChange),
		onNode(checkReusedLoopVariable),
		onNode(checkEmptyRedirection),
		onNode(checkStdinConsumers),
	},
	finish: []func(*pass){checkFunctionArguments},
}

func checkStatementLists(p *pass, n syntax.Node) {
	for _, list := range stmtLists(n) {
		if !p.options().Errexit {
			p.checkDirectoryRevert(list)
		}
		p.checkRepeatedAppends(list)
	}
}

func (p *pass) checkDirectoryRevert(list []*syntax.Stmt) {
	var previous *syntax.CallExpr
	for _, stmt := range list {
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok {
			continue
		}
		if name, named := parser.CallName(call); !named || path.Base(name) != "cd" {
			continue
		}
		if previous != nil && !isDirectoryRevert(previous) && isDirectoryRevert(call) {
			if span, ok := p.span(call); ok {
				p.report(2103, span, "Run the commands in a subshell, ( cd dir && cmd ), instead of changing back afterwards; then a failed cd cannot leave you in the wrong directory.")
			}
			return
		}
		previous = call
	}
}

func isDirectoryRevert(call *syntax.CallExpr) bool {
	if len(call.Args) != 2 {
		return false
	}
	target, _ := parser.Literal(call.Args[1])
	return target == ".." || target == "-"
}

func (p *pass) checkRepeatedAppends(list []*syntax.Stmt) {
	run, first := 0, (*syntax.Stmt)(nil)
	var target string
	flush := func() {
		if run >= 3 {
			if span, ok := p.span(first); ok {
				p.report(2129, span, "Several commands in a row append to "+target+"; group them with { cmd1; cmd2; } >> "+target+" to open it once.")
			}
		}
	}
	for _, stmt := range list {
		last, file := p.appendTarget(stmt)
		switch {
		case file == "":
			flush()
			run = 0
		case run > 0 && file == target:
			run++
		default:
			flush()
			run, first, target = 1, last, file
		}
	}
	flush()
}

func (p *pass) appendTarget(stmt *syntax.Stmt) (*syntax.Stmt, string) {
	if list, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && !pipes(list.Op) {
		return nil, ""
	}
	stages := pipelineStages(stmt, nil)
	last := stages[len(stages)-1]
	for _, r := range last.Redirs {
		if r.Op != syntax.AppOut || r.Word == nil {
			continue
		}
		if span, ok := p.span(r.Word); ok {
			return last, p.spanText(span)
		}
	}
	return nil, ""
}

func checkLoopJump(p *pass, call *syntax.CallExpr) {
	command, ok := p.resolved(call)
	if !ok {
		return
	}
	name := command.name
	if name != "break" && name != "continue" {
		return
	}
	span, ok := p.span(call)
	if !ok {
		return
	}
	inFunction, subshell, looped := p.jumpScopes()
	switch {
	case !looped && inFunction:
		p.report(2104, span, "In a function, "+name+" does not leave the function; use return instead.")
	case !looped:
		p.report(2105, span, name+" is only valid inside a loop.")
	case subshell != notSubshell:
		p.report(2106, span, name+" only exits the subshell created by this "+subshell.label()+", not the loop around it.")
	}
}

func (p *pass) jumpScopes() (bool, subshellKind, bool) {
	inFunction, subshell, looped := false, notSubshell, false
	first, decided := true, false
	for depth := 0; p.ancestor(depth) != nil; depth++ {
		n := p.ancestor(depth)
		_, function := n.(*syntax.FuncDecl)
		loop := isLoop(n)
		kind := p.subshellKind(n, depth)
		if !function && !loop && kind == notSubshell {
			continue
		}
		if first {
			first, inFunction = false, function
		}
		if !function && !decided {
			decided, subshell = true, kind
		}
		looped = looped || loop
	}
	return inFunction, subshell, looped
}

func isLoop(n syntax.Node) bool {
	switch n.(type) {
	case *syntax.WhileClause, *syntax.ForClause, *syntax.RepeatClause:
		return true
	}
	return false
}

func (p *pass) subshellKind(n syntax.Node, depth int) subshellKind {
	switch n := n.(type) {
	case *syntax.CmdSubst:
		if n.Backquotes {
			return subshellBackticks
		}
		return subshellSubstitution
	case *syntax.Subshell:
		return subshellGroup
	case *syntax.TestDecl:
		return subshellBats
	case *syntax.CoprocClause:
		return subshellCoproc
	case *syntax.Stmt:
		if n.Background {
			return subshellBackground
		}
		if p.pipedStatement(depth) {
			return subshellPipeline
		}
	}
	return notSubshell
}

func (p *pass) pipedStatement(depth int) bool {
	stmt, _ := p.ancestor(depth).(*syntax.Stmt)
	pipe, ok := p.ancestor(depth + 1).(*syntax.BinaryCmd)
	if !ok || !pipes(pipe.Op) {
		return false
	}
	if pipe.X == stmt || !p.lastPipe() {
		return true
	}
	outer, ok := p.ancestor(depth + 3).(*syntax.BinaryCmd)
	return ok && pipes(outer.Op) && outer.X == p.ancestor(depth+2)
}

func (p *pass) lastPipe() bool {
	if p.dialect.Features().LastPipeInShell {
		return true
	}
	return p.options().Lastpipe
}

var safeDirectory = regexp.MustCompile(`^/*((\.|\.\.)/+)*(\.|\.\.)?$`)

func checkUncheckedDirectoryChange(p *pass, call *syntax.CallExpr) {
	command, ok := p.resolved(call)
	if !ok {
		return
	}
	name, args := command.name, command.args
	if name != "cd" && name != "pushd" && name != "popd" {
		return
	}
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok || p.options().Errexit || p.inCondition(stmt) {
		return
	}
	if name != "cd" && parseOptions(name, args).Has("-n") {
		return
	}
	if name != "popd" && len(args) == 1 {
		if target, ok := parser.Literal(args[0]); ok && safeDirectory.MatchString(target) {
			return
		}
	}
	if span, ok := p.span(call); ok {
		p.report(2164, span, "If "+name+" fails, the following commands run in the wrong directory; add || exit (or || return in a function).", p.leaveOnFailure(stmt)...)
	}
}

func (p *pass) leaveOnFailure(stmt *syntax.Stmt) []Fix {
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return nil
	}
	at := commandEnd(stmt)
	leave := func(keyword string) Fix {
		return reviewFix("Add || "+keyword, textedit.Edit{Start: at, End: at, NewText: " || " + keyword})
	}
	if p.bats || slices.ContainsFunc(p.stack, func(n syntax.Node) bool { _, ok := n.(*syntax.FuncDecl); return ok }) {
		return []Fix{leave("return"), leave("exit")}
	}
	return []Fix{leave("exit")}
}

func loopVariable(f *syntax.ForClause) (string, bool) {
	if f.Select {
		return "", false
	}
	switch loop := f.Loop.(type) {
	case *syntax.WordIter:
		if loop.Name != nil {
			return loop.Name.Value, true
		}
	case *syntax.CStyleLoop:
		if init, ok := loop.Init.(*syntax.BinaryArithm); ok && init.Op == syntax.Assgn {
			return parser.ArithmeticName(init.X)
		}
	}
	return "", false
}

func checkReusedLoopVariable(p *pass, inner *syntax.ForClause) {
	name, ok := loopVariable(inner)
	if !ok || name == "_" {
		return
	}
	for _, ancestor := range slices.Backward(p.stack) {
		outer, ok := ancestor.(*syntax.ForClause)
		if !ok {
			continue
		}
		if other, ok := loopVariable(outer); !ok || other != name {
			continue
		}
		innerSpan, ok := p.span(inner)
		if !ok {
			return
		}
		outerSpan, ok := p.span(outer)
		if !ok {
			return
		}
		p.report(2165, innerSpan, "This nested loop reuses the outer loop's variable "+name+"; give it its own name.")
		p.report(2167, outerSpan, "A nested loop overwrites this loop's variable "+name+"; give one of them another name.")
		return
	}
}

func checkEmptyRedirection(p *pass, stmt *syntax.Stmt) {
	if stmt.Cmd != nil || len(stmt.Redirs) == 0 {
		return
	}
	span, ok := p.span(stmt.Redirs[0])
	if !ok {
		return
	}
	if pipe, ok := p.parent().(*syntax.BinaryCmd); ok && pipes(pipe.Op) {
		p.report(2189, span, "This pipeline element is only a redirection; the pipe cannot feed a command that is not there. Attach the redirection to a command.")
		return
	}
	if cs, ok := p.parent().(*syntax.CmdSubst); ok && len(cs.Stmts) == 1 {
		return
	}
	at := parser.Offset(stmt.Pos())
	fix := safeFix("Redirect the output of :", textedit.Edit{Start: at, End: at, NewText: ": "})
	if p.dialect == dialect.Zsh || stmt.Negated {
		fix.Safety = Review
	}
	p.report(2188, span, "This redirection has no command; use : > file or true > file to make the intent explicit.", fix)
}

var stdinConsumers = map[string]string{"ssh": " -n", "ffmpeg": " -nostdin", "mplayer": " -noconsolecontrols", "HandBrakeCLI": " < /dev/null"}

func checkStdinConsumers(p *pass, loop *syntax.WhileClause) {
	if loop.Until || len(loop.Cond) != 1 || !readsStdin(loop.Cond[0]) {
		return
	}
	loopSpan, ok := p.span(loop)
	if !ok {
		return
	}
	for _, stmt := range loop.Do {
		p.findStdinConsumers(stmt, loopSpan)
	}
}

func readsStdin(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || hasStdinRedirect(stmt) {
		return false
	}
	if name, _ := parser.CallName(call); name != "read" {
		return false
	}
	return !slices.ContainsFunc(call.Args, func(w *syntax.Word) bool { return w.Lit() == "-u" })
}

func hasStdinRedirect(stmt *syntax.Stmt) bool {
	return slices.ContainsFunc(stmt.Redirs, func(r *syntax.Redirect) bool {
		if r.N != nil {
			return r.N.Value == "0"
		}
		switch r.Op {
		case syntax.RdrIn, syntax.DplIn, syntax.Hdoc, syntax.DashHdoc, syntax.HashHdoc, syntax.WordHdoc:
			return true
		}
		return false
	})
}

func (p *pass) findStdinConsumers(stmt *syntax.Stmt, loop parser.Span) {
	stmt = pipelineStages(stmt, nil)[0]
	p.substitutionConsumers(stmt, loop)
	if hasStdinRedirect(stmt) {
		return
	}
	if call, ok := stmt.Cmd.(*syntax.CallExpr); ok {
		p.stdinConsumer(call, loop)
		return
	}
	for _, inner := range nestedStatements(stmt.Cmd) {
		p.findStdinConsumers(inner, loop)
	}
}

func nestedStatements(cmd syntax.Command) []*syntax.Stmt {
	switch cmd := cmd.(type) {
	case *syntax.BinaryCmd:
		return []*syntax.Stmt{cmd.X, cmd.Y}
	case *syntax.IfClause:
		out := slices.Concat(cmd.Cond, cmd.Then)
		if cmd.Else != nil {
			out = append(out, nestedStatements(cmd.Else)...)
		}
		return out
	case *syntax.CaseClause:
		var out []*syntax.Stmt
		for _, item := range cmd.Items {
			out = append(out, item.Stmts...)
		}
		return out
	}
	return slices.Concat(stmtLists(cmd)...)
}

func (p *pass) substitutionConsumers(stmt *syntax.Stmt, loop parser.Span) {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return
	}
	visit := func(n syntax.Node) bool {
		if cs, ok := n.(*syntax.CmdSubst); ok {
			for _, inner := range cs.Stmts {
				p.findStdinConsumers(inner, loop)
			}
			return false
		}
		return true
	}
	for _, a := range call.Assigns {
		parser.Walk(a, visit)
	}
	for _, w := range call.Args {
		parser.Walk(w, visit)
	}
}

func (p *pass) stdinConsumer(call *syntax.CallExpr, loop parser.Span) {
	c, ok := commandOf(call)
	if !ok {
		return
	}
	name := path.Base(c.name)
	insert, known := stdinConsumers[name]
	if !known || c.builtin {
		return
	}
	switch name {
	case "ssh":
		if c.options().Has("-n") {
			return
		}
	case "ffmpeg", "mplayer":
		if slices.ContainsFunc(c.args, func(w *syntax.Word) bool { return w.Lit() == strings.TrimSpace(insert) }) {
			return
		}
	}
	span, ok := p.span(call)
	if !ok {
		return
	}
	at := parser.Offset(c.word.End())
	if name == "HandBrakeCLI" {
		at = span.End
	}
	p.reportWithSeverity(2095, SeverityInfo, loop, "This loop reads its input from stdin, and "+name+" inside it can swallow that input; see the command below.")
	p.report(2095, span, name+" reads stdin and may consume the input of the surrounding while read loop; add "+strings.TrimSpace(insert)+".", reviewFix("Add "+strings.TrimSpace(insert), textedit.Edit{Start: at, End: at, NewText: insert}))
}

func checkFunctionArguments(p *pass) {
	latest := map[string]parser.Function{}
	for _, fn := range p.tree.Functions {
		latest[fn.Name] = fn
	}
	calls := map[string][]parser.Command{}
	for _, cmd := range p.tree.Commands {
		if _, ok := latest[cmd.Name]; ok {
			calls[cmd.Name] = append(calls[cmd.Name], cmd)
		}
	}
	suppressed := sync.OnceValue(p.suppressions)
	for _, fn := range p.tree.Functions {
		uses := calls[fn.Name]
		if latest[fn.Name].Decl != fn.Decl || len(uses) == 0 || !readsPositionals(fn.Decl) {
			continue
		}
		if slices.ContainsFunc(uses, func(cmd parser.Command) bool { return len(cmd.Args) > 0 }) {
			continue
		}
		if p.silenced(suppressed(), Finding{Code: 2120, Span: fn.Span}) {
			continue
		}
		p.report(2120, fn.Span, fn.Name+" reads positional parameters, but no call passes any arguments.")
		for _, call := range uses {
			p.report(2119, call.Span, "Pass the script's arguments with "+fn.Name+" \"$@\" if "+fn.Name+" should see them; it reads positional parameters.")
		}
	}
}

func readsPositionals(fn *syntax.FuncDecl) bool {
	if fn == nil || fn.Body == nil {
		return false
	}
	reads, assigns := false, false
	parser.Walk(fn.Body, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.FuncDecl:
			return false
		case *syntax.ParamExp:
			reads = reads || readsPositional(n)
		case *syntax.CallExpr:
			assigns = assigns || setsPositionals(n)
		}
		return true
	})
	return reads && !assigns
}

func readsPositional(pe *syntax.ParamExp) bool {
	if pe.Param == nil || pe.Names != 0 || guarded(pe) || isAlternativeExpansion(pe) {
		return false
	}
	kind, _ := parser.ParamKindOf(pe.Param.Value)
	return kind == parser.ParamAll || kind == parser.ParamCount || kind == parser.ParamPositional
}

func setsPositionals(call *syntax.CallExpr) bool {
	if name, _ := parser.CallName(call); len(call.Args) < 2 || name != "set" {
		return false
	}
	return slices.ContainsFunc(call.Args[1:], func(w *syntax.Word) bool {
		lit, ok := parser.Literal(w)
		return !ok || lit == "--" || !strings.HasPrefix(lit, "-") && !strings.HasPrefix(lit, "+")
	})
}
