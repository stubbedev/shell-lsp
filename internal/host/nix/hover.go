package nix

import (
	"fmt"
	"slices"

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
	}
	return false
}

func (d *Document) builtinHover(span syntax.Span) (Hover, bool) {
	chain := chainTo(d.tree, span)
	if len(chain) == 0 {
		return Hover{}, false
	}
	ev := newEvaluator(d.tree, d.table, chain, NoFiles)
	v, ok := ev.named(chain)
	if !ok || v.kind == setValue || v.kind == lambdaValue || v.kind == unknownValue {
		return Hover{}, false
	}
	b := v.builtin
	if v.kind == builtinsValue {
		b, _ = builtins.Lookup(builtinsName)
	}
	return Hover{Markdown: hoverFence + builtinSignature(b) + "\n```\n\n" + b.Doc + "\n\n" + arity(b), Span: span}, true
}

func (ev *evaluator) named(chain []syntax.Node) (value, bool) {
	leaf := chain[len(chain)-1]
	switch leaf := leaf.(type) {
	case *syntax.Ident:
		ref, ok := ev.table.Of(leaf)
		if !ok || ref.Kind == scope.Definition {
			return value{}, false
		}
		return ev.evalRef(ref, leaf.Name), true
	case *syntax.IdentAttr:
		if len(chain) < 2 {
			return value{}, false
		}
		return ev.namedAttr(chain[len(chain)-2], leaf)
	}
	return value{}, false
}

func (ev *evaluator) namedAttr(parent syntax.Node, leaf *syntax.IdentAttr) (value, bool) {
	var target value
	var path syntax.AttrPath
	switch parent := parent.(type) {
	case *syntax.Select:
		target, path = ev.eval(parent.Target), parent.Path
	case *syntax.HasAttr:
		target, path = ev.eval(parent.Target), parent.Path
	case *syntax.Inherit:
		if parent.From != nil {
			return ev.builtinMember(ev.eval(parent.From.Inner), leaf.Name)
		}
		ref, ok := ev.table.Inherited(leaf)
		if !ok || ref.Kind != scope.Global {
			return value{}, false
		}
		return ev.evalRef(ref, leaf.Name), true
	default:
		return value{}, false
	}
	i := slices.IndexFunc(path, func(a syntax.Attr) bool { return a == syntax.Attr(leaf) })
	return ev.builtinMember(ev.selected(target, path[:i]), leaf.Name)
}

func (ev *evaluator) builtinMember(owner value, name string) (value, bool) {
	if owner.kind != builtinsValue {
		return value{}, false
	}
	return ev.memberNamed(owner, name), true
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
