package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var commandWordFamily = family{
	codes: map[Code]meta{
		2211: {severity: SeverityWarning, summary: "A glob is used as a command name."},
		2215: {severity: SeverityWarning, summary: "A word that looks like an option is used as a command name."},
		2269: {severity: SeverityInfo, summary: "A variable is assigned its own value, which does nothing."},
		2270: {severity: SeverityError, summary: "Positional parameters cannot be assigned with =."},
		2271: {severity: SeverityError, summary: "A variable name computed from an expansion cannot be assigned with =."},
		2272: {severity: SeverityError, summary: "A command name contains ==, which suggests a comparison or assignment was meant."},
		2273: {severity: SeverityError, summary: "A line of equals signs looks like a leftover merge conflict marker or text border."},
		2274: {severity: SeverityError, summary: "A command cannot start with ===."},
		2275: {severity: SeverityError, summary: "A command cannot start with =."},
		2276: {severity: SeverityError, summary: "A word with = is run as a command because the part before = is not a valid variable name."},
		2277: {severity: SeverityError, summary: "$0 cannot be assigned like a variable in bash."},
		2278: {severity: SeverityError, summary: "$0 cannot be assigned in ksh."},
		2279: {severity: SeverityError, summary: "$0 cannot be assigned in this shell."},
		2280: {severity: SeverityError, summary: "$0 cannot be assigned in sh."},
		2281: {severity: SeverityError, summary: "The left side of an assignment has a $."},
		2282: {severity: SeverityError, summary: "Variable names cannot start with a digit, so the assignment runs as a command."},
		2283: {severity: SeverityError, summary: "Spaces around = turn an assignment into a command."},
		2284: {severity: SeverityError, summary: "== on its own is neither an assignment nor a command."},
		2285: {severity: SeverityError, summary: "Spaces around += turn an append into a command."},
		2286: {severity: SeverityError, summary: "The command name is an empty string."},
		2287: {severity: SeverityError, summary: "A command name ends in a slash, so it names a directory."},
		2288: {severity: SeverityWarning, summary: "A command name ends in a character that suggests a stray character or a missing space."},
		2289: {severity: SeverityError, summary: "A command name contains a tab or a line break."},
	},
	rules: []rule{
		{on: nodes(&syntax.CallExpr{}), node: checkCommandWords},
		{on: nodes(&syntax.CallExpr{}), node: checkSelfAssignment},
	},
}

const (
	minConflictMarker = 4
	maxConflictMarker = 12
	borderEquals      = 4
)

func checkCommandWords(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	p.checkEqualsAssignment(call)
	if len(call.Args) == 0 || p.continuesBracketTest(call) {
		return
	}
	name := call.Args[0]
	if isGlob(name) && !isNegatedSubshell(name) {
		p.reportNode(2211, name, "The command name "+p.nodeText(name)+" is a glob, so the command run depends on which files exist; quote it or fix the name.")
	}
	if lit, ok := name.Parts[0].(*syntax.Lit); ok && len(call.Assigns) == 0 && strings.HasPrefix(lit.Value, "-") && !p.timed() {
		p.reportNode(2215, name, p.nodeText(name)+" looks like an option but is used as a command name; did you forget [ ] around a test or break a line in the wrong place?")
	}
	p.checkEqualsInName(call)
	if name.Lit() != "[" {
		p.checkNameShape(name)
	}
	if len(call.Args) > 1 {
		p.checkSpacedAssignment(call)
	}
}

func isNegatedSubshell(name *syntax.Word) bool {
	glob, ok := name.Parts[0].(*syntax.ExtGlob)
	return ok && len(name.Parts) == 1 && glob.Op == syntax.GlobExcept
}

func (p *pass) continuesBracketTest(call *syntax.CallExpr) bool {
	if !endsWithBracket(call) {
		return false
	}
	prev, ok := p.previousStatement()
	return ok && isUnterminatedBracket(prev.Cmd)
}

func (p *pass) timed() bool {
	_, ok := p.ancestor(1).(*syntax.TimeClause)
	return ok
}

func (p *pass) checkEqualsAssignment(call *syntax.CallExpr) {
	for _, a := range call.Assigns {
		if a.Value == nil || len(a.Value.Parts) == 0 {
			continue
		}
		if lit, ok := a.Value.Parts[0].(*syntax.Lit); ok && strings.HasPrefix(lit.Value, "==") {
			p.reportDoubleEquals(a)
			return
		}
	}
}

func (p *pass) nodeText(n syntax.Node) string {
	span, ok := p.span(n)
	if !ok {
		return ""
	}
	return p.text[span.Start:span.End]
}

type equalsName struct {
	call    *syntax.CallExpr
	word    *syntax.Word
	lit     *syntax.Lit
	leading []syntax.WordPart
	before  string
	rest    string
}

func equalsNameOf(call *syntax.CallExpr) (equalsName, bool) {
	word := call.Args[0]
	for i, part := range word.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		before, after, found := strings.Cut(lit.Value, "=")
		if !found {
			continue
		}
		return equalsName{
			call:    call,
			word:    word,
			lit:     lit,
			leading: word.Parts[:i],
			before:  strings.TrimSuffix(before, "+"),
			rest:    after,
		}, true
	}
	return equalsName{}, false
}

func (e equalsName) text() string { return e.before + "=" + e.rest }

func (p *pass) reportDoubleEquals(name syntax.Node) {
	p.reportNode(2272, name, "The command name "+p.nodeText(name)+" contains ==; to compare values use [ a = b ], to assign use a single = without spaces.")
}

func (p *pass) checkEqualsInName(call *syntax.CallExpr) {
	e, ok := equalsNameOf(call)
	if !ok {
		return
	}
	s := e.text()
	switch {
	case len(e.leading) == 0 && (strings.HasPrefix(s, "-") || parser.IsName(e.before)):
		return
	case len(e.leading) == 0 && e.before == "":
		p.reportLeadingEquals(e)
		return
	case strings.Contains(s, "=="):
		p.reportDoubleEquals(e.word)
		return
	}
	if pe, ok := soleParamExp(e.leading); ok && e.before == "" {
		p.checkDollarAssignment(e, pe)
		return
	}
	if len(e.leading) == 0 {
		p.checkLiteralAssignment(e)
		return
	}
	if dynamicName(e) {
		p.reportNode(2271, e.word, "Variable names cannot be computed like this; use declare \"name=value\", printf -v, or an associative array.")
		return
	}
	p.reportEqualsGeneric(e)
}

func (p *pass) reportLeadingEquals(e equalsName) {
	span, ok := p.span(e.call)
	if !ok {
		return
	}
	lit := e.word.Lit()
	switch {
	case len(e.call.Assigns) == 0 && len(e.call.Args) == 1 && isConflictMarker(lit):
		p.report(2273, span, "This line of equals signs looks like a leftover merge conflict marker or a text border; remove it.")
	case strings.HasPrefix(e.text(), "==="):
		p.report(2274, span, "A command cannot start with ===; if this is a separator or heading, turn it into a comment.")
	default:
		p.reportNode(2275, e.word, "A command cannot start with =; if this was meant as an assignment, remove the spaces or line break before the equals sign.")
	}
}

func isConflictMarker(lit string) bool {
	return len(lit) >= minConflictMarker && len(lit) <= maxConflictMarker && strings.Trim(lit, "=") == ""
}

func soleParamExp(parts []syntax.WordPart) (*syntax.ParamExp, bool) {
	if len(parts) != 1 {
		return nil, false
	}
	pe, ok := parts[0].(*syntax.ParamExp)
	return pe, ok
}

func (p *pass) checkDollarAssignment(e equalsName, pe *syntax.ParamExp) {
	switch {
	case pe.Param == nil || pe.Length:
		p.reportEqualsGeneric(e)
	case parser.IsDigits(pe.Param.Value) && parser.IsPlainParam(pe) && p.dialect.Features().AssignsPositionals:
		p.reportNode(2281, pe, "Drop the $ on the left side of an assignment: write "+pe.Param.Value+"=value, not "+p.nodeText(pe)+"=value.", dollarRemoval(p, pe)...)
	case pe.Param.Value == "0" && parser.IsPlainParam(pe):
		p.reportDollarZero(pe)
	case isPositional(pe.Param.Value) && pe.Param.Value != "@" && pe.Param.Value != "*" && parser.IsPlainParam(pe):
		p.reportNode(2270, pe, "Positional parameters cannot be assigned with =; use set -- to replace them.")
	case parser.IsName(pe.Param.Value) && (parser.IsPlainParam(pe) || isIndexedParam(pe)):
		p.reportNode(2281, pe, "Drop the $ on the left side of an assignment: write "+p.assignedName(pe)+"=value, not "+p.nodeText(pe)+"=value.", dollarRemoval(p, pe)...)
	default:
		p.reportNode(2271, pe, "Variable names cannot be computed like this; use declare \"name=value\", printf -v, or an associative array.")
	}
}

func (p *pass) assignedName(pe *syntax.ParamExp) string {
	text := p.nodeText(pe)
	if pe.Short {
		return strings.TrimPrefix(text, "$")
	}
	return strings.TrimSuffix(strings.TrimPrefix(text, "${"), "}")
}

func dollarRemoval(p *pass, pe *syntax.ParamExp) []Fix {
	span, ok := p.span(pe)
	if !ok {
		return nil
	}
	if pe.Short {
		return []Fix{reviewFix("Remove the $", textedit.Edit{Start: span.Start, End: span.Start + 1})}
	}
	return []Fix{reviewFix("Remove the $", textedit.Edit{Start: span.Start, End: span.Start + 2}, textedit.Edit{Start: span.End - 1, End: span.End})}
}

func isIndexedParam(pe *syntax.ParamExp) bool {
	if pe.Index == nil {
		return false
	}
	bare := *pe
	bare.Index = nil
	return parser.IsPlainParam(&bare)
}

func (p *pass) reportDollarZero(at syntax.Node) {
	switch p.dialect {
	case dialect.Bash:
		span, ok := p.span(at)
		if !ok {
			return
		}
		replace := span
		if _, literal := at.(*syntax.Lit); literal {
			replace.End = replace.Start + 1
		}
		p.report(2277, span, "$0 cannot be assigned like a variable; in bash, assign BASH_ARGV0 to change it.", reviewFix("Assign BASH_ARGV0", textedit.Edit{Start: replace.Start, End: replace.End, NewText: "BASH_ARGV0"}))
	case dialect.Ksh:
		p.reportNode(2278, at, "$0 cannot be assigned in ksh.")
	case dialect.Dash, dialect.Busybox:
		p.reportNode(2279, at, "$0 cannot be assigned in "+shellLabel(p.dialect)+".")
	default:
		p.reportNode(2280, at, "$0 cannot be assigned, and sh offers no portable way to change it.")
	}
}

func (p *pass) checkLiteralAssignment(e equalsName) {
	digits := len(e.before) - len(strings.TrimLeft(e.before, "0123456789"))
	switch {
	case digits > 0 && digits == len(e.before) && p.dialect.Features().AssignsPositionals:
	case e.before == "0":
		p.reportDollarZero(e.lit)
	case digits > 0 && digits == len(e.before) && digits <= 2:
		p.reportNode(2270, e.lit, "Positional parameters cannot be assigned with =; use set -- to replace them.")
	case digits > 0 && digits < len(e.before) && parser.IsName("_"+e.before):
		p.reportNode(2282, e.word, "Variable names cannot start with a digit, so "+e.before+" is run as a command; rename the variable.")
	default:
		p.reportEqualsGeneric(e)
	}
}

func dynamicName(e equalsName) bool {
	var name strings.Builder
	for _, part := range e.leading {
		switch part := part.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(part.Value, "*?[") {
				return false
			}
			name.WriteString(part.Value)
		case *syntax.SglQuoted, *syntax.DblQuoted, *syntax.ExtGlob, *syntax.BraceExp:
			return false
		case *syntax.ParamExp:
			if kindOf(part) == expandsElements {
				return false
			}
			name.WriteByte('x')
		default:
			name.WriteByte('x')
		}
	}
	name.WriteString(e.before)
	return parser.IsName(name.String())
}

func (p *pass) reportEqualsGeneric(e equalsName) {
	p.reportNode(2276, e.word, p.nodeText(e.word)+" is run as a command because the part before = is not a valid variable name; quote the = if the command is intended.")
}

var suspiciousEndings = map[byte]string{
	'\\': `'\'`, '.': `'.'`, ',': `','`, '(': `'('`, ')': `')'`, '[': `'['`, ']': `']'`, '{': `'{'`, '}': `'}'`,
	'<': `'<'`, '>': `'>'`, '#': `'#'`, '"': "doublequote", '\'': "apostrophe", '%': `'%'`, ' ': "space",
}

func (p *pass) checkNameShape(name *syntax.Word) {
	shape := literalShape(name)
	if shape == "" {
		p.reportNode(2286, name, "The command name is an empty string; this is usually an empty variable or a stray pair of quotes.")
		return
	}
	last := shape[len(shape)-1]
	switch {
	case last == '/':
		if shape != "//" {
			p.reportNode(2287, name, "The command name "+p.nodeText(name)+" ends in a slash, so it names a directory rather than a program.")
		}
		return
	case suspiciousEndings[last] != "":
		if shape != " " && shape != "." && !keywordGluedToComment(shape) {
			p.reportNode(2288, name, "The command name "+p.nodeText(name)+" ends in "+suspiciousEndings[last]+"; check for a stray character or a missing space.")
		}
		return
	case strings.ContainsRune(shape, '\t'):
		p.reportNode(2289, name, "The command name contains a tab; check the quoting around it.")
	case strings.ContainsRune(shape, '\n'):
		p.reportNode(2289, name, "The command name contains a line break; check the quoting around it.")
	}
}

var reservedWords = setOf("!", "{", "}", "case", "do", "done", "elif", "else", "esac", "fi", "for", "function", "if", "in", "select", "then", "until", "while")

func keywordGluedToComment(shape string) bool {
	stem, glued := strings.CutSuffix(shape, "#")
	return glued && reservedWords[stem]
}

func literalShape(w *syntax.Word) string {
	var sb strings.Builder
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			unquotedShape(&sb, part.Value)
		default:
			text, _ := parser.Text(&syntax.Word{Parts: []syntax.WordPart{part}}, "x")
			sb.WriteString(text)
		}
	}
	return sb.String()
}

func unquotedShape(sb *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s):
			i++
			sb.WriteByte(s[i])
		case c == '*' || c == '?':
			sb.WriteByte('x')
		case c == '[' && strings.IndexByte(s[i+1:], ']') >= 0:
			i += 1 + strings.IndexByte(s[i+1:], ']')
			sb.WriteByte('x')
		default:
			sb.WriteByte(c)
		}
	}
}

func (p *pass) checkSpacedAssignment(call *syntax.CallExpr) {
	second := call.Args[1]
	lit, ok := second.Parts[0].(*syntax.Lit)
	if !ok {
		return
	}
	if strings.HasPrefix(lit.Value, "+=") {
		p.reportNode(2285, call, "This looks like an append, but spaces around += turn it into a command; write name+=value without spaces.")
		return
	}
	switch equals := len(lit.Value) - len(strings.TrimLeft(lit.Value, "=")); {
	case equals == 1:
		p.reportNode(2283, lit, "This looks like an assignment, but spaces around = turn it into a command; remove the spaces, or use [ ] to compare.")
	case equals > 1 && equals < borderEquals:
		p.reportNode(2284, call, "== is not an assignment or a command; to compare values use [ a = b ] or [[ a == b ]].")
	}
}

func checkSelfAssignment(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || len(call.Args) > 0 {
		return
	}
	for _, a := range call.Assigns {
		if a.Append || a.Naked || a.Index != nil || a.Array != nil || a.Name == nil || a.Value == nil {
			continue
		}
		if pe, ok := soleBareExpansion(a.Value); ok && pe.Param.Value == a.Name.Value {
			p.reportNode(2269, a, a.Name.Value+" is assigned its own value, which does nothing; remove the assignment or fix the name.")
		}
	}
}
