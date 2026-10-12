package lint

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var outputFamily = family{
	codes: map[Code]meta{
		2005: {severity: SeverityStyle, summary: "echo only repeats the output of a command substitution."},
		2028: {severity: SeverityInfo, summary: "echo does not reliably interpret backslash escapes."},
		2059: {severity: SeverityInfo, bug: true, summary: "A printf format contains an expansion, so data can be read as format directives."},
		2182: {severity: SeverityError, summary: "A printf format has no conversion specifiers, so its arguments are ignored."},
		2183: {severity: SeverityWarning, summary: "A printf format expects a different number of arguments than it is given."},
		2291: {severity: SeverityInfo, bug: true, summary: "Unquoted runs of spaces between echo arguments collapse to a single space."},
	},
	commands: []commandRule{
		{match: matchExact, names: []string{"echo"}, check: checkUselessEcho},
		{match: matchBase, names: []string{"echo"}, check: checkEchoEscapes},
		{match: matchBase, names: []string{"echo"}, check: checkEchoSpacing},
		{match: matchExact, names: []string{"printf"}, check: checkPrintfArguments},
	},
}

func (p *pass) unwrapEchoFix(c command) []Fix {
	cs, csOK := solePart(c.args[0]).(*syntax.CmdSubst)
	if !csOK {
		return nil
	}
	call, ok := cs.Stmts[0].Cmd.(*syntax.CallExpr)
	start, startOK := p.span(c.word)
	end, endOK := p.span(c.args[0])
	if !ok || !startOK || !endOK || cs.Backquotes || c.word.Lit() != "echo" || len(cs.Stmts[0].Redirs) > 0 || cs.Stmts[0].Negated || cs.Stmts[0].Background {
		return nil
	}
	inner, ok := p.span(call)
	if !ok {
		return nil
	}
	return []Fix{reviewFix("Run the command directly", textedit.Edit{Start: start.Start, End: end.End, NewText: p.text[inner.Start:inner.End]})}
}

func checkUselessEcho(p *pass, c command) {
	if len(c.args) != 1 {
		return
	}
	if !soleCommandOutput(c.args[0]) {
		return
	}
	p.reportNode(2005, c.args[0], "This echo only repeats the output of the command substitution; run the command directly instead.", p.unwrapEchoFix(c)...)
}

var echoEscapeShells = dialect.SetOf(dialect.Bash, dialect.Posix, dialect.Ksh)

var echoEscape = regexp.MustCompile(`\\([rntabefv\\']|[0-7]{1,3}|x[0-9A-Fa-f]{1,2})`)

func checkEchoEscapes(p *pass, c command) {
	if !echoEscapeShells.Has(p.dialect) {
		return
	}
	if c.options().Has("-e") {
		return
	}
	for _, arg := range c.args {
		if text, _ := parser.Text(arg, ""); echoEscape.MatchString(text) {
			p.reportNode(2028, arg, "echo does not reliably interpret backslash escapes like the one in this argument; use printf instead.")
		}
	}
}

const collapsedGap = 4

func checkEchoSpacing(p *pass, c command) {
	stmt, _ := p.parent().(*syntax.Stmt)
	for i := 1; i < len(c.args); i++ {
		end, start := parser.Offset(c.args[i-1].End()), parser.Offset(c.args[i].Pos())
		if c.args[i-1].End().Line() != c.args[i].Pos().Line() || visualWidth(p.text, end, start) < collapsedGap {
			continue
		}
		if stmt != nil && redirectsBetween(stmt, end, start) {
			continue
		}
		p.reportNode(2291, c.call, "Unquoted runs of spaces between echo arguments collapse to a single space; quote the text to keep the spacing.")
		return
	}
}

func visualWidth(text string, from, to int) int {
	if !strings.ContainsRune(text[from:to], '\t') {
		return to - from
	}
	start := lineStart(text, from)
	return visualColumn(text[start:to]) - visualColumn(text[start:from])
}

const tabStop = 8

func visualColumn(line string) int {
	col := 0
	for _, r := range line {
		if r == '\t' {
			col += tabStop - col%tabStop
			continue
		}
		col++
	}
	return col
}

func redirectsBetween(stmt *syntax.Stmt, from, to int) bool {
	for _, r := range stmt.Redirs {
		if at := parser.Offset(r.Pos()); at >= from && at < to {
			return true
		}
	}
	return false
}

type printfCall struct {
	format *syntax.Word
	data   []*syntax.Word
}

func printfArguments(args []*syntax.Word) (printfCall, bool) {
	for i := 0; i < len(args); i++ {
		lit, ok := parser.Literal(args[i])
		switch {
		case ok && lit == "--":
		case ok && lit == "-v":
			i++
		case ok && strings.HasPrefix(lit, "-v"):
		default:
			return printfCall{format: args[i], data: args[i+1:]}, true
		}
	}
	return printfCall{}, false
}

func checkPrintfArguments(p *pass, c command) {
	call, ok := printfArguments(c.args)
	if !ok {
		return
	}
	text, literal := parser.Text(call.format, "_")
	if !literal {
		if !strings.Contains(text, "%") && !p.constantWord(call.format) {
			var fixes []Fix
			if span, ok := p.span(call.format); ok && len(call.data) == 0 {
				fixes = append(fixes, reviewFix("Print it with the format '%s'", textedit.Edit{Start: span.Start, End: span.Start, NewText: "'%s' "}))
			}
			p.reportNode(2059, call.format, "This printf format contains an expansion; pass the data as an argument to a fixed format such as '%s'.", fixes...)
		}
		return
	}
	conversions := printfConversions(text)
	if len(conversions) == 0 {
		if len(call.data) > 0 {
			p.reportNode(2182, call.format, "This printf format has no conversion specifiers, so its arguments are ignored.")
		}
		return
	}
	if slices.ContainsFunc(call.data, mayExpandToSeveralWords) || argumentsFit(conversions, len(call.data)) {
		return
	}
	p.reportNode(2183, call.format, fmt.Sprintf("This printf format expects %s but is given %s.",
		plural(len(conversions), "argument"), plural(len(call.data), "argument")))
}

func (p *pass) constantWord(w *syntax.Word) bool {
	span, ok := p.span(w)
	return ok && p.constantParts(w.Parts, p.tree.ScopeAt(span.Start), map[string]bool{})
}

func (p *pass) constantParts(parts []syntax.WordPart, scope parser.ScopeID, seen map[string]bool) bool {
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit, *syntax.SglQuoted:
		case *syntax.DblQuoted:
			if !p.constantParts(part.Parts, scope, seen) {
				return false
			}
		case *syntax.ParamExp:
			if !parser.IsPlainParam(part) || !p.constantVariable(part.Param.Value, scope, seen) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (p *pass) constantVariable(name string, scope parser.ScopeID, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	defer delete(seen, name)
	defs := p.tree.Visible(name, scope)
	if defs.Len() == 0 {
		return false
	}
	for def := range defs.All() {
		assign, ok := def.Node.(*syntax.Assign)
		if !ok || assign.Array != nil || assign.Append || def.Kind != parser.DefAssign && def.Kind != parser.DefDeclare {
			return false
		}
		if assign.Value != nil && !p.constantParts(assign.Value.Parts, def.Within, seen) {
			return false
		}
	}
	return true
}

func argumentsFit(conversions []byte, given int) bool {
	if given > 0 && given%len(conversions) == 0 {
		return true
	}
	return given < len(conversions) && strings.Trim(string(conversions[given:]), "T") == ""
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

const (
	printfFlags             = "#-+ 0"
	printfConversion        = "diouxXfFeEgGaAcsbqQSC"
	printfNumericConversion = "diouxXfFeEgGaA*"
)

func printsOnlyNumbers(args []*syntax.Word) bool {
	call, ok := printfArguments(args)
	if !ok {
		return false
	}
	format, ok := parser.Literal(call.format)
	return ok && strings.Trim(string(printfConversions(format)), printfNumericConversion) == ""
}

var printfLengths = []string{"hh", "h", "ll", "l", "q", "L", "j", "z", "Z", "t"}

func printfConversions(format string) []byte {
	var out []byte
	for i := 0; i < len(format)-1; i++ {
		if format[i] != '%' {
			continue
		}
		switch format[i+1] {
		case '%':
			i++
			continue
		case '(':
			closing := strings.IndexByte(format[i+2:], ')')
			at := i + 2 + closing + 1
			if closing < 0 || at >= len(format) {
				return out
			}
			out = append(out, format[at])
			i = at
			continue
		}
		spec, end, ok := printfSpecifier(format, i+1)
		if !ok {
			out = append(out, format[i+1])
			i++
			continue
		}
		out = append(out, spec...)
		i = end
	}
	return out
}

func printfSpecifier(format string, j int) ([]byte, int, bool) {
	var out []byte
	for _, flag := range []byte(printfFlags) {
		if j < len(format) && format[j] == flag {
			j++
		}
	}
	j, out = printfWidth(format, j, out)
	if j < len(format) && format[j] == '.' {
		j++
	}
	j, out = printfWidth(format, j, out)
	for _, length := range printfLengths {
		if strings.HasPrefix(format[j:], length) {
			j += len(length)
			break
		}
	}
	if j >= len(format) || strings.IndexByte(printfConversion, format[j]) < 0 {
		return nil, 0, false
	}
	return append(out, format[j]), j, true
}

func printfWidth(format string, j int, out []byte) (int, []byte) {
	if j < len(format) && format[j] == '*' {
		return j + 1, append(out, '*')
	}
	for j < len(format) && format[j] >= '0' && format[j] <= '9' {
		j++
	}
	return j, out
}
