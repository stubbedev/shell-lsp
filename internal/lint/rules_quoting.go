package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var quotingFamily = family{
	codes: map[Code]meta{
		2006: {severity: SeverityStyle, tag: TagDeprecated},
		2046: {severity: SeverityWarning},
		2048: {severity: SeverityWarning},
		2068: {severity: SeverityError},
		2086: {severity: SeverityInfo},
		2206: {severity: SeverityWarning},
		2207: {severity: SeverityWarning},
		2223: {severity: SeverityInfo},
		2248: {severity: SeverityStyle, optional: "quote-safe-variables"},
	},
	rules: []rule{
		{on: nodes(&syntax.ParamExp{}), node: checkUnquotedExpansion},
		{on: nodes(&syntax.CmdSubst{}), node: checkUnquotedSubstitution},
	},
}

var spacelessSpecials = setOf("-", "$", "?", "!", "#")

func quoteFix(span parser.Span) []textedit.Edit {
	return []textedit.Edit{{Start: span.Start, End: span.Start, NewText: `"`}, {Start: span.End, End: span.End, NewText: `"`}}
}

func checkUnquotedExpansion(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || pe.Param == nil || !p.dialect.Features().SplitsUnquoted {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	text := p.text[span.Start:span.End]
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
			p.report(2068, span, "Quote this array expansion so each element stays one word: \""+text+"\".", quoteFix(span)...)
		}
		return
	case expandsJoined:
		if p.soleWordPart(pe) {
			p.report(2048, span, "Unquoted "+text+" joins the elements and splits them again; use \"$@\" or \"${array[@]}\" to keep them intact.")
		}
	}
	if context != contextSplit || spacelessSpecials[pe.Param.Value] || p.namesAssignment(pe) {
		return
	}
	defaultAssignment := assignsParameter(pe) && p.isArgumentOf(":")
	p.withFlow(func(flow *flowResult) {
		switch {
		case flow.expansion(pe).clean():
			p.report(2248, span, text+" holds no spaces or globs today, but quoting it keeps it safe if that changes: \""+text+"\".", quoteFix(span)...)
		case defaultAssignment:
			p.report(2223, span, "This default assignment is expanded unquoted, so its value is globbed on every run; quote it.", quoteFix(span)...)
		default:
			p.report(2086, span, "Unquoted "+text+" is split on whitespace and expanded as a glob; double-quote it.", quoteFix(span)...)
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

func checkUnquotedSubstitution(p *pass, n syntax.Node) {
	cs, ok := n.(*syntax.CmdSubst)
	if !ok || len(cs.Stmts) == 0 {
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
			p.report(2046, span, "The output of this command substitution is split into words and globbed; quote it.", quoteFix(span)...)
		}
	case contextArrayElement:
		p.report(2207, span, "Command output is split on whitespace and globbed when building this array; read it with mapfile or read -a instead.")
	}
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
	return ok && c.isBase("seq", "pgrep")
}

func backtickFix(p *pass, cs *syntax.CmdSubst, span parser.Span) []textedit.Edit {
	replacement, ok := parser.DollarSubstitution(p.text, cs, p.stack)
	if !ok {
		return nil
	}
	return []textedit.Edit{{Start: span.Start, End: span.End, NewText: replacement}}
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
