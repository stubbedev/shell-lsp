package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var sedSubstitutionFamily = family{
	codes: map[Code]meta{
		2001: {severity: SeverityStyle, summary: "sed runs on a single value that a ${var//pattern/replacement} expansion could handle."},
	},
	nodes: []nodeRule{onNodes(checkSedSubstitution, &syntax.BinaryCmd{}, &syntax.Stmt{})},
}

var substitutionShells = dialect.SetOf(dialect.Bash, dialect.Ksh, dialect.Zsh)

const sedSubstitutionMessage = "This runs sed on a single value; use the ${variable//pattern/replacement} expansion instead."

func checkSedSubstitution(p *pass, n syntax.Node) {
	if !substitutionShells.Has(p.dialect) {
		return
	}
	switch n := n.(type) {
	case *syntax.BinaryCmd:
		if p.isTwoStageEchoSed(n) {
			if span, ok := p.span(n); ok {
				p.report(2001, span, sedSubstitutionMessage, p.sedSubstitutionFix(p.parent())...)
			}
		}
	case *syntax.Stmt:
		call, ok := n.Cmd.(*syntax.CallExpr)
		if ok && hasHereString(n) && isSimpleSedSubstitution(call) {
			if span, ok := p.span(n); ok {
				p.report(2001, span, sedSubstitutionMessage, p.sedSubstitutionFix(n)...)
			}
		}
	}
}

func (p *pass) sedSubstitutionFix(node syntax.Node) []Fix {
	stmt, ok := node.(*syntax.Stmt)
	if !ok {
		return nil
	}
	cs, ok := p.enclosingSubstitution(stmt)
	if !ok {
		return nil
	}
	f, ok := filterOfValue(stmt)
	if !ok || !f.filter.isBase("sed") {
		return nil
	}
	sub, ok := parseSedSubstitution(parser.LiteralChars(f.filter.args[len(f.filter.args)-1]))
	if !ok {
		return nil
	}
	span, ok := p.span(cs)
	if !ok {
		return nil
	}
	rewrite := sub.expansion(f.name, p.dialect)
	return []Fix{reviewFix("Rewrite as "+rewrite, textedit.Edit{Start: span.Start, End: span.End, NewText: rewrite})}
}

func (p *pass) enclosingSubstitution(stmt *syntax.Stmt) (*syntax.CmdSubst, bool) {
	for depth := 0; ; depth++ {
		switch n := p.ancestor(depth).(type) {
		case *syntax.CmdSubst:
			sole, ok := soleStatement(n)
			return n, ok && sole == stmt
		case *syntax.Stmt, *syntax.BinaryCmd:
			if n != stmt {
				return nil, false
			}
		default:
			return nil, false
		}
	}
}

type sedSubstitution struct {
	pattern, replacement string
	all, start, end      bool
}

func sedFields(script string) ([]string, bool) {
	if len(script) < 2 || script[0] != 's' || script[1] == '\\' || script[1] == '\n' {
		return nil, false
	}
	delim := script[1]
	var fields []string
	var field strings.Builder
	for i := 2; i < len(script); i++ {
		switch c := script[i]; {
		case c == '\\' && i+1 < len(script) && script[i+1] == delim:
			field.WriteByte(delim)
			i++
		case c == '\\' && i+1 < len(script):
			field.WriteString(script[i : i+2])
			i++
		case c == delim && len(fields) < 2:
			fields = append(fields, field.String())
			field.Reset()
		default:
			field.WriteByte(c)
		}
	}
	return append(fields, field.String()), len(fields) == 2
}

func parseSedSubstitution(script string) (sedSubstitution, bool) {
	fields, ok := sedFields(script)
	if !ok || fields[2] != "" && fields[2] != "g" {
		return sedSubstitution{}, false
	}
	sub := sedSubstitution{all: fields[2] == "g"}
	pattern := fields[0]
	if rest, ok := strings.CutPrefix(pattern, "^"); ok {
		pattern, sub.start = rest, true
	}
	if rest, ok := strings.CutSuffix(pattern, "$"); ok && !strings.HasSuffix(rest, `\`) {
		pattern, sub.end = rest, true
	}
	if sub.pattern, ok = unescapeSed(pattern, breSpecial); !ok || sub.pattern == "" || sub.start && sub.end {
		return sedSubstitution{}, false
	}
	if sub.replacement, ok = unescapeSed(fields[1], `\&`); !ok {
		return sedSubstitution{}, false
	}
	return sub, true
}

func unescapeSed(field, special string) (string, bool) {
	var sb strings.Builder
	for i := 0; i < len(field); i++ {
		c := field[i]
		switch {
		case c == '\\' && i+1 < len(field) && strings.IndexByte(special+"/", field[i+1]) >= 0:
			i++
			sb.WriteByte(field[i])
		case strings.IndexByte(special, c) >= 0 || c == '\n':
			return "", false
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String(), true
}

func (s sedSubstitution) expansion(name string, d dialect.Dialect) string {
	op := "/"
	switch {
	case s.start:
		op = "/#"
	case s.end:
		op = "/%"
	case s.all:
		op = "//"
	}
	var pattern, replacement strings.Builder
	for i := range len(s.pattern) {
		pattern.WriteString(escapedGlobChar(s.pattern[i]))
	}
	for i := range len(s.replacement) {
		replacement.WriteString(replacementChar(s.replacement[i], d))
	}
	if replacement.Len() == 0 {
		return "${" + name + op + pattern.String() + "}"
	}
	return "${" + name + op + pattern.String() + "/" + replacement.String() + "}"
}

func (p *pass) isTwoStageEchoSed(bin *syntax.BinaryCmd) bool {
	if !pipes(bin.Op) {
		return false
	}
	if outer, ok := p.ancestor(1).(*syntax.BinaryCmd); ok && pipes(outer.Op) {
		return false
	}
	echo, ok := bin.X.Cmd.(*syntax.CallExpr)
	if !ok || !isEchoOfOneExpansion(echo) {
		return false
	}
	sed, ok := bin.Y.Cmd.(*syntax.CallExpr)
	return ok && isSimpleSedSubstitution(sed)
}

func isEchoOfOneExpansion(call *syntax.CallExpr) bool {
	if len(call.Args) != 2 {
		return false
	}
	if name, _ := parser.CallName(call); name != "echo" {
		return false
	}
	parts := call.Args[1].Parts
	if len(parts) == 1 {
		if dq, ok := parts[0].(*syntax.DblQuoted); ok {
			parts = dq.Parts
		}
	}
	return len(parts) == 1 && isExpansionPart(parts[0])
}

func isExpansionPart(part syntax.WordPart) bool {
	switch part.(type) {
	case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp:
		return true
	}
	return false
}

func isSimpleSedSubstitution(call *syntax.CallExpr) bool {
	if len(call.Args) != 2 && len(call.Args) != 3 {
		return false
	}
	if name, _ := parser.CallName(call); name != "sed" {
		return false
	}
	if flag, _ := parser.Literal(call.Args[1]); len(call.Args) == 3 && flag != "-e" {
		return false
	}
	return isSimpleSubstitute(parser.LiteralChars(call.Args[len(call.Args)-1]))
}

func isSimpleSubstitute(expr string) bool {
	if len(expr) < 2 || expr[0] != 's' || strings.ContainsRune(expr, '\n') {
		return false
	}
	return strings.Count(expr[2:], expr[1:2]) == 2
}

func hasHereString(stmt *syntax.Stmt) bool {
	for _, r := range stmt.Redirs {
		if r.Op == syntax.WordHdoc {
			return true
		}
	}
	return false
}
