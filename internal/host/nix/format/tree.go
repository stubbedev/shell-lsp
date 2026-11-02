package format

import (
	"cmp"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type nodeKind uint8

const (
	kindIdent nodeKind = iota
	kindNumber
	kindURI
	kindPath
	kindString
	kindInterpolation
	kindParen
	kindList
	kindAttrSet
	kindLet
	kindLegacyLet
	kindWith
	kindAssert
	kindIf
	kindLambda
	kindPattern
	kindFormal
	kindApply
	kindSelect
	kindHasAttr
	kindBinary
	kindUnary
	kindAssign
	kindInherit
	kindAttrPath
	kindAttrName
	kindRoot
)

type element struct {
	child *node
	token syntax.Token
}

type stringPart struct {
	text   string
	interp *node
}

type node struct {
	kind     nodeKind
	span     syntax.Span
	elements []element
	kids     []*node
	parts    []stringPart
	indented bool
	dynamic  bool
	rec      bool
	op       string
	from     *node
	attrs    []*node
	name     *node
	fallback *node
}

type converter struct{ file *syntax.File }

func isTrivia(k syntax.TokenKind) bool {
	return k == syntax.TokWhitespace || k == syntax.TokLineComment || k == syntax.TokBlockComment
}

func (c *converter) assemble(n *node, children []*node) *node {
	slices.SortStableFunc(children, func(a, b *node) int { return cmp.Compare(a.span.Start, b.span.Start) })
	at := n.span.Start
	for _, child := range children {
		n.elements = c.tokens(n.elements, syntax.Span{Start: at, End: child.span.Start})
		n.elements = append(n.elements, element{child: child})
		at = child.span.End
	}
	n.elements = c.tokens(n.elements, syntax.Span{Start: at, End: n.span.End})
	return n
}

func (c *converter) tokens(out []element, span syntax.Span) []element {
	if span.End <= span.Start {
		return out
	}
	for _, t := range c.file.TokensIn(span) {
		if t.Span.Start >= span.Start && t.Span.End > t.Span.Start {
			out = append(out, element{token: t})
		}
	}
	return out
}

func (c *converter) strings(parts []syntax.Part) []stringPart {
	out := make([]stringPart, 0, len(parts))
	for _, p := range parts {
		in, ok := p.(*syntax.Interpolation)
		if ok {
			out = append(out, stringPart{interp: c.interpolation(in.Span(), in.Value, false)})
			continue
		}
		s := c.file.Text[p.Span().Start:p.Span().End]
		if last := len(out) - 1; last >= 0 && out[last].interp == nil {
			out[last].text += s
			continue
		}
		out = append(out, stringPart{text: s})
	}
	return out
}

func (c *converter) interpolation(span syntax.Span, value syntax.Expr, dynamic bool) *node {
	inner := c.expr(value)
	return c.assemble(&node{kind: kindInterpolation, span: span, kids: []*node{inner}, dynamic: dynamic}, []*node{inner})
}

func (c *converter) quoted(s *syntax.String) *node {
	n := &node{kind: kindString, span: s.Span(), indented: s.Kind == syntax.Indented, parts: c.strings(s.Parts)}
	for _, p := range n.parts {
		if p.interp != nil {
			n.kids = append(n.kids, p.interp)
		}
	}
	return n
}

func (c *converter) attr(a syntax.Attr) *node {
	switch a := a.(type) {
	case *syntax.StringAttr:
		return c.quoted(a.Value)
	case *syntax.DynamicAttr:
		return c.interpolation(a.Span(), a.Value, true)
	}
	return &node{kind: kindAttrName, span: a.Span()}
}

func (c *converter) attrPath(path syntax.AttrPath) []*node {
	out := make([]*node, len(path))
	for i, a := range path {
		out[i] = c.attr(a)
	}
	return out
}

func (c *converter) exprs(list []syntax.Expr) []*node {
	out := make([]*node, len(list))
	for i, e := range list {
		out[i] = c.expr(e)
	}
	return out
}

func (c *converter) binding(b syntax.Binding) *node {
	switch b := b.(type) {
	case *syntax.Assign:
		attrs := c.attrPath(b.Path)
		span := syntax.Span{Start: attrs[0].span.Start, End: attrs[len(attrs)-1].span.End}
		path := c.assemble(&node{kind: kindAttrPath, span: span, attrs: attrs}, slices.Clone(attrs))
		value := c.expr(b.Value)
		return c.assemble(&node{kind: kindAssign, span: b.Span(), kids: []*node{path, value}}, []*node{path, value})
	case *syntax.Inherit:
		n := &node{kind: kindInherit, span: b.Span(), attrs: c.attrPath(b.Attrs)}
		children := slices.Clone(n.attrs)
		if b.From != nil {
			n.from = c.expr(b.From)
			children = append(children, n.from)
		}
		return c.assemble(n, children)
	}
	panic("format: a parsed Nix file has an unknown binding")
}

func (c *converter) bindings(list []syntax.Binding) []*node {
	out := make([]*node, len(list))
	for i, b := range list {
		out[i] = c.binding(b)
	}
	return out
}

func (c *converter) param(p syntax.Param) *node {
	switch p := p.(type) {
	case *syntax.IdentParam:
		return c.expr(p.Name)
	case *syntax.PatternParam:
		n := &node{kind: kindPattern, span: p.Span()}
		children := make([]*node, 0, len(p.Formals)+1)
		for _, f := range p.Formals {
			formal := &node{kind: kindFormal, span: f.Span(), name: c.expr(f.Name)}
			inner := []*node{formal.name}
			if f.Default != nil {
				formal.fallback = c.expr(f.Default)
				inner = append(inner, formal.fallback)
			}
			c.assemble(formal, inner)
			n.kids = append(n.kids, formal)
			children = append(children, formal)
		}
		if p.Alias != nil {
			n.name = c.expr(p.Alias)
			children = append(children, n.name)
		}
		return c.assemble(n, children)
	}
	panic("format: a parsed Nix file has an unknown parameter")
}

func (c *converter) expr(e syntax.Expr) *node {
	switch e := e.(type) {
	case *syntax.Ident:
		return &node{kind: kindIdent, span: e.Span()}
	case *syntax.Int, *syntax.Float:
		return &node{kind: kindNumber, span: e.Span()}
	case *syntax.URI:
		return &node{kind: kindURI, span: e.Span()}
	case *syntax.String:
		return c.quoted(e)
	case *syntax.Path:
		n := &node{kind: kindPath, span: e.Span(), parts: c.strings(e.Parts)}
		if e.Kind == syntax.SearchPath {
			n.parts = []stringPart{{text: c.file.Text[n.span.Start:n.span.End]}}
		}
		return n
	case *syntax.Paren:
		inner := c.expr(e.Inner)
		return c.assemble(&node{kind: kindParen, span: e.Span(), kids: []*node{inner}}, []*node{inner})
	case *syntax.List:
		kids := c.exprs(e.Items)
		return c.assemble(&node{kind: kindList, span: e.Span(), kids: kids}, slices.Clone(kids))
	case *syntax.AttrSet:
		kids := c.bindings(e.Bindings)
		return c.assemble(&node{kind: kindAttrSet, span: e.Span(), kids: kids, rec: e.Rec}, slices.Clone(kids))
	case *syntax.Let:
		kids := append(c.bindings(e.Bindings), c.expr(e.Body))
		return c.assemble(&node{kind: kindLet, span: e.Span(), kids: kids}, slices.Clone(kids))
	case *syntax.LegacyLet:
		set := c.expr(e.Set)
		return c.assemble(&node{kind: kindLegacyLet, span: e.Span(), kids: []*node{set}}, []*node{set})
	case *syntax.With:
		return c.fixed(kindWith, e.Span(), e.Scope, e.Body)
	case *syntax.Assert:
		return c.fixed(kindAssert, e.Span(), e.Cond, e.Body)
	case *syntax.If:
		return c.fixed(kindIf, e.Span(), e.Cond, e.Then, e.Else)
	case *syntax.Lambda:
		param, body := c.param(e.Param), c.expr(e.Body)
		return c.assemble(&node{kind: kindLambda, span: e.Span(), kids: []*node{param, body}}, []*node{param, body})
	case *syntax.Apply:
		return c.fixed(kindApply, e.Span(), append([]syntax.Expr{e.Fn}, e.Args...)...)
	case *syntax.Select:
		target := c.expr(e.Target)
		attrs := c.attrPath(e.Path)
		n := &node{kind: kindSelect, span: e.Span(), kids: []*node{target}, attrs: attrs}
		children := append([]*node{target}, attrs...)
		if e.Default != nil {
			n.fallback = c.expr(e.Default)
			children = append(children, n.fallback)
		}
		return c.assemble(n, children)
	case *syntax.HasAttr:
		target := c.expr(e.Target)
		attrs := c.attrPath(e.Path)
		return c.assemble(&node{kind: kindHasAttr, span: e.Span(), kids: []*node{target}, attrs: attrs}, append([]*node{target}, attrs...))
	case *syntax.Binary:
		n := c.fixed(kindBinary, e.Span(), e.Left, e.Right)
		n.op = e.Op.String()
		return n
	case *syntax.Unary:
		return c.fixed(kindUnary, e.Span(), e.Operand)
	}
	panic("format: a parsed Nix file has an unknown expression")
}

func (c *converter) fixed(kind nodeKind, span syntax.Span, exprs ...syntax.Expr) *node {
	kids := c.exprs(exprs)
	return c.assemble(&node{kind: kind, span: span, kids: kids}, slices.Clone(kids))
}
