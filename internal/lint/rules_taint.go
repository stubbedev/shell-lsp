package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var taintFamily = family{
	codes: map[Code]meta{
		2089: {severity: SeverityWarning},
		2090: {severity: SeverityWarning},
	},
	rules: []rule{
		{on: nodes(&syntax.Assign{}, &syntax.WordIter{}, &syntax.ParamExp{}), node: trackQuoteTaint},
	},
}

func trackQuoteTaint(p *pass, n syntax.Node) {
	switch n := n.(type) {
	case *syntax.Assign:
		if n.Name != nil && n.Array == nil && n.Index == nil && !n.Naked {
			p.taintFrom(n.Name.Value, n.Value, n.Append)
		}
	case *syntax.WordIter:
		if n.Name != nil && n.InPos.IsValid() {
			p.taintFromItems(n.Name.Value, n.Items)
		}
	case *syntax.ParamExp:
		p.checkTaintedReference(n)
		if n.Param != nil && isDefaultAssignment(n) && n.Exp.Word != nil {
			p.taintFrom(n.Param.Value, n.Exp.Word, false)
		}
	}
}

func (p *pass) taintFrom(name string, value *syntax.Word, appending bool) {
	if origin, ok := p.quoteOrigin(value); ok {
		p.setTaint(name, origin)
		return
	}
	if !appending {
		delete(p.quoteTaint, name)
	}
}

func (p *pass) taintFromItems(name string, items []*syntax.Word) {
	for _, item := range items {
		if origin, ok := p.quoteOrigin(item); ok {
			p.setTaint(name, origin)
			return
		}
	}
	delete(p.quoteTaint, name)
}

func (p *pass) setTaint(name string, origin parser.Span) {
	if p.quoteTaint == nil {
		p.quoteTaint = map[string]parser.Span{}
	}
	p.quoteTaint[name] = origin
}

func (p *pass) quoteOrigin(w *syntax.Word) (parser.Span, bool) {
	if w == nil {
		return parser.Span{}, false
	}
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			if hasLiteralQuotes(parser.UnescapeLit(part.Value, false)) {
				return p.span(part)
			}
		case *syntax.SglQuoted:
			if hasLiteralQuotes(part.Value) {
				return p.span(part)
			}
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				switch inner := inner.(type) {
				case *syntax.Lit:
					if hasLiteralQuotes(parser.UnescapeLit(inner.Value, true)) {
						return p.span(inner)
					}
				case *syntax.ParamExp:
					if origin, ok := p.plainTaint(inner); ok {
						return origin, true
					}
				}
			}
		case *syntax.ParamExp:
			if origin, ok := p.plainTaint(part); ok {
				return origin, true
			}
		}
	}
	return parser.Span{}, false
}

func (p *pass) plainTaint(pe *syntax.ParamExp) (parser.Span, bool) {
	if !parser.IsPlainParam(pe) {
		return parser.Span{}, false
	}
	origin, ok := p.quoteTaint[pe.Param.Value]
	return origin, ok
}

func hasLiteralQuotes(text string) bool {
	if strings.Contains(text, `"`) || strings.Contains(text, `\ `) {
		return true
	}
	for i := range len(text) {
		if text[i] != '\'' {
			continue
		}
		if i == 0 || strings.IndexByte("/= ", text[i-1]) >= 0 || i == len(text)-1 || text[i+1] == ' ' {
			return true
		}
	}
	return false
}

func (p *pass) checkTaintedReference(pe *syntax.ParamExp) {
	if pe.Param == nil || pe.Length || len(p.quoteTaint) == 0 {
		return
	}
	origin, ok := p.quoteTaint[pe.Param.Value]
	if !ok {
		return
	}
	switch p.contextOf(pe) {
	case contextSplit, contextCommandName, contextArrayElement:
	default:
		return
	}
	if p.isArgumentOf("eval") {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	advice := "use an array to keep arguments with spaces together"
	if !p.dialect.Features().Arrays {
		advice = "use set -- or a function to keep arguments with spaces together"
	}
	p.report(2089, origin, "The quotes or backslashes in this value are stored literally and will not group words when "+pe.Param.Value+" is expanded; "+advice+".")
	p.report(2090, span, "The quotes or backslashes stored in "+pe.Param.Value+" are passed on literally, not used for grouping; "+advice+".")
}
