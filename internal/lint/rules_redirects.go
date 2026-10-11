package lint

import (
	"path"
	"slices"
	"strconv"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var redirectsFamily = family{
	codes: map[Code]meta{
		2210: {severity: SeverityWarning},
		2216: {severity: SeverityWarning},
		2217: {severity: SeverityWarning},
		2238: {severity: SeverityWarning},
		2257: {severity: SeverityWarning},
		2259: {severity: SeverityError},
		2260: {severity: SeverityError},
		2261: {severity: SeverityError},
		2327: {severity: SeverityWarning},
		2328: {severity: SeverityError},
	},
	rules: []rule{
		{on: nodes(&syntax.Redirect{}), node: checkRedirectTargets},
		{on: nodes(&syntax.BinaryCmd{}), node: checkPipedRedirections},
		{on: nodes(&syntax.Stmt{}), node: checkCompetingRedirections},
		{on: nodes(&syntax.Stmt{}), node: checkRedirectedInput},
		{on: nodes(&syntax.CmdSubst{}), node: checkDiscardedSubstitution},
		{on: nodes(&syntax.Stmt{}), node: checkRedirectSideEffects},
	},
}

var nonReadingCommands = setOf(
	"alias", "basename", "bg", "cal", "cd", "chgrp", "chmod", "chown", "cp", "du", "echo", "export", "fg", "fuser",
	"getconf", "getopt", "getopts", "ipcrm", "ipcs", "jobs", "kill", "ln", "ls", "locale", "mv", "printf", "ps", "pwd",
	"readlink", "realpath", "renice", "rm", "rmdir", "set", "sleep", "touch", "trap", "ulimit", "unalias", "uname",
)

var utilityNames = setOf(
	":", ".", "admin", "alias", "ar", "asa", "at", "awk", "basename", "batch", "bc", "bg", "break", "c99", "cal", "cat",
	"cd", "cflow", "chgrp", "chmod", "chown", "cksum", "cmp", "comm", "command", "compress", "continue", "cp", "crontab",
	"csplit", "ctags", "cut", "cxref", "date", "dd", "delta", "df", "diff", "dirname", "du", "echo", "ed", "env", "eval",
	"ex", "exec", "exit", "expand", "export", "expr", "false", "fc", "fg", "find", "fold", "fort77", "fuser", "gencat",
	"get", "getconf", "getopts", "grep", "hash", "head", "iconv", "id", "ipcrm", "ipcs", "jobs", "join", "kill", "lex",
	"link", "ln", "locale", "localedef", "logger", "logname", "lp", "ls", "m4", "mailx", "make", "man", "mesg", "mkdir",
	"mkfifo", "more", "mv", "newgrp", "nice", "nl", "nm", "nohup", "od", "paste", "patch", "pathchk", "pax", "pr",
	"printf", "prs", "qalter", "qdel", "qhold", "qmove", "qmsg", "qrerun", "qrls", "qselect", "qsig", "qstat", "qsub",
	"read", "readlink", "readonly", "realpath", "renice", "return", "rm", "rmdel", "rmdir", "sact", "sccs", "sed", "set",
	"sh", "shift", "sleep", "sort", "split", "strings", "strip", "stty", "tabs", "tail", "talk", "tee", "test", "time",
	"timeout", "times", "touch", "tput", "tr", "trap", "true", "tsort", "tty", "type", "ulimit", "umask", "unalias",
	"uname", "uncompress", "unexpand", "unget", "uniq", "unlink", "unset", "uucp", "uudecode", "uuencode", "uustat",
	"uux", "val", "vi", "wait", "wc", "what", "who", "write", "xargs", "yacc", "zcat",
)

func isFileRedirect(op syntax.RedirOperator) bool {
	switch op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrIn, syntax.RdrInOut, syntax.RdrClob, syntax.AppClob,
		syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
		return true
	}
	return false
}

func checkRedirectTargets(p *pass, n syntax.Node) {
	r, ok := n.(*syntax.Redirect)
	if !ok || r.Word == nil || !isFileRedirect(r.Op) || len(r.Word.Parts) != 1 {
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
	case utilityNames[lit.Value] && lit.Value != "file":
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

func checkPipedRedirections(p *pass, n syntax.Node) {
	bin, ok := n.(*syntax.BinaryCmd)
	if !ok || !pipes(bin.Op) {
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

func checkCompetingRedirections(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || len(stmt.Redirs) < 2 {
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
	word := p.effectiveCommandWord(call)
	if word == nil {
		return nonReader{}, false
	}
	lit, ok := parser.Literal(word)
	if !ok {
		return nonReader{}, false
	}
	name := path.Base(lit)
	if !nonReadingCommands[name] {
		return nonReader{}, false
	}
	return nonReader{name: name, args: call.Args[slices.Index(call.Args, word)+1:]}, true
}

func (c nonReader) interactive() bool {
	if c.name != "cp" && c.name != "mv" && c.name != "rm" {
		return false
	}
	return parseFlags(c.args, "").has('i', "interactive")
}

func (c nonReader) readsFileList() bool {
	flags := parseFlags(c.args, "")
	return c.name == "du" && (flags.has(0, "exclude-from") || flags.has(0, "files0-from"))
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

func checkRedirectedInput(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || !slices.ContainsFunc(stmt.Redirs, readsIntoStdin) {
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

func checkDiscardedSubstitution(p *pass, n syntax.Node) {
	cs, ok := n.(*syntax.CmdSubst)
	if !ok || cs.ReplyVar || len(cs.Stmts) != 1 {
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

func checkRedirectSideEffects(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || len(stmt.Redirs) == 0 || p.dialect == dialect.Dash || p.dialect == dialect.Busybox {
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
