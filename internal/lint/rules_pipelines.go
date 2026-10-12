package lint

import (
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var pipelinesFamily = family{
	codes: map[Code]meta{
		2000: {severity: SeverityStyle, summary: "echo piped to wc counts a string's length that ${#var} gives directly."},
		2002: {severity: SeverityStyle, optional: optionalUselessCat, summary: "cat only feeds a file to the next command, which could read it directly."},
		2009: {severity: SeverityInfo, bug: true, summary: "Grepping ps output also matches the grep itself."},
		2010: {severity: SeverityWarning, summary: "Filtering ls output with grep breaks on unusual file names."},
		2011: {severity: SeverityWarning, summary: "Piping ls output to xargs breaks on file names with spaces."},
		2012: {severity: SeverityInfo, bug: true, summary: "Parsing ls output breaks on unusual file names."},
		2015: {severity: SeverityInfo, bug: true, summary: "A && B || C is not if-then-else: C also runs when A succeeds and B fails."},
		2036: {severity: SeverityWarning, summary: "A pipeline stage that only assigns a variable pipes nothing."},
		2038: {severity: SeverityWarning, summary: "Piping find output to xargs breaks on file names with spaces or newlines."},
		2126: {severity: SeverityStyle, summary: "grep piped to wc -l counts matching lines that grep -c counts directly."},
	},
	rules: []rule{{on: nodes(&syntax.Stmt{}), node: checkPipeline}, {on: nodes(&syntax.BinaryCmd{}), node: checkAndOrFallback}},
}

type stage struct {
	stmt *syntax.Stmt
	call *syntax.CallExpr
	name string
}

func stagesOf(stmts []*syntax.Stmt) []stage {
	out := make([]stage, len(stmts))
	for i, stmt := range stmts {
		out[i].stmt = stmt
		if call, ok := stmt.Cmd.(*syntax.CallExpr); ok {
			out[i].call = call
			if len(call.Args) > 0 {
				out[i].name, _ = parser.Literal(call.Args[0])
			}
		}
	}
	return out
}

func (s stage) args() []*syntax.Word { return s.call.Args[1:] }

func (p *pass) pipelineTop(n syntax.Node) (*syntax.Stmt, bool) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok {
		return nil, false
	}
	bin, ok := p.parent().(*syntax.BinaryCmd)
	return stmt, !ok || !pipes(bin.Op)
}

func isPipe(stmt *syntax.Stmt) bool {
	bin, ok := stmt.Cmd.(*syntax.BinaryCmd)
	return ok && pipes(bin.Op)
}

func checkPipeline(p *pass, n syntax.Node) {
	top, ok := p.pipelineTop(n)
	if !ok || !isPipe(top) {
		return
	}
	stmts := pipelineStages(top, nil)
	if len(stmts) < 2 || slices.ContainsFunc(stmts, func(s *syntax.Stmt) bool { return s.Cmd == nil }) {
		return
	}
	stages := stagesOf(stmts)
	p.checkStringLength(top, stages)
	p.checkUselessCat(stages)
	p.checkAssignmentStage(stages[0])
	listingReported := false
	for i := range len(stages) - 1 {
		listingReported = p.checkStagePair(stages[i], stages[i+1]) || listingReported
	}
	if listingReported {
		return
	}
	for _, s := range stages[:len(stages)-1] {
		if s.name == "ls" && !hasDashWordWith(s.args(), "N") {
			p.reportStage(2012, s, "Parsing ls output breaks on unusual file names; use a glob, find, or stat instead.")
		}
	}
}

func (p *pass) reportStage(code Code, s stage, message string) {
	if span, ok := p.span(s.stmt); ok {
		p.report(code, span, message)
	}
}

func (p *pass) checkStagePair(left, right stage) bool {
	if left.call == nil || right.call == nil {
		return false
	}
	switch left.name + " " + right.name {
	case "find xargs":
		if !nullSeparated(left.call.Args, right.call.Args) {
			p.reportStage(2038, left, "find output piped to xargs breaks on file names with spaces or newlines; use find -print0 | xargs -0, or find -exec.")
		}
	case "ps grep":
		if !parseOptions(left.name, left.args()).Has("-p", "--pid", "-q", "--quick-pid") {
			p.reportStage(2009, left, "Instead of grepping ps output, consider pgrep, which does not match itself.")
		}
	case "grep wc":
		if p.countsMatchingLines(left, right) {
			p.reportStage(2126, left, "grep -c counts matching lines without a wc -l pipeline.")
		}
	case "ls grep":
		p.reportNode(2010, left.call.Args[0], "Filtering ls output with grep breaks on unusual file names; use a glob or a for loop with a condition instead.")
		return true
	case "ls xargs":
		p.reportNode(2011, left.call.Args[0], "ls output piped to xargs breaks on file names with spaces; use find -print0 | xargs -0, or a glob.")
		return true
	}
	return false
}

func nullSeparated(lists ...[]*syntax.Word) bool {
	for _, words := range lists {
		for _, w := range words {
			text := roughText(w)
			if strings.HasPrefix(text, "-") && strings.Contains(text, "0") {
				return true
			}
			option := strings.TrimLeft(text, "-")
			if strings.HasPrefix(option, "null") || strings.HasPrefix(option, "print0") || strings.HasPrefix(option, "printf") {
				return true
			}
		}
	}
	return false
}

func (p *pass) countsMatchingLines(grep, wc stage) bool {
	if len(wc.args()) == 0 {
		return false
	}
	if parseOptions(grep.name, grep.args()).Has("-l", "--files-with-matches", "-L", "--files-without-matches", "-o", "--only-matching",
		"-r", "--recursive", "-R", "-A", "--after-context", "-B", "--before-context") {
		return false
	}
	return !parseOptions(wc.name, wc.args()).Has("-m", "--chars", "-w", "--words", "-c", "--bytes", "-L", "--max-line-length")
}

func hasDashWordWith(words []*syntax.Word, letters string) bool {
	for _, w := range words {
		text := roughText(w)
		if strings.HasPrefix(text, "-") && strings.ContainsAny(text, letters) {
			return true
		}
	}
	return false
}

func (p *pass) checkStringLength(top *syntax.Stmt, stages []stage) {
	if len(stages) != 2 || stages[0].call == nil || stages[1].call == nil {
		return
	}
	echo, wc := stages[0], stages[1]
	if echo.name != "echo" || len(echo.args()) != 1 || wc.name != "wc" || len(wc.args()) != 1 {
		return
	}
	if flag, _ := parser.Literal(wc.args()[0]); flag != "-c" && flag != "-m" {
		return
	}
	switch parser.SolePart(echo.args()[0]).(type) {
	case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp:
		if span, ok := p.span(top); ok {
			p.report(2000, span, "Use the ${#var} expansion to get a string's length instead of echo | wc.")
		}
	}
}

func (p *pass) checkUselessCat(stages []stage) {
	first := stages[0]
	if first.call == nil || len(first.call.Args) != 2 || path.Base(first.name) != "cat" {
		return
	}
	file := first.call.Args[1]
	if mayExpandToSeveralWords(file) || strings.HasPrefix(roughText(file), "-") {
		return
	}
	p.reportNode(2002, file, "Useless cat: pass the file to the next command directly, or redirect it with < file.", p.redirectInsteadOfCat(first, stages[1], file)...)
}

func (p *pass) redirectInsteadOfCat(cat, next stage, file *syntax.Word) []Fix {
	catSpan, ok := p.span(cat.stmt)
	fileSpan, fileOK := p.span(file)
	nextStart := parser.Offset(next.stmt.Pos())
	if !ok || !fileOK || len(cat.stmt.Redirs) > 0 || len(cat.call.Assigns) > 0 || strings.TrimSpace(p.text[catSpan.End:nextStart]) != "|" {
		return nil
	}
	if next.stmt.Negated || next.stmt.Background || slices.ContainsFunc(next.stmt.Redirs, readsInput) {
		return nil
	}
	end := commandEnd(next.stmt)
	return []Fix{reviewFix("Redirect the file instead of using cat",
		textedit.Edit{Start: catSpan.Start, End: nextStart},
		textedit.Edit{Start: end, End: end, NewText: " < " + p.text[fileSpan.Start:fileSpan.End]})}
}

func readsInput(r *syntax.Redirect) bool {
	switch r.Op {
	case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		return r.N == nil || r.N.Value == "0"
	}
	return false
}

func (p *pass) checkAssignmentStage(first stage) {
	if first.call == nil || len(first.call.Assigns) == 0 || len(first.call.Args) > 0 {
		return
	}
	if span, ok := p.span(first.call); ok {
		p.report(2036, span, "This pipeline stage only assigns a variable, so nothing is piped; use var=$(command | filter) to capture output.")
	}
}

func checkAndOrFallback(p *pass, n syntax.Node) {
	or, ok := n.(*syntax.BinaryCmd)
	if !ok || or.Op != syntax.OrStmt {
		return
	}
	and, ok := or.X.Cmd.(*syntax.BinaryCmd)
	if !ok || and.Op != syntax.AndStmt || isTestCommand(and.Y) || isSafeFallback(or.Y) {
		return
	}
	if stmt, ok := p.parent().(*syntax.Stmt); !ok || p.inCondition(stmt) {
		return
	}
	p.report(2015, opAt(and.OpPos, "&&").span, "A && B || C is not if-then-else: C also runs when A succeeds and B fails. Use an if statement if that is not intended.")
}

var safeFallbacks = setOf("echo", "exit", "return", "printf", "true", ":")

func isSafeFallback(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return false
	}
	if len(call.Args) == 0 {
		return len(call.Assigns) > 0
	}
	name, ok := parser.Literal(call.Args[0])
	return ok && safeFallbacks[path.Base(name)]
}
