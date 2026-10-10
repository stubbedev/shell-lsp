package lint

import (
	"cmp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var bracketsFamily = family{
	codes: map[Code]meta{
		1014: {severity: SeverityWarning},
		1019: {severity: SeverityError},
		1020: {severity: SeverityError},
		1021: {severity: SeverityError},
		1026: {severity: SeverityError},
		1027: {severity: SeverityError},
		1034: {severity: SeverityError},
		1035: {severity: SeverityError},
	},
	rules: []rule{
		{node: checkSingleBracketTest},
		{node: checkDoubleBracketTest},
		{node: checkGluedNegation},
	},
}

var posixUtilities = setOf(
	"admin", "alias", "ar", "asa", "at", "awk", "basename", "batch", "bc", "bg", "break", "c99", "cal", "cat", "cd",
	"cflow", "chgrp", "chmod", "chown", "cksum", "cmp", "comm", "command", "compress", "continue", "cp", "crontab",
	"csplit", "ctags", "cut", "cxref", "date", "dd", "delta", "df", "diff", "dirname", "du", "echo", "ed", "env",
	"eval", "ex", "exec", "exit", "expand", "export", "expr", "fc", "fg", "file", "find", "fold", "fort77", "fuser",
	"gencat", "get", "getconf", "getopts", "grep", "hash", "head", "iconv", "ipcrm", "ipcs", "jobs", "join", "kill",
	"lex", "link", "ln", "locale", "localedef", "logger", "logname", "lp", "ls", "m4", "mailx", "make", "man", "mesg",
	"mkdir", "mkfifo", "more", "mv", "newgrp", "nice", "nl", "nm", "nohup", "od", "paste", "patch", "pathchk", "pax",
	"pr", "printf", "prs", "ps", "pwd", "qalter", "qdel", "qhold", "qmove", "qmsg", "qrerun", "qrls", "qselect",
	"qsig", "qstat", "qsub", "read", "readonly", "renice", "return", "rm", "rmdel", "rmdir", "sact", "sccs", "sed",
	"set", "sh", "shift", "sleep", "sort", "split", "strings", "strip", "stty", "tabs", "tail", "talk", "tee", "test",
	"time", "times", "touch", "tput", "tr", "trap", "tsort", "tty", "type", "ulimit", "umask", "unalias", "uname",
	"uncompress", "unexpand", "unget", "uniq", "unlink", "unset", "uucp", "uudecode", "uuencode", "uustat", "uux",
	"val", "vi", "wait", "wc", "what", "who", "write", "xargs", "yacc", "zcat",
)

var testSymbolOperators = symbolTestOperators()

func symbolTestOperators() []string {
	var out []string
	for op := range binaryTestOperators {
		switch op[0] {
		case '-':
			continue
		case '<', '>':
			out = append(out, `\`+op)
		}
		out = append(out, op)
	}
	slices.SortFunc(out, func(a, b string) int { return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b)) })
	return out
}

const (
	testAnd = "-a"
	testOr  = "-o"
)

type testWord struct {
	start, end int
}

type bracketTest struct {
	p     *pass
	words []testWord
	next  int
	close int
}

func checkSingleBracketTest(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	if first, ok := p.span(call.Args[0]); !ok || !strings.HasPrefix(p.text[first.Start:], "[") {
		return
	}
	words := make([]testWord, 0, len(call.Args))
	for _, arg := range call.Args {
		span, ok := p.span(arg)
		if !ok {
			return
		}
		words = append(words, testWord{start: span.Start, end: span.End})
	}
	opening, last := words[0], words[len(words)-1]
	opener, closer := p.text[opening.start:opening.end], p.text[last.start:last.end]
	bracket := "["
	if strings.HasPrefix(opener, "[[") {
		bracket = "[["
	}
	switch {
	case opener == "[":
		words = words[1:]
	case opener != "[[" && strings.HasSuffix(closer, "]"):
		end := opening.start + len(bracket)
		p.report(1035, parser.Span{Start: opening.start, End: end}, "Put a space after "+bracket+" and before the closing bracket; without it the shell looks for a command named "+opener+".")
		words[0].start = end
	default:
		return
	}
	t := bracketTest{p: p, words: words, close: last.end}
	switch closer {
	case "]]":
		if bracket == "[" {
			p.report(1034, parser.Span{Start: opening.start, End: opening.start + 1}, "This test opens with a single [ but ends with ]]; use [ ... ] or [[ ... ]] consistently.")
		}
		t.words, t.close = words[:len(words)-1], last.start
	case "]":
		t.words, t.close = words[:len(words)-1], last.start
	}
	if len(t.words) == 0 {
		return
	}
	t.commandAsOperand()
	t.expression()
}

func (t *bracketTest) text(w testWord) string { return t.p.text[w.start:w.end] }

func (t *bracketTest) peek() (testWord, bool) {
	if t.next >= len(t.words) {
		return testWord{}, false
	}
	return t.words[t.next], true
}

func (t *bracketTest) skip(n int) {
	t.words[t.next].start += n
}

func (t *bracketTest) commandAsOperand() {
	first := t.words[0]
	if name := t.text(first); posixUtilities[name] {
		t.p.report(1014, parser.Span{Start: first.start, End: first.end}, commandInTestMessage(name))
	}
}

func commandInTestMessage(name string) string {
	return "The brackets test the string " + name + " instead of running it; to act on its exit status write if " + name + " ...; then without brackets."
}

func (t *bracketTest) expression() bool {
	for {
		if !t.term() {
			return false
		}
		w, ok := t.peek()
		if !ok || isTestGroupClose(t.text(w)) {
			return true
		}
		text := t.text(w)
		switch dashWord(text) {
		case testAnd, testOr:
			if len(text) == len(testAnd) {
				t.next++
				continue
			}
			t.missingSpace(w.start+len(testAnd), text[:len(testAnd)])
			t.skip(len(testAnd))
			continue
		}
		return false
	}
}

func (t *bracketTest) term() bool {
	w, ok := t.peek()
	if !ok {
		return false
	}
	text := t.text(w)
	switch {
	case text == "!":
		t.next++
		return t.term()
	case text[0] == '!':
		t.missingSpace(w.start+1, "!")
		t.skip(1)
		return t.term()
	case isTestGroupOpen(text):
		t.next++
		if !t.expression() {
			return false
		}
		if w, ok := t.peek(); ok && isTestGroupClose(t.text(w)) {
			t.next++
			return true
		}
		return false
	case text[0] == '[':
		t.p.report(1026, parser.Span{Start: w.start, End: w.start + 1}, "A nested [ does not group conditions inside [ ]; use escaped parentheses \\( ... \\) or separate tests joined with && or ||.")
		return false
	}
	if op := dashWord(text); op != "" {
		if len(op) < len(text) {
			t.missingSpace(w.start+len(op), op)
			t.skip(len(op))
			return t.operand()
		}
		t.next++
		operand, ok := t.peek()
		if !ok {
			t.missingOperand(op, t.close)
			return false
		}
		if !t.operand() {
			t.missingOperand(op, operand.start)
			return false
		}
		return true
	}
	return t.operand() && t.binary()
}

func (t *bracketTest) missingOperand(op string, at int) {
	t.p.report(1019, parser.Span{Start: at, End: at}, "The unary test "+op+" needs an operand before the test ends.")
}

func (t *bracketTest) operand() bool {
	w, _ := t.peek()
	t.next++
	text := t.text(w)
	switch {
	case strings.HasSuffix(text, "]") && !strings.HasSuffix(text, `\]`) && !strings.Contains(text, "["):
		t.p.report(1020, parser.Span{Start: w.end, End: w.end}, "Put a space before the closing ]; glued to "+text[:len(text)-1]+" it is part of that word and does not end the test.")
		return false
	case strings.HasSuffix(text, `\)`):
		t.p.report(1021, parser.Span{Start: w.end, End: w.end}, `Put a space before the escaped \); glued to the word it is part of that word and does not close the group.`)
		return false
	}
	return true
}

func (t *bracketTest) binary() bool {
	w, ok := t.peek()
	if !ok {
		return true
	}
	text := t.text(w)
	op := testOperatorPrefix(text)
	switch {
	case op == "" && isTestGroupClose(text), op == testAnd, op == testOr:
		return true
	case op == "":
		return false
	case len(op) < len(text):
		t.missingSpace(w.start+len(op), op)
		t.skip(len(op))
		return t.operand()
	}
	t.next++
	if _, ok := t.peek(); !ok {
		t.p.report(1027, parser.Span{Start: w.start, End: w.end}, "The comparison "+op+" needs a right-hand operand before the test ends.")
		return false
	}
	return t.operand()
}

func (t *bracketTest) missingSpace(at int, after string) {
	t.p.report(1035, parser.Span{Start: at, End: at}, "Put a space after "+after+"; the test reads it as glued to the next word.")
}

func dashWord(text string) string {
	if len(text) < 2 || text[0] != '-' {
		return ""
	}
	end := 1
	for end < len(text) && isASCIILetter(text[end]) {
		end++
	}
	if end == 1 {
		return ""
	}
	return text[:end]
}

func testOperatorPrefix(text string) string {
	if op := dashWord(text); op != "" {
		return op
	}
	for _, op := range testSymbolOperators {
		if strings.HasPrefix(text, op) {
			return op
		}
	}
	return ""
}

func isTestGroupOpen(text string) bool {
	return text == `\(` || text == `'('` || text == `"("`
}

func isTestGroupClose(text string) bool {
	return text == `\)` || text == `')'` || text == `")"`
}

func isASCIILetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func checkDoubleBracketTest(p *pass, n syntax.Node) {
	tc, ok := n.(*syntax.TestClause)
	if !ok {
		return
	}
	if w := leftmostTestWord(tc.X); w != nil {
		if name, ok := parser.Literal(w); ok && posixUtilities[name] && w.Lit() == name {
			if span, ok := p.span(w); ok {
				p.report(1014, span, commandInTestMessage(name))
			}
		}
	}
	p.gluedTestTerms(tc.X)
}

func leftmostTestWord(x syntax.TestExpr) *syntax.Word {
	for {
		switch e := x.(type) {
		case *syntax.Word:
			return e
		case *syntax.BinaryTest:
			x = e.X
		default:
			return nil
		}
	}
}

func (p *pass) gluedTestTerms(x syntax.TestExpr) {
	switch e := x.(type) {
	case *syntax.Word:
		p.gluedTestTerm(e)
	case *syntax.UnaryTest:
		if e.Op == syntax.TsNot {
			p.gluedTestTerms(e.X)
		}
	case *syntax.BinaryTest:
		if e.Op == syntax.AndTest || e.Op == syntax.OrTest {
			p.gluedTestTerms(e.X)
			p.gluedTestTerms(e.Y)
		}
	case *syntax.ParenTest:
		p.gluedTestTerms(e.X)
	}
}

func (p *pass) gluedTestTerm(w *syntax.Word) {
	span, ok := p.span(w)
	if !ok {
		return
	}
	text := p.text[span.Start:span.End]
	after := ""
	switch op := dashWord(text); {
	case len(text) > 1 && text[0] == '!':
		after = "!"
	case op != "" && len(op) < len(text):
		after = op
	default:
		return
	}
	at := span.Start + len(after)
	p.report(1035, parser.Span{Start: at, End: at}, "Put a space after "+after+"; the test reads it as glued to the next word.")
}

func checkGluedNegation(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	span, ok := p.span(call.Args[0])
	if !ok {
		return
	}
	text := p.text[span.Start:span.End]
	if strings.HasPrefix(text, "!#") || span.Start == 0 && strings.HasPrefix(text, "!/") {
		return
	}
	for i := 0; i+1 < len(text) && text[i] == '!'; i++ {
		at := span.Start + i + 1
		p.report(1035, parser.Span{Start: at, End: at}, "Put a space after ! to negate the command; glued to it the shell looks for a command named "+text[i:]+".")
	}
}
