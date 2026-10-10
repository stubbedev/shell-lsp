package lint

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var controlFamily = family{
	codes: map[Code]meta{
		2095: {severity: SeverityWarning},
		2103: {severity: SeverityInfo},
		2104: {severity: SeverityError},
		2105: {severity: SeverityError},
		2106: {severity: SeverityWarning},
		2119: {severity: SeverityInfo},
		2120: {severity: SeverityWarning},
		2129: {severity: SeverityStyle},
		2164: {severity: SeverityWarning},
		2165: {severity: SeverityWarning},
		2167: {severity: SeverityWarning},
		2188: {severity: SeverityWarning},
		2189: {severity: SeverityError},
	},
	rules: []rule{
		{node: checkStatementLists},
		{node: checkLoopJump},
		{node: checkUncheckedDirectoryChange},
		{node: checkReusedLoopVariable},
		{node: checkEmptyRedirection},
		{node: checkStdinConsumers},
		{finish: checkFunctionArguments},
	},
}

func checkStatementLists(p *pass, n syntax.Node) {
	for _, list := range stmtLists(n) {
		if !p.options().errexit {
			p.checkDirectoryRevert(list)
		}
		p.checkRepeatedAppends(list)
	}
}

func (p *pass) checkDirectoryRevert(list []*syntax.Stmt) {
	var previous *syntax.CallExpr
	for _, stmt := range list {
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 || path.Base(call.Args[0].Lit()) != "cd" {
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
	if list, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && list.Op != syntax.Pipe && list.Op != syntax.PipeAll {
		return nil, ""
	}
	stages := pipelineStages(stmt, nil)
	last := stages[len(stages)-1]
	for _, r := range last.Redirs {
		if r.Op != syntax.AppOut || r.Word == nil {
			continue
		}
		if span, ok := p.span(r.Word); ok {
			return last, p.text[span.Start:span.End]
		}
	}
	return nil, ""
}

func checkLoopJump(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	word := p.effectiveCommandWord(call)
	if word == nil {
		return
	}
	name := word.Lit()
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
	case subshell != "":
		p.report(2106, span, name+" only exits the subshell created by this "+subshell+", not the loop around it.")
	}
}

func (p *pass) jumpScopes() (bool, string, bool) {
	inFunction, subshell, looped := false, "", false
	first, decided := true, false
	for depth := 0; p.ancestor(depth) != nil; depth++ {
		n := p.ancestor(depth)
		_, function := n.(*syntax.FuncDecl)
		loop := isLoop(n)
		kind := p.subshellKind(n, depth)
		if !function && !loop && kind == "" {
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
	case *syntax.WhileClause, *syntax.ForClause:
		return true
	}
	return false
}

func (p *pass) subshellKind(n syntax.Node, depth int) string {
	switch n := n.(type) {
	case *syntax.CmdSubst:
		if n.Backquotes {
			return "backtick expansion"
		}
		return "command substitution"
	case *syntax.Subshell:
		return "parenthesized group"
	case *syntax.TestDecl:
		return "bats test"
	case *syntax.CoprocClause:
		return "coprocess"
	case *syntax.Stmt:
		if n.Background {
			return "backgrounding"
		}
		if p.pipedStatement(depth) {
			return "pipeline"
		}
	}
	return ""
}

func (p *pass) pipedStatement(depth int) bool {
	stmt, _ := p.ancestor(depth).(*syntax.Stmt)
	pipe, ok := p.ancestor(depth + 1).(*syntax.BinaryCmd)
	if !ok || (pipe.Op != syntax.Pipe && pipe.Op != syntax.PipeAll) {
		return false
	}
	if pipe.X == stmt || !p.lastPipe() {
		return true
	}
	outer, ok := p.ancestor(depth + 3).(*syntax.BinaryCmd)
	return ok && (outer.Op == syntax.Pipe || outer.Op == syntax.PipeAll) && outer.X == p.ancestor(depth+2)
}

func (p *pass) lastPipe() bool {
	if p.dialect.Features().LastPipeInShell {
		return true
	}
	return slices.ContainsFunc(p.tree.Commands, func(cmd parser.Command) bool {
		return (cmd.Name == "shopt" || cmd.Name == "set") && mentions(cmd.Args, "lastpipe")
	})
}

var safeDirectory = regexp.MustCompile(`^/*((\.|\.\.)/+)*(\.|\.\.)?$`)

func checkUncheckedDirectoryChange(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	word := p.effectiveCommandWord(call)
	if word == nil {
		return
	}
	name := word.Lit()
	if name != "cd" && name != "pushd" && name != "popd" {
		return
	}
	args := call.Args[slices.Index(call.Args, word)+1:]
	if stmt, ok := p.parent().(*syntax.Stmt); !ok || p.options().errexit || p.inCondition(stmt) {
		return
	}
	if name != "cd" && parseFlags(args, "").has('n', "") {
		return
	}
	if name != "popd" && len(args) == 1 {
		if target, ok := parser.Literal(args[0]); ok && safeDirectory.MatchString(target) {
			return
		}
	}
	if span, ok := p.span(call); ok {
		p.report(2164, span, "If "+name+" fails, the following commands run in the wrong directory; add || exit (or || return in a function).")
	}
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

func checkReusedLoopVariable(p *pass, n syntax.Node) {
	inner, ok := n.(*syntax.ForClause)
	if !ok {
		return
	}
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

func checkEmptyRedirection(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || stmt.Cmd != nil || len(stmt.Redirs) == 0 {
		return
	}
	span, ok := p.span(stmt.Redirs[0])
	if !ok {
		return
	}
	if pipe, ok := p.parent().(*syntax.BinaryCmd); ok && (pipe.Op == syntax.Pipe || pipe.Op == syntax.PipeAll) {
		p.report(2189, span, "This pipeline element is only a redirection; the pipe cannot feed a command that is not there. Attach the redirection to a command.")
		return
	}
	if cs, ok := p.parent().(*syntax.CmdSubst); ok && len(cs.Stmts) == 1 {
		return
	}
	p.report(2188, span, "This redirection has no command; use : > file or true > file to make the intent explicit.")
}

var stdinConsumers = map[string]string{"ssh": " -n", "ffmpeg": " -nostdin", "mplayer": " -noconsolecontrols", "HandBrakeCLI": " < /dev/null"}

func checkStdinConsumers(p *pass, n syntax.Node) {
	loop, ok := n.(*syntax.WhileClause)
	if !ok || loop.Until || len(loop.Cond) != 1 || !readsStdin(loop.Cond[0]) {
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
	if !ok || len(call.Args) == 0 || call.Args[0].Lit() != "read" || hasStdinRedirect(stmt) {
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
		case syntax.RdrIn, syntax.DplIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
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
		syntax.Walk(a, visit)
	}
	for _, w := range call.Args {
		syntax.Walk(w, visit)
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
		if parseFlags(c.args, "bcDEeFIiJLlmOopQRSWw").has('n', "") {
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
	p.report(2095, span, name+" reads stdin and may consume the input of the surrounding while read loop; add "+strings.TrimSpace(insert)+".", textedit.Edit{Start: at, End: at, NewText: insert})
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
		if suppressed().covers(Finding{Code: 2120, Span: fn.Span}) {
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
	syntax.Walk(fn.Body, func(n syntax.Node) bool {
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
	name := pe.Param.Value
	return name == "*" || name == "@" || name == "#" || (isPositional(name) && name != "0")
}

func setsPositionals(call *syntax.CallExpr) bool {
	if len(call.Args) < 2 || call.Args[0].Lit() != "set" {
		return false
	}
	return slices.ContainsFunc(call.Args[1:], func(w *syntax.Word) bool {
		lit, ok := parser.Literal(w)
		return !ok || lit == "--" || !strings.HasPrefix(lit, "-") && !strings.HasPrefix(lit, "+")
	})
}
