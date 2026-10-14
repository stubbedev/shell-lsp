package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var quotingFamily = family{
	codes: map[Code]meta{
		2006: {severity: SeverityStyle, tag: TagDeprecated, summary: "Backtick command substitution is legacy syntax that nests and escapes poorly."},
		2046: {severity: SeverityWarning, summary: "An unquoted command substitution is split into words and expanded as a glob."},
		2048: {severity: SeverityWarning, summary: "An unquoted $@ or ${array[@]} joins the elements and splits them again."},
		2068: {severity: SeverityError, summary: "An unquoted array expansion splits its elements again."},
		2086: {severity: SeverityInfo, bug: true, summary: "An unquoted expansion is split on whitespace and expanded as a glob."},
		2206: {severity: SeverityWarning, summary: "An unquoted expansion is split into words and globbed when building an array."},
		2207: {severity: SeverityWarning, summary: "Command output is split on whitespace and globbed when building an array."},
		2223: {severity: SeverityInfo, bug: true, summary: "An unquoted ${var:=default} is globbed on every run."},
		2248: {severity: SeverityStyle, optional: optionalQuoteSafeVariables, summary: "A variable that holds no spaces or globs today is expanded unquoted."},
	},
	nodes: []nodeRule{
		onNode(checkUnquotedExpansion),
		onNode(checkUnquotedSubstitution),
	},
}

var spacelessSpecials = setOf("-", "$", "?", "!", "#")

func (p *pass) quoteFix(span parser.Span, safety Safety) Fix {
	return Fix{Title: "Double-quote " + snippet(p.spanText(span)), Safety: safety, Edits: []textedit.Edit{
		{Start: span.Start, End: span.Start, NewText: `"`},
		{Start: span.End, End: span.End, NewText: `"`},
	}}
}

func (p *pass) soleExpansionQuoteFix(w *syntax.Word, span parser.Span) []Fix {
	switch parser.SolePart(w).(type) {
	case *syntax.ParamExp, *syntax.CmdSubst:
		return []Fix{p.quoteFix(span, Review)}
	}
	return nil
}

func checkUnquotedExpansion(p *pass, pe *syntax.ParamExp) {
	if pe.Param == nil || !p.dialect.Features().SplitsUnquoted {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	text := p.spanText(span)
	context := p.contextOf(pe)
	if context == contextArrayElement {
		if !pe.Length && !isAlternativeExpansion(pe) && !spacelessVariables[pe.Param.Value] {
			p.report(2206, span, text+" is split into words and globbed when building this array; quote it, or split it deliberately with read -a or mapfile.")
		}
		return
	}
	if context == contextQuoted || pe.Length || isAlternativeExpansion(pe) {
		return
	}
	switch kindOf(pe) {
	case expandsElements:
		if p.firstArrayPart(pe) {
			p.report(2068, span, "Quote this array expansion so each element stays one word: \""+text+"\".", p.quoteFix(span, Safe))
		}
		return
	case expandsJoined:
		if p.soleWordPart(pe) {
			p.report(2048, span, "Unquoted "+text+" joins the elements and splits them again; use \"$@\" or \"${array[@]}\" to keep them intact.",
				reviewFix("Expand each element: \""+strings.Replace(text, "*", "@", 1)+"\"", textedit.Edit{Start: span.Start, End: span.End, NewText: `"` + strings.Replace(text, "*", "@", 1) + `"`}))
		}
	case expandsScalar:
	}
	if context != contextSplit || spacelessSpecials[pe.Param.Value] || p.namesAssignment(pe) {
		return
	}
	defaultAssignment := assignsParameter(pe) && p.isArgumentOf(":")
	p.withFlow(func(flow *flowResult) {
		switch {
		case flow.expansion(pe).clean():
			p.report(2248, span, text+" holds no spaces or globs today, but quoting it keeps it safe if that changes: \""+text+"\".", p.quoteFix(span, Safe))
		case defaultAssignment:
			p.report(2223, span, "This default assignment is expanded unquoted, so its value is globbed on every run; quote it.", p.quoteFix(span, Safe))
		default:
			p.report(2086, span, "Unquoted "+text+" is split on whitespace and expanded as a glob; double-quote it.", p.quoteFix(span, Review))
		}
	})
}

func (p *pass) firstArrayPart(pe *syntax.ParamExp) bool {
	word, ok := p.parent().(*syntax.Word)
	if !ok {
		return false
	}
	for _, part := range word.Parts {
		if other, ok := part.(*syntax.ParamExp); ok && kindOf(other) == expandsElements && !isAlternativeExpansion(other) {
			return other == pe
		}
	}
	return false
}

func (p *pass) soleWordPart(part syntax.WordPart) bool {
	word, ok := p.parent().(*syntax.Word)
	return ok && len(word.Parts) == 1 && word.Parts[0] == part
}

func (p *pass) namesAssignment(pe *syntax.ParamExp) bool {
	word, ok := p.parent().(*syntax.Word)
	if !ok || len(word.Parts) < 2 || word.Parts[0] != pe {
		return false
	}
	if call, ok := p.ancestor(1).(*syntax.CallExpr); !ok || call.Args[0] != word {
		return false
	}
	lit, ok := word.Parts[1].(*syntax.Lit)
	return ok && strings.HasPrefix(lit.Value, "=")
}

func checkUnquotedSubstitution(p *pass, cs *syntax.CmdSubst) {
	if len(cs.Stmts) == 0 {
		return
	}
	span, ok := p.span(cs)
	if !ok {
		return
	}
	if cs.Backquotes {
		fix := backtickFix(p, cs, span)
		at := span
		if at.Start > 0 && p.text[at.Start-1] == '\\' {
			at.Start--
		}
		p.report(2006, at, "Backtick command substitution is legacy syntax that nests and escapes poorly; use $(...).", fix...)
	}
	switch p.contextOf(cs) {
	case contextSplit:
		if !producesSafeWords(cs) {
			p.report(2046, span, "The output of this command substitution is "+p.substitutionFate(cs, "split into words")+"; quote it.", p.quoteFix(span, Review))
		}
	case contextArrayElement:
		if producesSafeWords(cs) {
			return
		}
		advice := "read it with mapfile or read -a instead"
		if p.dialect == dialect.Zsh {
			advice = `split it explicitly instead, such as into lines with ${(f)"$(...)"}`
		}
		p.report(2207, span, "Command output is "+p.substitutionFate(cs, "split on whitespace")+" when building this array; "+advice+".", p.lineArrayFix(cs, span)...)
	default:
	}
}

func (p *pass) lineArrayFix(cs *syntax.CmdSubst, span parser.Span) []Fix {
	word, ok := p.parent().(*syntax.Word)
	if !ok || len(word.Parts) != 1 || cs.Backquotes || cs.TempFile || cs.ReplyVar || !cs.Right.IsValid() {
		return nil
	}
	elem, ok := p.ancestor(1).(*syntax.ArrayElem)
	array, arrayOK := p.ancestor(2).(*syntax.ArrayExpr)
	assign, assignOK := p.ancestor(3).(*syntax.Assign)
	call, callOK := p.ancestor(4).(*syntax.CallExpr)
	if !ok || !arrayOK || !assignOK || !callOK || elem.Index != nil || len(array.Elems) != 1 || assign.Append || assign.Index != nil || len(call.Args) > 0 || len(call.Assigns) != 1 {
		return nil
	}
	inner := p.text[parser.Offset(cs.Left)+len("$(") : parser.Offset(cs.Right)]
	switch p.dialect {
	case dialect.Bash:
		callSpan, ok := p.span(call)
		if !ok {
			return nil
		}
		return []Fix{reviewFix("Read the lines with mapfile", textedit.Edit{Start: callSpan.Start, End: callSpan.End, NewText: "mapfile -t " + assign.Name.Value + " < <(" + inner + ")"})}
	case dialect.Zsh:
		return []Fix{reviewFix("Split the output into lines", textedit.Edit{Start: span.Start, End: span.End, NewText: `${(f)"` + p.spanText(span) + `"}`})}
	default:
	}
	return nil
}

func (p *pass) substitutionFate(cs *syntax.CmdSubst, split string) string {
	if p.substitutionGlobbed(cs) {
		return split + " and globbed"
	}
	return split
}

func producesSafeWords(cs *syntax.CmdSubst) bool {
	if len(cs.Stmts) != 1 {
		return false
	}
	call, ok := cs.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok {
		return false
	}
	c, ok := commandOf(call)
	if !ok {
		return false
	}
	return c.isBase("seq", "pgrep") || c.is("compgen") && compgenProducesWords(c.args)
}

var (
	compgenWordActions     = "bekguv"
	compgenWordActionsLong = setOf("arrayvar", "builtin", "disabled", "enabled", "export", "group", "keyword", "setopt", "shopt", "signal", "user", "variable")
	compgenHarmlessOptions = setOf("nosort", "nospace", "noquote", "plusdirs")
)

func compgenProducesWords(args []*syntax.Word) bool {
	actions := 0
	for i := 0; i < len(args); i++ {
		flag, literal := parser.Literal(args[i])
		if !literal || !strings.HasPrefix(flag, "-") {
			return actions > 0 && i == len(args)-1
		}
		if flag == "--" {
			return actions > 0 && i >= len(args)-2
		}
		for j := 1; j < len(flag); j++ {
			letter := flag[j]
			if strings.IndexByte(compgenWordActions, letter) >= 0 {
				actions++
				continue
			}
			value := flag[j+1:]
			if value == "" {
				i++
				if i == len(args) {
					return false
				}
				if value, literal = parser.Literal(args[i]); !literal {
					return false
				}
			}
			switch {
			case letter == 'A' && compgenWordActionsLong[value]:
				actions++
			case letter == 'W' && plainWords(value):
				actions++
			case letter == 'o' && compgenHarmlessOptions[value]:
			case letter == 'X':
			case (letter == 'P' || letter == 'S') && plainWords(value) && !strings.ContainsAny(value, " \t\n"):
			default:
				return false
			}
			break
		}
	}
	return actions > 0
}

func plainWords(s string) bool {
	return !strings.ContainsAny(s, "$`\\'\"*?[")
}

func backtickFix(p *pass, cs *syntax.CmdSubst, span parser.Span) []Fix {
	replacement, ok := parser.DollarSubstitution(p.text, cs, p.stack)
	if !ok {
		return nil
	}
	return []Fix{safeFix("Replace the backticks with $(...)", textedit.Edit{Start: span.Start, End: span.End, NewText: replacement})}
}

func (p *pass) isArgumentOf(name string) bool {
	for depth := 1; ; depth++ {
		switch n := p.ancestor(depth).(type) {
		case *syntax.Word, *syntax.DblQuoted:
			continue
		case *syntax.CallExpr:
			c, ok := commandOf(n)
			return ok && c.is(name)
		}
		return false
	}
}
