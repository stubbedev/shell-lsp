package lint

import (
	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var portableTestsFamily = family{
	codes: map[Code]meta{
		2332: {severity: SeverityError},
		3010: {severity: SeverityWarning},
		3012: {severity: SeverityWarning},
		3014: {severity: SeverityWarning},
		3015: {severity: SeverityWarning},
		3016: {severity: SeverityWarning},
		3017: {severity: SeverityWarning},
		3062: {severity: SeverityWarning},
		3063: {severity: SeverityWarning},
		3064: {severity: SeverityWarning},
		3065: {severity: SeverityWarning},
		3066: {severity: SeverityWarning},
		3067: {severity: SeverityWarning},
	},
	rules: []rule{{node: checkPortableTest}},
}

var (
	unaryTestIssues = map[string]portabilityIssue{
		"-v": {code: 3016, shells: strictShells, feature: "test -v", alternative: `[ -n "${name+set}" ]`},
		"-a": {code: 3017, shells: strictShells, feature: "The unary test -a", alternative: "-e"},
		"-o": {code: 3062, shells: strictShells, feature: "Checking a shell option with test -o", alternative: `case $- in *x*) ... esac`},
		"-R": {code: 3063, shells: strictShells, feature: "test -R, like namerefs in general,"},
		"-N": {code: 3064, shells: strictShells, feature: "test -N"},
		"-k": {code: 3065, shells: posixOnly, feature: "test -k"},
		"-G": {code: 3066, shells: posixOnly, feature: "test -G"},
		"-O": {code: 3067, shells: posixOnly, feature: "test -O"},
	}
	binaryTestIssues = map[string]portabilityIssue{
		"<":  {code: 3012, shells: posixOnly, feature: "Lexicographic comparison with <"},
		">":  {code: 3012, shells: posixOnly, feature: "Lexicographic comparison with >"},
		"<=": {code: 3012, shells: posixOnly, feature: "Lexicographic comparison with <="},
		">=": {code: 3012, shells: posixOnly, feature: "Lexicographic comparison with >="},
		"==": {code: 3014, shells: posixOrDash, feature: "Comparing with ==", alternative: "="},
		"=~": {code: 3015, shells: strictShells, feature: "Regex matching with =~", alternative: "expr or a case pattern"},
	}
	unaryTestOperators  = setOf("-a", "-b", "-c", "-d", "-e", "-f", "-g", "-h", "-k", "-n", "-o", "-p", "-r", "-s", "-t", "-u", "-w", "-x", "-z", "-G", "-L", "-N", "-O", "-R", "-S", "-v")
	binaryTestOperators = setOf("=", "==", "!=", "<", ">", "<=", ">=", "=~", "-eq", "-ne", "-lt", "-le", "-gt", "-ge", "-nt", "-ot", "-ef")
)

func checkPortableTest(p *pass, n syntax.Node) {
	if p.dialect != dialect.Bash && !strictShells.Has(p.dialect) {
		return
	}
	switch n := n.(type) {
	case *syntax.TestClause:
		p.nonPortableNode(portabilityIssue{code: 3010, shells: posixOrDash, feature: "The [[ ]] test", alternative: "[ ] or test"}, n)
	case *syntax.UnaryTest:
		op := p.text[parser.Offset(n.OpPos):][:2]
		if issue, ok := unaryTestIssues[op]; ok {
			p.nonPortable(issue, posSpan(n.OpPos, len(op)))
		}
	case *syntax.BinaryTest:
		if issue, ok := binaryTestIssues[n.Op.String()]; ok {
			p.nonPortable(issue, posSpan(n.OpPos, len(n.Op.String())))
		}
	case *syntax.CallExpr:
		checkTestCommand(p, n)
	}
}

func checkTestCommand(p *pass, call *syntax.CallExpr) {
	if len(call.Args) == 0 {
		return
	}
	name, _ := parser.Literal(call.Args[0])
	switch name {
	case "[":
		checkBracketTest(p, call.Args[1:])
		checkRedirectedComparison(p, call)
	case "test":
		checkTestArguments(p, call)
	}
}

func checkBracketTest(p *pass, args []*syntax.Word) {
	if len(args) == 0 {
		return
	}
	if closing, _ := parser.Literal(args[len(args)-1]); closing != "]" {
		return
	}
	for _, op := range bracketTestOperators(args[:len(args)-1]) {
		span, ok := p.span(op.word)
		if !ok {
			continue
		}
		issues := binaryTestIssues
		if op.unary {
			issues = unaryTestIssues
		}
		if issue, ok := issues[op.name]; ok {
			p.nonPortable(issue, span)
		}
		if op.negation != nil && p.dialect == dialect.Bash && (op.name == "-o" || op.name == "-a") {
			p.reportNegatedConnective(op)
		}
	}
}

func checkRedirectedComparison(p *pass, call *syntax.CallExpr) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return
	}
	closing := call.Args[len(call.Args)-1].Pos()
	for _, r := range stmt.Redirs {
		if r.N == nil && (r.Op == syntax.RdrOut || r.Op == syntax.RdrIn) && r.OpPos.Offset() < closing.Offset() {
			p.nonPortable(binaryTestIssues[r.Op.String()], posSpan(r.OpPos, 1))
		}
	}
}

func (p *pass) reportNegatedConnective(op testOperator) {
	span, ok := p.span(op.negation)
	if !ok {
		return
	}
	if op.name == "-o" {
		p.report(2332, span, "With three arguments, [ ! -o name ] treats -o as the binary or, so it is always true; use [[ ! -o name ]] or ! [ -o name ].")
		return
	}
	p.report(2332, span, "With three arguments, [ ! -a file ] treats -a as the binary and, so it is always true; use [ ! -e file ].")
}

func checkTestArguments(p *pass, call *syntax.CallExpr) {
	operator, issues := 2, binaryTestIssues
	switch len(call.Args) {
	case 3:
		operator, issues = 1, unaryTestIssues
	case 4:
	default:
		return
	}
	op, ok := parser.Literal(call.Args[operator])
	if issue, found := issues[op]; ok && found {
		p.nonPortableNode(issue, call)
	}
}

type testOperator struct {
	word     *syntax.Word
	name     string
	unary    bool
	negation *syntax.Word
}

type bracketParser struct {
	words []*syntax.Word
	names []string
	pos   int
	out   []testOperator
}

func bracketTestOperators(words []*syntax.Word) []testOperator {
	b := bracketParser{words: words, names: make([]string, len(words))}
	for i, w := range words {
		b.names[i], _ = parser.Literal(w)
	}
	for b.pos < len(words) {
		b.or()
		b.pos++
	}
	return b.out
}

func (b *bracketParser) peek(offset int) string {
	if b.pos+offset >= len(b.names) {
		return ""
	}
	return b.names[b.pos+offset]
}

func (b *bracketParser) or() {
	b.and()
	for b.peek(0) == "-o" {
		b.pos++
		b.and()
	}
}

func (b *bracketParser) and() {
	b.not(nil)
	for b.peek(0) == "-a" {
		b.pos++
		b.not(nil)
	}
}

func (b *bracketParser) not(negation *syntax.Word) {
	if b.peek(0) == "!" && b.pos+1 < len(b.words) {
		bang := b.words[b.pos]
		b.pos++
		b.not(bang)
		return
	}
	b.primary(negation)
}

func (b *bracketParser) primary(negation *syntax.Word) {
	switch {
	case b.pos >= len(b.words):
	case b.peek(0) == "(":
		b.pos++
		b.or()
		if b.peek(0) == ")" {
			b.pos++
		}
	case unaryTestOperators[b.peek(0)] && b.pos+1 < len(b.words):
		b.out = append(b.out, testOperator{word: b.words[b.pos], name: b.peek(0), unary: true, negation: negation})
		b.pos += 2
	case binaryTestOperators[b.peek(1)] && b.pos+2 < len(b.words):
		b.out = append(b.out, testOperator{word: b.words[b.pos+1], name: b.peek(1)})
		b.pos += 3
	default:
		b.pos++
	}
}
