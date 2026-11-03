package nix

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const hoverFence = "```nix\n"

type Hover struct {
	Markdown string
	Span     syntax.Span
}

func (d *Document) Hover(offset int, known Evaluated) (Hover, bool) {
	token, ok := d.tokenAt(offset)
	if !ok {
		return Hover{}, false
	}
	if token.Kind == syntax.TokIdent {
		if h, ok := d.builtinHover(token.Span); ok {
			return h, true
		}
		return d.evaluatedHover(token.Span, known)
	}
	k, ok := keywordNamed(d.tree.Text[token.Span.Start:token.Span.End])
	if !ok {
		return Hover{}, false
	}
	return Hover{Markdown: hoverFence + k.Name + "\n```\n\n" + k.Doc, Span: token.Span}, true
}

func (a Analysis) Document() *Document { return &Document{tree: a.tree, table: a.Scope} }

type LexicalName struct {
	Name    string
	Span    syntax.Span
	Uses    []syntax.Span
	Origins []syntax.Span
	Source  string
}

func (d *Document) LexicalAt(offset int) (LexicalName, bool) {
	id := d.identAt(offset)
	if id == nil {
		return LexicalName{}, false
	}
	ref, ok := d.table.Of(id)
	if !ok || (ref.Kind != scope.Dynamic && ref.Kind != scope.Global) {
		return LexicalName{}, false
	}
	found := LexicalName{Name: id.Name, Span: id.Span(), Source: "is built into Nix"}
	if ref.Kind == scope.Dynamic {
		found.Source = "is not bound in this file, so it comes from " + d.withSources(ref.With)
		for _, with := range d.table.Withs(ref.With) {
			at := with.Span().Start
			found.Origins = append(found.Origins, syntax.Span{Start: at, End: at + len("with")})
		}
	}
	for _, root := range slices.Concat([]syntax.Expr{d.tree.Root}, d.tree.Stray) {
		syntax.Inspect(root, func(n syntax.Node) bool {
			use, isIdent := n.(*syntax.Ident)
			if !isIdent || use.Name != id.Name {
				return true
			}
			other, known := d.table.Of(use)
			if known && other.Kind == ref.Kind && other.With == ref.With {
				found.Uses = append(found.Uses, use.Span())
			}
			return true
		})
	}
	return found, true
}

func (d *Document) identAt(offset int) *syntax.Ident {
	var out *syntax.Ident
	for _, root := range slices.Concat([]syntax.Expr{d.tree.Root}, d.tree.Stray) {
		syntax.Inspect(root, func(n syntax.Node) bool {
			s := n.Span()
			if out != nil || offset < s.Start || offset > s.End {
				return false
			}
			if id, isIdent := n.(*syntax.Ident); isIdent {
				out = id
			}
			return out == nil
		})
	}
	return out
}

func (d *Document) withSources(innermost *syntax.With) string {
	withs := d.table.Withs(innermost)
	names := make([]string, len(withs))
	for i, with := range withs {
		s := with.Scope.Span()
		names[i] = "`with " + d.tree.Text[s.Start:s.End] + "`"
	}
	return strings.Join(names, " or ")
}

func (d *Document) Syntax() (*syntax.File, *scope.Table) { return d.tree, d.table }

func (d *Document) tokenAt(offset int) (syntax.Token, bool) {
	tokens := d.tree.TokensIn(syntax.Span{Start: max(offset-1, 0), End: offset + 1})
	i := slices.IndexFunc(tokens, func(t syntax.Token) bool { return hoverable(t.Kind) && t.Span.Start <= offset && offset < t.Span.End })
	if i < 0 {
		i = slices.IndexFunc(tokens, func(t syntax.Token) bool { return hoverable(t.Kind) && t.Span.End == offset })
	}
	if i < 0 {
		return syntax.Token{}, false
	}
	return tokens[i], true
}

func hoverable(kind syntax.TokenKind) bool {
	switch kind {
	case syntax.TokIdent, syntax.TokIf, syntax.TokThen, syntax.TokElse, syntax.TokAssert, syntax.TokWith, syntax.TokLet, syntax.TokIn, syntax.TokRec, syntax.TokInherit, syntax.TokOrKeyword:
		return true
	default:
		return false
	}
}

func (d *Document) builtinHover(span syntax.Span) (Hover, bool) {
	chain := chainTo(d.tree, span)
	if len(chain) == 0 {
		return Hover{}, false
	}
	b, ok := builtinOf(newReading(d, chain, NoFiles).named(chain))
	if !ok {
		return Hover{}, false
	}
	return Hover{Markdown: hoverFence + builtinSignature(b) + "\n```\n\n" + b.Doc + "\n\n" + arity(b), Span: span}, true
}

func builtinOf(values []flowValue) (builtins.Builtin, bool) {
	for _, v := range values {
		switch v := v.(type) {
		case flowBuiltin:
			return builtins.Lookup(v.name)
		case flowBuiltins:
			return builtins.Lookup(builtinsName)
		}
	}
	return builtins.Builtin{}, false
}

func (r *reading) named(chain []syntax.Node) []flowValue {
	switch leaf := chain[len(chain)-1].(type) {
	case *syntax.Ident:
		ref, ok := r.table.Of(leaf)
		if !ok || ref.Kind == scope.Definition || ref.Kind == scope.Undefined {
			return nil
		}
		p, u := r.linked()
		return p.evalRef(u, ref, leaf.Name)
	case *syntax.IdentAttr:
		if len(chain) < 2 {
			return nil
		}
		return r.namedAttr(chain[len(chain)-2], leaf)
	}
	return nil
}

func (r *reading) namedAttr(parent syntax.Node, leaf *syntax.IdentAttr) []flowValue {
	switch parent := parent.(type) {
	case *syntax.Select:
		return r.builtinMember(parent.Target, parent.Path, leaf)
	case *syntax.HasAttr:
		return r.builtinMember(parent.Target, parent.Path, leaf)
	case *syntax.Inherit:
		if parent.From != nil {
			return r.builtinMember(parent.From.Inner, syntax.AttrPath{leaf}, leaf)
		}
		ref, ok := r.table.Inherited(leaf)
		if !ok || ref.Kind != scope.Global {
			return nil
		}
		p, u := r.linked()
		return p.evalRef(u, ref, leaf.Name)
	}
	return nil
}

func (r *reading) builtinMember(target syntax.Expr, path syntax.AttrPath, leaf *syntax.IdentAttr) []flowValue {
	p, u := r.linked()
	i := slices.IndexFunc(path, func(a syntax.Attr) bool { return a == syntax.Attr(leaf) })
	if !slices.Contains(p.evalPath(u, target, path[:i]), flowValue(flowBuiltins{})) {
		return nil
	}
	return p.selectFrom(flowBuiltins{}, leaf.Name)
}

func arity(b builtins.Builtin) string {
	switch n := len(b.Params); n {
	case 0:
		return "A value, not a function."
	case 1:
		return "Takes 1 argument."
	default:
		return fmt.Sprintf("Takes %d arguments.", n)
	}
}

func (d *Document) CommentAbove(offset int) string {
	var owner syntax.Node
	visit := func(n syntax.Node) bool {
		span := n.Span()
		if offset < span.Start || offset >= span.End {
			return false
		}
		switch n.(type) {
		case *syntax.Assign, *syntax.Inherit, *syntax.Formal, *syntax.IdentParam:
			owner = n
		}
		return true
	}
	for _, root := range slices.Concat([]syntax.Expr{d.tree.Root}, d.tree.Stray) {
		syntax.Inspect(root, visit)
	}
	if owner == nil {
		return ""
	}
	return commentAbove(d.tree, owner.Span().Start)
}
