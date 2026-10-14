package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	codeShellDoesIt     = nativeBase + 2020
	codeCatSubstitution = nativeBase + 2021
)

var forksFamily = family{
	codes: map[Code]meta{
		codeShellDoesIt:     {severity: SeverityStyle},
		codeCatSubstitution: {severity: SeverityStyle, optional: optionalUselessCat},
	},
	nodes: []nodeRule{onNode(checkForkingSubstitution), onNode(checkGrepForLiteral)},
}

var caseShells = dialect.SetOf(dialect.Bash, dialect.Zsh)

type valueFilter struct {
	name   string
	filter command
}

func soleStatement(cs *syntax.CmdSubst) (*syntax.Stmt, bool) {
	if len(cs.Stmts) != 1 || cs.TempFile || cs.ReplyVar {
		return nil, false
	}
	stmt := cs.Stmts[0]
	return stmt, !stmt.Negated && !stmt.Background && !stmt.Coprocess && stmt.Cmd != nil
}

func filterOfValue(stmt *syntax.Stmt) (valueFilter, bool) {
	if bin, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && bin.Op == syntax.Pipe && len(stmt.Redirs) == 0 {
		name, ok := echoedValue(bin.X)
		if !ok || len(bin.Y.Redirs) > 0 {
			return valueFilter{}, false
		}
		filter, ok := plainCommand(bin.Y)
		return valueFilter{name: name, filter: filter}, ok
	}
	if len(stmt.Redirs) != 1 || stmt.Redirs[0].Op != syntax.WordHdoc {
		return valueFilter{}, false
	}
	name, ok := plainParameter(stmt.Redirs[0].Word, false)
	if !ok {
		return valueFilter{}, false
	}
	filter, ok := plainCommand(&syntax.Stmt{Cmd: stmt.Cmd})
	return valueFilter{name: name, filter: filter}, ok
}

func plainCommand(stmt *syntax.Stmt) (command, bool) {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 || len(stmt.Redirs) > 0 || stmt.Negated {
		return command{}, false
	}
	return commandOf(call)
}

func echoedValue(stmt *syntax.Stmt) (string, bool) {
	c, ok := plainCommand(stmt)
	if !ok {
		return "", false
	}
	args, value := c.args, (*syntax.Word)(nil)
	switch {
	case c.is("echo") && len(args) == 1:
		value = args[0]
	case c.is("printf") && len(args) == 2 && (parser.LiteralIs(args[0], `%s\n`) || parser.LiteralIs(args[0], "%s")):
		value = args[1]
	default:
		return "", false
	}
	return plainParameter(value, true)
}

func plainParameter(w *syntax.Word, mustQuote bool) (string, bool) {
	if w == nil || len(w.Parts) != 1 {
		return "", false
	}
	if _, quoted := w.Parts[0].(*syntax.DblQuoted); mustQuote && !quoted {
		return "", false
	}
	pe, ok := parser.SoleParam(w)
	if !ok || !parser.IsPlainParam(pe) {
		return "", false
	}
	name := pe.Param.Value
	if kind, _ := parser.ParamKindOf(name); !parser.IsName(name) && kind != parser.ParamPositional {
		return "", false
	}
	return name, true
}

func (p *pass) suggest(code Code, cs *syntax.CmdSubst, message, rewrite string) {
	span, ok := p.span(cs)
	if !ok {
		return
	}
	p.report(code, span, message, reviewFix("Rewrite as "+rewrite, textedit.Edit{Start: span.Start, End: span.End, NewText: rewrite}))
}

func checkForkingSubstitution(p *pass, cs *syntax.CmdSubst) {
	stmt, ok := soleStatement(cs)
	if !ok {
		return
	}
	if c, ok := plainCommand(stmt); ok && len(stmt.Redirs) == 0 {
		p.forkingCommand(cs, c)
		return
	}
	if f, ok := filterOfValue(stmt); ok {
		p.forkingFilter(cs, f)
	}
}

func (p *pass) forkingCommand(cs *syntax.CmdSubst, c command) {
	switch {
	case c.isBase("basename") && len(c.args) == 1:
		name, ok := plainParameter(c.args[0], true)
		if !ok {
			return
		}
		if p.dialect == dialect.Zsh {
			p.suggest(codeShellDoesIt, cs, "basename runs a separate process to drop the directory part; ${"+name+":t} does it in the shell, though it gives nothing instead of / for the root.", "${"+name+":t}")
			return
		}
		p.suggest(codeShellDoesIt, cs, "basename runs a separate process to drop the directory part; ${"+name+"##*/} does it in the shell, though it gives nothing for a path that ends in /.", "${"+name+"##*/}")
	case c.isBase("dirname") && len(c.args) == 1 && p.dialect == dialect.Zsh:
		name, ok := plainParameter(c.args[0], true)
		if !ok {
			return
		}
		p.suggest(codeShellDoesIt, cs, "dirname runs a separate process to drop the last path component; ${"+name+":h} does the same in the shell.", "${"+name+":h}")
	case c.is("pwd") && len(c.args) == 0:
		p.suggest(codeShellDoesIt, cs, "$(pwd) starts a subshell to print the working directory, which the shell already keeps in $PWD.", "$PWD")
	case c.isBase("cat") && len(c.args) == 1 && substitutionShells.Has(p.dialect):
		file := c.args[0]
		if lit, ok := parser.Literal(file); ok && strings.HasPrefix(lit, "-") || mayExpandToSeveralWords(file) {
			return
		}
		span, ok := p.span(file)
		if !ok {
			return
		}
		p.suggest(codeCatSubstitution, cs, "$(cat file) runs cat to read a file; $(<file) lets the shell read it without another process.", "$(<"+p.spanText(span)+")")
	}
}

func (p *pass) forkingFilter(cs *syntax.CmdSubst, f valueFilter) {
	args, ok := f.filter.literalArgs()
	if !ok {
		return
	}
	switch {
	case f.filter.isBase("tr"):
		p.forkingTr(cs, f.name, args)
	case f.filter.isBase("cut"):
		p.forkingCut(cs, f.name, args)
	}
}

var (
	lowerSets = map[string]bool{"a-z": true, "[a-z]": true, "[:lower:]": true}
	upperSets = map[string]bool{"A-Z": true, "[A-Z]": true, "[:upper:]": true}
)

func (p *pass) forkingTr(cs *syntax.CmdSubst, name string, args []string) {
	switch {
	case len(args) == 2 && lowerSets[args[0]] && upperSets[args[1]] && caseShells.Has(p.dialect):
		rewrite := "${" + name + "^^}"
		if p.dialect == dialect.Zsh {
			rewrite = "${(U)" + name + "}"
		}
		p.suggest(codeShellDoesIt, cs, "tr runs a separate process to uppercase one value; "+rewrite+" does it in the shell, though it also converts letters outside a-z.", rewrite)
	case len(args) == 2 && upperSets[args[0]] && lowerSets[args[1]] && caseShells.Has(p.dialect):
		rewrite := "${" + name + ",,}"
		if p.dialect == dialect.Zsh {
			rewrite = "${(L)" + name + "}"
		}
		p.suggest(codeShellDoesIt, cs, "tr runs a separate process to lowercase one value; "+rewrite+" does it in the shell, though it also converts letters outside A-Z.", rewrite)
	case len(args) == 2 && args[0] == "-d" && substitutionShells.Has(p.dialect):
		from, ok := trChar(args[1])
		if !ok {
			return
		}
		rewrite := "${" + name + "//" + escapedGlobChar(from) + "}"
		p.suggest(codeShellDoesIt, cs, "tr runs a separate process to delete one character from one value; "+rewrite+" does it in the shell.", rewrite)
	case len(args) == 2 && substitutionShells.Has(p.dialect):
		from, okFrom := trChar(args[0])
		to, okTo := trChar(args[1])
		if !okFrom || !okTo {
			return
		}
		rewrite := "${" + name + "//" + escapedGlobChar(from) + "/" + replacementChar(to, p.dialect) + "}"
		p.suggest(codeShellDoesIt, cs, "tr runs a separate process to replace one character in one value; "+rewrite+" does it in the shell.", rewrite)
	}
}

var trEscapes = map[string]byte{`\n`: '\n', `\t`: '\t', `\\`: '\\'}

func trChar(set string) (byte, bool) {
	if c, ok := trEscapes[set]; ok {
		return c, true
	}
	if len(set) != 1 || set[0] == '\\' || set[0] >= 0x80 {
		return 0, false
	}
	return set[0], true
}

var (
	ansiCEscapes = map[byte]string{'\n': `$'\n'`, '\t': `$'\t'`}
	globSpecial  = `\*?[]/}{$"'` + "`" + `()|#^~<>!@+&%`
)

func escapedGlobChar(c byte) string {
	if quoted, ok := ansiCEscapes[c]; ok {
		return quoted
	}
	if strings.IndexByte(globSpecial, c) >= 0 {
		return `\` + string(c)
	}
	return string(c)
}

func replacementChar(c byte, d dialect.Dialect) string {
	if c == '&' && d != dialect.Bash {
		return "&"
	}
	return escapedGlobChar(c)
}

func (p *pass) forkingCut(cs *syntax.CmdSubst, name string, args []string) {
	delimiter, fields, ok := cutArguments(args)
	if !ok || len(delimiter) != 1 || delimiter[0] >= 0x80 {
		return
	}
	d := escapedGlobChar(delimiter[0])
	switch fields {
	case "1":
		rewrite := "${" + name + "%%" + d + "*}"
		p.suggest(codeShellDoesIt, cs, "cut runs a separate process to take the first field of one value; "+rewrite+" does it in the shell, the same for a value on a single line.", rewrite)
	case "2-":
		rewrite := "${" + name + "#*" + d + "}"
		p.suggest(codeShellDoesIt, cs, "cut runs a separate process to drop the first field of one value; "+rewrite+" does it in the shell, the same for a value on a single line.", rewrite)
	}
}

func cutArguments(args []string) (delimiter, fields string, ok bool) {
	var haveD, haveF bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var value *string
		switch {
		case strings.HasPrefix(arg, "-d") && !haveD:
			value, haveD = &delimiter, true
		case strings.HasPrefix(arg, "-f") && !haveF:
			value, haveF = &fields, true
		default:
			return "", "", false
		}
		if len(arg) > 2 {
			*value = arg[2:]
			continue
		}
		if i+1 == len(args) {
			return "", "", false
		}
		i++
		*value = args[i]
	}
	return delimiter, fields, haveD && haveF
}

const breSpecial = `.[]*^$\`

func checkGrepForLiteral(p *pass, stmt *syntax.Stmt) {
	if !substitutionShells.Has(p.dialect) {
		return
	}
	f, ok := filterOfValue(stmt)
	if !ok || !f.filter.isBase("grep") {
		return
	}
	args, ok := f.filter.literalArgs()
	if !ok || len(args) < 2 {
		return
	}
	quiet, fixed := false, false
	for _, opt := range args[:len(args)-1] {
		switch opt {
		case "-q", "--quiet", "--silent":
			quiet = true
		case "-F", "--fixed-strings":
			fixed = true
		case "-qF", "-Fq":
			quiet, fixed = true, true
		default:
			return
		}
	}
	needle := args[len(args)-1]
	if !quiet || needle == "" || strings.HasPrefix(needle, "-") || strings.ContainsRune(needle, '\n') || !fixed && strings.ContainsAny(needle, breSpecial) {
		return
	}
	span, ok := p.span(stmt.Cmd)
	for _, r := range stmt.Redirs {
		redirect, inSpan := p.span(r)
		ok = ok && inSpan
		span = parser.Span{Start: min(span.Start, redirect.Start), End: max(span.End, redirect.End)}
	}
	if !ok {
		return
	}
	rewrite := "[[ $" + braced(f.name) + " == *" + doubleQuoted(needle) + "* ]]"
	p.report(codeShellDoesIt, span, "This pipes one value through grep to test whether it contains a fixed string; "+rewrite+" tests it in the shell.",
		reviewFix("Rewrite as "+rewrite, textedit.Edit{Start: span.Start, End: span.End, NewText: rewrite}))
}

func braced(name string) string {
	if len(name) > 1 && !parser.IsName(name) {
		return "{" + name + "}"
	}
	return name
}

func doubleQuoted(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for i := range len(s) {
		if strings.IndexByte("\\\"$`", s[i]) >= 0 {
			sb.WriteByte('\\')
		}
		sb.WriteByte(s[i])
	}
	sb.WriteByte('"')
	return sb.String()
}
