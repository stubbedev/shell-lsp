package lint

import (
	"path"
	"slices"
	"strconv"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var redirectsFamily = family{
	codes: map[Code]meta{
		2210: {severity: SeverityWarning, summary: "A redirection to a number writes to or reads from a file with that name."},
		2216: {severity: SeverityWarning, summary: "A command that does not read standard input is piped data."},
		2217: {severity: SeverityWarning, summary: "A command that does not read standard input is given redirected input."},
		2238: {severity: SeverityWarning, summary: "A redirection names a file that is also a command, which suggests a pipe was meant."},
		2257: {severity: SeverityWarning, summary: "Arithmetic that modifies a variable in a redirection may run in a subshell or not at all."},
		2259: {severity: SeverityError, summary: "An input redirection replaces the piped input, discarding the previous command's output."},
		2260: {severity: SeverityError, summary: "An output redirection takes the output away from the pipe, so the next command reads nothing."},
		2261: {severity: SeverityError, summary: "Several redirections compete for the same file descriptor, and only the last takes effect."},
		2327: {severity: SeverityWarning, summary: "A command substitution is always empty because the command's output is redirected elsewhere."},
		2328: {severity: SeverityError, summary: "A redirection takes the output away from the command substitution it is in."},
	},
	nodes: []nodeRule{
		onNode(checkRedirectTargets),
		onNode(checkPipedRedirections),
		onNode(checkCompetingRedirections),
		onNode(checkRedirectedInput),
		onNode(checkDiscardedSubstitution),
		onNode(checkRedirectSideEffects),
	},
}

var nonReadingCommands = setOf(
	"alias", "basename", "bg", "cal", "cd", "chgrp", "chmod", "chown", "cp", "du", "echo", "export", "fg", "fuser",
	"getconf", "getopt", "getopts", "ipcrm", "ipcs", "jobs", "kill", "ln", "ls", "locale", "mv", "printf", "ps", "pwd",
	"readlink", "realpath", "renice", "rm", "rmdir", "set", "sleep", "touch", "trap", "ulimit", "unalias", "uname",
)

func isFileRedirect(op syntax.RedirOperator) bool {
	switch op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrIn, syntax.RdrInOut, syntax.RdrClob, syntax.AppClob,
		syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
		return true
	}
	return false
}

func checkRedirectTargets(p *pass, r *syntax.Redirect) {
	if r.Word == nil || !isFileRedirect(r.Op) || len(r.Word.Parts) != 1 {
		return
	}
	lit, ok := r.Word.Parts[0].(*syntax.Lit)
	if !ok {
		return
	}
	switch {
	case parser.IsDigits(lit.Value) && !p.insideBrackets(r):
		span, ok := p.span(r.Word)
		if !ok {
			return
		}
		span.Start = parser.Offset(r.OpPos)
		p.report(2210, span, "This writes to or reads from a file named "+lit.Value+"; use -gt and friends to compare numbers, or >&"+lit.Value+" to duplicate a file descriptor.")
	case posixUtilities[lit.Value] && lit.Value != "file":
		p.reportNode(2238, r.Word, "This redirects to or from a file named "+lit.Value+", which is also a command name; did you mean to pipe to it instead?")
	}
}

func (p *pass) insideBrackets(r *syntax.Redirect) bool {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return false
	}
	call, ok := bracketCall(stmt.Cmd)
	return ok && endsWithBracket(call) && isWithin(call, r)
}

type descriptors struct {
	fds   []int
	known bool
}

func redirectDescriptors(r *syntax.Redirect) descriptors {
	var fds []int
	switch r.Op {
	case syntax.RdrIn, syntax.Hdoc, syntax.DashHdoc, syntax.HashHdoc, syntax.WordHdoc:
		fds = []int{0}
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob:
		fds = []int{1}
	case syntax.RdrAll, syntax.AppAll:
		fds = []int{1, 2}
	case syntax.DplOut:
		if target := r.Word.Lit(); target == "" || target == "-" || parser.IsDigits(target) {
			return descriptors{}
		}
		fds = []int{1, 2}
	default:
		return descriptors{}
	}
	if r.N == nil {
		return descriptors{fds: fds, known: true}
	}
	fd, err := strconv.Atoi(r.N.Value)
	if err != nil {
		return descriptors{}
	}
	return descriptors{fds: []int{fd}, known: true}
}

type redirectTarget struct {
	redirect *syntax.Redirect
	fds      []int
}

func determinableRedirects(stmt *syntax.Stmt) ([]redirectTarget, bool) {
	out := make([]redirectTarget, 0, len(stmt.Redirs))
	for _, r := range stmt.Redirs {
		d := redirectDescriptors(r)
		if !d.known {
			return nil, false
		}
		out = append(out, redirectTarget{redirect: r, fds: d.fds})
	}
	return out, true
}

func lastTargeting(targets []redirectTarget, fd int) (*syntax.Redirect, bool) {
	for _, t := range slices.Backward(targets) {
		if slices.Contains(t.fds, fd) {
			return t.redirect, true
		}
	}
	return nil, false
}

func checkPipedRedirections(p *pass, bin *syntax.BinaryCmd) {
	if !pipes(bin.Op) {
		return
	}
	receiver := leftmost(bin.Y)
	consumer := containsConsumer(receiver)
	if targets, ok := determinableRedirects(receiver); ok && !consumer {
		if r, found := lastTargeting(targets, 0); found {
			p.report(2259, p.inputOverrideSpan(r), "This input redirection replaces the piped input, so the output of the previous command is discarded.")
		}
	}
	if call, ok := receiver.Cmd.(*syntax.CallExpr); ok && !consumer {
		p.checkPipedIntoNonReader(call)
	}
	sender := rightmost(bin.X)
	if bin.Op != syntax.Pipe || hasProcSubst(sender, syntax.CmdOut) {
		return
	}
	if targets, ok := determinableRedirects(sender); ok {
		if r, found := lastTargeting(targets, 1); found {
			p.report(2260, operatorSpan(r), "This output redirection takes the output away from the pipe, so the next command reads nothing.")
		}
	}
}

func (p *pass) inputOverrideSpan(r *syntax.Redirect) parser.Span {
	if parser.IsHeredoc(r.Op) {
		if span, ok := p.span(r.Word); ok {
			return span
		}
	}
	return operatorSpan(r)
}

func operatorSpan(r *syntax.Redirect) parser.Span {
	start := parser.Offset(r.OpPos)
	end := start + len(r.Op.String())
	if r.Op == syntax.RdrAll || r.Op == syntax.AppAll {
		start++
	}
	return parser.Span{Start: start, End: end}
}

func checkCompetingRedirections(p *pass, stmt *syntax.Stmt) {
	if len(stmt.Redirs) < 2 {
		return
	}
	targets, ok := determinableRedirects(stmt)
	if !ok {
		return
	}
	for i, t := range targets {
		if fd, clash := sharedDescriptor(t, targets, i); clash {
			p.report(2261, operatorSpan(t.redirect), "Several redirections compete for "+descriptorName(fd)+"; only the last one takes effect.")
		}
	}
}

func sharedDescriptor(t redirectTarget, all []redirectTarget, self int) (int, bool) {
	for j, other := range all {
		if j == self {
			continue
		}
		for _, fd := range t.fds {
			if slices.Contains(other.fds, fd) {
				return fd, true
			}
		}
	}
	return 0, false
}

func descriptorName(fd int) string {
	switch fd {
	case 0:
		return "stdin"
	case 1:
		return "stdout"
	case 2:
		return "stderr"
	}
	return "FD " + strconv.Itoa(fd)
}

func containsConsumer(root syntax.Node) bool {
	found := false
	parser.Walk(root, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.CmdSubst:
			found = found || (!n.TempFile && !n.ReplyVar)
		case *syntax.ProcSubst:
			found = found || n.Op == syntax.CmdIn
		}
		return !found
	})
	return found
}

func hasProcSubst(root syntax.Node, op syntax.ProcOperator) bool {
	found := false
	parser.Walk(root, func(n syntax.Node) bool {
		if ps, ok := n.(*syntax.ProcSubst); ok && ps.Op == op {
			found = true
		}
		return !found
	})
	return found
}

type nonReader struct {
	name string
	args []*syntax.Word
}

func (p *pass) nonReaderOf(call *syntax.CallExpr) (nonReader, bool) {
	command, ok := p.resolved(call)
	if !ok || !command.literal {
		return nonReader{}, false
	}
	name := path.Base(command.name)
	if !nonReadingCommands[name] {
		return nonReader{}, false
	}
	return nonReader{name: name, args: command.args}, true
}

func (c nonReader) interactive() bool {
	if c.name != "cp" && c.name != "mv" && c.name != "rm" {
		return false
	}
	return parseOptions(c.name, c.args).Has("-i", "--interactive")
}

func (c nonReader) readsFileList() bool {
	return c.name == "du" && parseOptions(c.name, c.args).Has("--exclude-from", "--files0-from")
}

func (c nonReader) advice() string {
	if c.name == "echo" {
		return "did you mean cat?"
	}
	return "check for a wrong command, bad quoting or a missing xargs."
}

func (p *pass) checkPipedIntoNonReader(call *syntax.CallExpr) {
	c, ok := p.nonReaderOf(call)
	if !ok || c.interactive() || c.readsFileList() {
		return
	}
	p.reportNode(2216, call, c.name+" does not read standard input, so the piped data is ignored; "+c.advice())
}

func checkRedirectedInput(p *pass, stmt *syntax.Stmt) {
	if !slices.ContainsFunc(stmt.Redirs, readsIntoStdin) {
		return
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || containsConsumer(call) {
		return
	}
	c, ok := p.nonReaderOf(call)
	if !ok || c.interactive() {
		return
	}
	p.reportNode(2217, call, c.name+" does not read standard input, so the redirected input is ignored; "+c.advice())
}

func readsIntoStdin(r *syntax.Redirect) bool {
	switch r.Op {
	case syntax.RdrIn, syntax.Hdoc, syntax.DashHdoc, syntax.HashHdoc, syntax.WordHdoc:
		return r.N == nil || r.N.Value == "0"
	}
	return false
}

func checkDiscardedSubstitution(p *pass, cs *syntax.CmdSubst) {
	if cs.ReplyVar || len(cs.Stmts) != 1 {
		return
	}
	stmt := cs.Stmts[0]
	if stmt.Negated || stmt.Background || stmt.Coprocess {
		return
	}
	if bin, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && (bin.Op == syntax.AndStmt || bin.Op == syntax.OrStmt) {
		return
	}
	r, ok := stdoutDiversion(rightmost(stmt).Redirs)
	if !ok {
		return
	}
	p.reportNode(2327, cs, "This command substitution is always empty because the command's output is redirected elsewhere.")
	advice := "remove the redirection, or use tee to both save and capture the output."
	if r.Word != nil && r.Word.Lit() == "/dev/null" {
		advice = "remove the redirection to capture the output."
	}
	p.reportNode(2328, r, "This redirection takes the output away from the command substitution; "+advice)
}

func stdoutDiversion(redirs []*syntax.Redirect) (*syntax.Redirect, bool) {
	for _, r := range redirs {
		fromStdout := r.N == nil || r.N.Value == "1"
		switch r.Op {
		case syntax.DplOut:
			switch {
			case r.Word.Lit() == "1", r.N != nil && r.N.Value == "1":
				return nil, false
			case r.N == nil:
				return r, true
			}
		case syntax.RdrOut, syntax.AppOut:
			if fromStdout {
				return r, true
			}
		}
	}
	return nil, false
}

func checkRedirectSideEffects(p *pass, stmt *syntax.Stmt) {
	if len(stmt.Redirs) == 0 || p.dialect == dialect.Dash || p.dialect == dialect.Busybox {
		return
	}
	if call, ok := stmt.Cmd.(*syntax.CallExpr); !ok || len(call.Args) == 0 {
		return
	}
	for _, r := range stmt.Redirs {
		for _, part := range redirectParts(r) {
			if arith, ok := part.(*syntax.ArithmExp); ok {
				p.reportArithmeticModifications(arith.X)
			}
		}
	}
}

func redirectParts(r *syntax.Redirect) []syntax.WordPart {
	if r.Hdoc != nil {
		return r.Hdoc.Parts
	}
	if r.Word == nil {
		return nil
	}
	return flattenQuotes(r.Word.Parts)
}

func (p *pass) reportArithmeticModifications(expr syntax.ArithmExpr) {
	switch e := expr.(type) {
	case *syntax.UnaryArithm:
		if e.Op == syntax.Inc || e.Op == syntax.Dec {
			p.reportArithmeticModification(e.OpPos, e)
		}
	case *syntax.BinaryArithm:
		if parser.IsArithmeticAssignment(e.Op) {
			p.reportArithmeticModification(e.OpPos, e)
			return
		}
		p.reportArithmeticModifications(e.X)
		p.reportArithmeticModifications(e.Y)
	}
}

func (p *pass) reportArithmeticModification(op syntax.Pos, n syntax.Node) {
	span, ok := p.span(n)
	if !ok {
		return
	}
	span.Start = parser.Offset(op)
	p.report(2257, span, "This arithmetic modification happens in a redirection, which the shell may evaluate in a subshell or not at all; do it in a separate command.")
}
