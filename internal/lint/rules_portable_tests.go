package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var portableTestsFamily = family{
	codes: map[Code]meta{
		2332: {severity: SeverityError, summary: "[ ! -o name ] or [ ! -a file ] reads -o or -a as a binary operator and is always true."},
		3010: {shells: posixOrDash, summary: "[[ ]] tests are undefined in POSIX sh and dash."},
		3012: {shells: posixOnly, summary: "Lexicographic comparison with < or > in [ ] is undefined in POSIX sh."},
		3014: {shells: posixOrDash, summary: "== in [ ] is undefined in POSIX sh and dash."},
		3015: {shells: strictShells, summary: "Regex matching with =~ is undefined in POSIX sh."},
		3016: {shells: strictShells, summary: "test -v is undefined in POSIX sh."},
		3017: {shells: strictShells, summary: "The unary test -a is undefined in POSIX sh."},
		3062: {shells: strictShells, summary: "Checking a shell option with test -o is undefined in POSIX sh."},
		3063: {shells: strictShells, summary: "test -R is undefined in POSIX sh."},
		3064: {shells: strictShells, summary: "test -N is undefined in POSIX sh."},
		3065: {shells: posixOnly, summary: "test -k is undefined in POSIX sh."},
		3066: {shells: posixOnly, summary: "test -G is undefined in POSIX sh."},
		3067: {shells: posixOnly, summary: "test -O is undefined in POSIX sh."},
	},
	nodes: []nodeRule{onNodes(checkPortableTest, &syntax.TestClause{}, &syntax.CallExpr{})},
}

var (
	unaryTestIssues = map[string]portabilityIssue{
		"-v": {code: 3016, feature: "test -v", alternative: `[ -n "${name+set}" ]`},
		"-a": {code: 3017, feature: "The unary test -a", alternative: "-e", replacement: "-e"},
		"-o": {code: 3062, feature: "Checking a shell option with test -o", alternative: `case $- in *x*) ... esac`},
		"-R": {code: 3063, feature: "test -R, like namerefs in general,"},
		"-N": {code: 3064, feature: "test -N"},
		"-k": {code: 3065, feature: "test -k"},
		"-G": {code: 3066, feature: "test -G"},
		"-O": {code: 3067, feature: "test -O"},
	}
	binaryTestIssues = map[string]portabilityIssue{
		"<":  {code: 3012, feature: "Lexicographic comparison with <"},
		">":  {code: 3012, feature: "Lexicographic comparison with >"},
		"<=": {code: 3012, feature: "Lexicographic comparison with <="},
		">=": {code: 3012, feature: "Lexicographic comparison with >="},
		"==": {code: 3014, feature: "Comparing with ==", alternative: "=", replacement: "="},
		"=~": {code: 3015, feature: "Regex matching with =~", alternative: "expr or a case pattern"},
	}
)

func checkPortableTest(p *pass, n syntax.Node) {
	if p.dialect != dialect.Bash && !strictShells.Has(p.dialect) {
		return
	}
	switch n := n.(type) {
	case *syntax.TestClause:
		p.nonPortableNode(portabilityIssue{code: 3010, feature: "The [[ ]] test", alternative: "[ ] or test"}, n)
		if t, ok := testCommandOf(n); ok {
			p.checkPortableOperators(t)
		}
	case *syntax.CallExpr:
		checkTestCommand(p, n)
	}
}

func checkTestCommand(p *pass, call *syntax.CallExpr) {
	t, ok := testCommandOf(call)
	switch {
	case !ok:
	case t.style == singleBracket:
		if !t.unterminated {
			p.checkPortableOperators(t)
		}
		checkRedirectedComparison(p, call)
	case t.name == "test":
		checkTestArguments(p, call)
	}
}

func (p *pass) checkPortableOperators(t testCommand) {
	p.testOperands(t, func(o testOperand) {
		issues := binaryTestIssues
		switch o.shape {
		case shapeUnary:
			issues = unaryTestIssues
		case shapeLeft:
		default:
			return
		}
		if issue, ok := issues[o.op]; ok {
			p.nonPortable(issue, o.opSpan, p.replacementFix(issue, o.opSpan)...)
		}
		if o.negated && !o.double && p.dialect == dialect.Bash && (o.op == "-o" || o.op == "-a") {
			p.reportNegatedConnective(o)
		}
	})
}

func checkRedirectedComparison(p *pass, call *syntax.CallExpr) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return
	}
	for _, r := range stmt.Redirs {
		if isComparisonRedirect(r) && isWithin(call, r) {
			op := redirectedOperator(r)
			p.nonPortable(binaryTestIssues[op], posSpan(r.OpPos, len(op)))
		}
	}
}

func redirectedOperator(r *syntax.Redirect) string {
	op := r.Op.String()
	if r.Word == nil || parser.Offset(r.Word.Pos()) != parser.Offset(r.OpPos)+len(op) {
		return op
	}
	if lit, ok := r.Word.Parts[0].(*syntax.Lit); ok && strings.HasPrefix(lit.Value, "=") {
		return op + "="
	}
	return op
}

func (p *pass) reportNegatedConnective(op testOperand) {
	span := op.negation
	if op.op == "-o" {
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
