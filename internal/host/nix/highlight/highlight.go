package highlight

import (
	"cmp"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

const (
	builtinsSet = "builtins"
	blank       = " \t\r"
)

type class struct {
	kind semantic.Type
	mods semantic.Modifier
}

func (c class) declared() class {
	return class{kind: c.kind, mods: c.mods | semantic.ModDeclaration}
}

var (
	keywordClass   = class{kind: semantic.TypeKeyword}
	numberClass    = class{kind: semantic.TypeNumber}
	stringClass    = class{kind: semantic.TypeString}
	escapeClass    = class{kind: semantic.TypeRegexp}
	operatorClass  = class{kind: semantic.TypeOperator}
	commentClass   = class{kind: semantic.TypeComment}
	locationClass  = class{kind: semantic.TypeString, mods: semantic.ModExternal}
	variableClass  = class{kind: semantic.TypeVariable}
	parameterClass = class{kind: semantic.TypeParameter}
	propertyClass  = class{kind: semantic.TypeProperty}
	dynamicClass   = class{kind: semantic.TypeVariable, mods: semantic.ModExternal}
	namespaceClass = class{kind: semantic.TypeNamespace, mods: semantic.ModDefaultLibrary}
	functionClass  = class{kind: semantic.TypeFunction, mods: semantic.ModDefaultLibrary}
	constantClass  = class{kind: semantic.TypeVariable, mods: semantic.ModReadonly | semantic.ModDefaultLibrary}
)

func Tokens(f *syntax.File, table *scope.Table, shell []semantic.Token, pieces []syntax.Span) []semantic.Token {
	n := namer{table: table, classes: make(map[int]class, len(f.Tokens)/4)}
	syntax.Inspect(f.Root, n.visit)
	for _, e := range f.Stray {
		syntax.Inspect(e, n.visit)
	}
	return overlay(f.Text, classified(f, n.classes), clipped(f, outermost(n.antiquotes), pieces, shell))
}

func lexical(kind syntax.TokenKind) (class, bool) {
	switch kind {
	case syntax.TokIf, syntax.TokThen, syntax.TokElse, syntax.TokAssert, syntax.TokWith, syntax.TokLet, syntax.TokIn, syntax.TokRec, syntax.TokInherit, syntax.TokOrKeyword:
		return keywordClass, true
	case syntax.TokInt, syntax.TokFloat:
		return numberClass, true
	case syntax.TokQuote, syntax.TokIndentQuote, syntax.TokStringText:
		return stringClass, true
	case syntax.TokEscape:
		return escapeClass, true
	case syntax.TokPath, syntax.TokHomePath, syntax.TokSearchPath, syntax.TokPathText, syntax.TokURI:
		return locationClass, true
	case syntax.TokLineComment, syntax.TokBlockComment:
		return commentClass, true
	case syntax.TokInterp, syntax.TokEllipsis, syntax.TokDot, syntax.TokColon, syntax.TokAssign, syntax.TokAt, syntax.TokQuestion, syntax.TokNot,
		syntax.TokPlus, syntax.TokMinus, syntax.TokStar, syntax.TokSlash, syntax.TokUpdate, syntax.TokConcat,
		syntax.TokEq, syntax.TokNe, syntax.TokLt, syntax.TokLe, syntax.TokGt, syntax.TokGe,
		syntax.TokAnd, syntax.TokOr, syntax.TokImplies, syntax.TokPipeInto, syntax.TokPipeFrom:
		return operatorClass, true
	default:
		return class{}, false
	}
}

func classified(f *syntax.File, named map[int]class) []semantic.Token {
	out := make([]semantic.Token, 0, len(f.Tokens)/2)
	for _, tok := range f.Tokens {
		c, ok := named[tok.Span().Start]
		if !ok {
			c, ok = lexical(tok.Kind)
		}
		if ok {
			out = appendLines(out, f.Text, tok.Span(), c)
		}
	}
	return out
}

func appendLines(out []semantic.Token, text string, span syntax.Span, c class) []semantic.Token {
	for start := span.Start; start < span.End; {
		end := span.End
		if i := strings.IndexByte(text[start:end], '\n'); i >= 0 {
			end = start + i
		}
		first := start
		if start > span.Start {
			first = end - len(strings.TrimLeft(text[start:end], blank))
		}
		if strings.Trim(text[first:end], blank) != "" {
			out = append(out, semantic.Token{Span: syntax.Span{Start: first, End: end}, Type: c.kind, Modifiers: c.mods})
		}
		start = end + 1
	}
	return out
}

type namer struct {
	table      *scope.Table
	classes    map[int]class
	antiquotes []syntax.Span
}

func (n *namer) visit(node syntax.Node) bool {
	switch node := node.(type) {
	case *syntax.Ident:
		n.ident(node)
	case *syntax.Interpolation:
		n.antiquote(node.Span(), node.Closed)
	case *syntax.DynamicAttr:
		n.antiquote(node.Span(), node.Closed)
	case *syntax.Select:
		n.selection(node.Target, node.Path)
	case *syntax.HasAttr:
		n.selection(node.Target, node.Path)
	case *syntax.Assign:
		n.bound(node.Path)
	case *syntax.Inherit:
		n.bound(node.Attrs)
	}
	return true
}

func (n *namer) antiquote(span syntax.Span, closed bool) {
	n.antiquotes = append(n.antiquotes, span)
	if closed {
		n.classes[span.End-1] = operatorClass
	}
}

func (n *namer) ident(id *syntax.Ident) {
	ref, resolved := n.table.Of(id)
	if !resolved {
		return
	}
	c, known := n.reference(ref, id.Name)
	if !known {
		return
	}
	if _, binds := n.table.Binder(id); binds {
		c = c.declared()
	}
	n.classes[id.Span().Start] = c
}

func (n *namer) reference(ref scope.Ref, name string) (class, bool) {
	switch ref.Kind {
	case scope.Definition:
		return definition(ref.Def), true
	case scope.Global:
		return library(*ref.Builtin), true
	case scope.Dynamic:
		if member, ok := builtinMember(name); ok && n.isBuiltins(ref.With.Scope) {
			return library(member), true
		}
		return dynamicClass, true
	default:
		return class{}, false
	}
}

func definition(d *scope.Def) class {
	switch {
	case d.Kind == scope.ArgDef, d.Kind == scope.FormalDef, d.Kind == scope.AliasDef:
		return parameterClass
	case d.Scope.Kind == scope.RecScope:
		return propertyClass
	}
	return variableClass
}

func library(b builtins.Builtin) class {
	switch {
	case b.Name == builtinsSet:
		return namespaceClass
	case len(b.Params) > 0:
		return functionClass
	}
	return constantClass
}

func builtinMember(name string) (builtins.Builtin, bool) {
	if strings.HasPrefix(name, builtins.HiddenAliasPrefix) {
		return builtins.Builtin{}, false
	}
	return builtins.Lookup(name)
}

func (n *namer) isBuiltins(e syntax.Expr) bool {
	id, ok := syntax.Unparenthesized(e).(*syntax.Ident)
	if !ok || id.Name != builtinsSet {
		return false
	}
	ref, resolved := n.table.Of(id)
	return resolved && ref.Kind == scope.Global
}

func (n *namer) selection(target syntax.Expr, path syntax.AttrPath) {
	for i, a := range path {
		c := propertyClass
		if i == 0 {
			c = n.member(target, a)
		}
		n.attr(a, c)
	}
}

func (n *namer) member(target syntax.Expr, a syntax.Attr) class {
	name, named := syntax.AttrName(a)
	if !named || !n.isBuiltins(target) {
		return propertyClass
	}
	member, ok := builtinMember(name)
	if !ok {
		return propertyClass
	}
	return library(member)
}

func (n *namer) bound(attrs []syntax.Attr) {
	for _, a := range attrs {
		c := propertyClass
		if def, binds := n.table.Binder(a); binds {
			c = definition(def)
		}
		n.attr(a, c.declared())
	}
}

func (n *namer) attr(a syntax.Attr, c class) {
	switch a := a.(type) {
	case *syntax.IdentAttr:
		n.classes[a.Span().Start] = c
	case *syntax.StringAttr:
		if !syntax.StaticAttr(a) {
			return
		}
		for _, part := range a.Value.Parts {
			if literal, ok := part.(*syntax.Literal); ok {
				n.classes[literal.Span().Start] = c
			}
		}
	}
}

func clipped(f *syntax.File, antiquotes, pieces []syntax.Span, shell []semantic.Token) []semantic.Token {
	if len(shell) == 0 {
		return nil
	}
	runs := shellRuns(literalRuns(f.Tokens), antiquotes, pieces)
	parts := make([]shellPart, 0, len(shell))
	for _, t := range shell {
		first := sort.Search(len(runs), func(i int) bool { return runs[i].End > t.Span.Start })
		for _, r := range runs[first:] {
			if r.Start >= t.Span.End {
				break
			}
			parts = append(parts, shellPart{span: syntax.Span{Start: max(r.Start, t.Span.Start), End: min(r.End, t.Span.End)}, width: t.Span.End - t.Span.Start, class: class{kind: t.Type, mods: t.Modifiers}})
		}
	}
	slices.SortStableFunc(parts, func(a, b shellPart) int {
		return cmp.Or(cmp.Compare(a.span.Start, b.span.Start), cmp.Compare(a.width, b.width))
	})
	out := make([]semantic.Token, 0, len(parts))
	end := 0
	for _, part := range parts {
		start := max(part.span.Start, end)
		if start >= part.span.End {
			continue
		}
		out = appendLines(out, f.Text, syntax.Span{Start: start, End: part.span.End}, part.class)
		end = part.span.End
	}
	return out
}

type shellPart struct {
	span  syntax.Span
	width int
	class class
}

func shellRuns(runs, antiquotes, pieces []syntax.Span) []syntax.Span {
	out := make([]syntax.Span, 0, len(runs))
	outer := 0
	for _, run := range runs {
		for outer < len(antiquotes) && antiquotes[outer].End <= run.Start {
			outer++
		}
		if outer == len(antiquotes) || antiquotes[outer].Start >= run.End {
			out = append(out, run)
			continue
		}
		first := sort.Search(len(pieces), func(i int) bool { return pieces[i].End > run.Start })
		for _, piece := range pieces[first:] {
			if piece.Start >= run.End {
				break
			}
			out = append(out, syntax.Span{Start: max(run.Start, piece.Start), End: min(run.End, piece.End)})
		}
	}
	return out
}

func outermost(antiquotes []syntax.Span) []syntax.Span {
	slices.SortFunc(antiquotes, func(a, b syntax.Span) int { return cmp.Compare(a.Start, b.Start) })
	end := 0
	return slices.DeleteFunc(antiquotes, func(s syntax.Span) bool {
		if s.Start < end {
			return true
		}
		end = s.End
		return false
	})
}

func literalRuns(tokens []syntax.Token) []syntax.Span {
	var runs []syntax.Span
	for _, tok := range tokens {
		if tok.Kind != syntax.TokStringText && tok.Kind != syntax.TokEscape {
			continue
		}
		if n := len(runs); n > 0 && runs[n-1].End == tok.Span().Start {
			runs[n-1].End = tok.Span().End
			continue
		}
		runs = append(runs, tok.Span())
	}
	return runs
}

func overlay(text string, nix, shell []semantic.Token) []semantic.Token {
	if len(shell) == 0 {
		return nix
	}
	out := make([]semantic.Token, 0, len(nix)+len(shell))
	covered := 0
	next := 0
	for _, t := range nix {
		at := max(t.Span.Start, covered)
		for next < len(shell) && shell[next].Span.Start < t.Span.End {
			s := shell[next]
			if s.Span.Start > at {
				out = fragment(out, text, t, at, s.Span.Start)
			}
			out = append(out, s)
			covered = s.Span.End
			at = max(at, covered)
			next++
		}
		if at < t.Span.End {
			out = fragment(out, text, t, at, t.Span.End)
		}
	}
	return append(out, shell[next:]...)
}

func fragment(out []semantic.Token, text string, t semantic.Token, start, end int) []semantic.Token {
	if start == t.Span.Start && end == t.Span.End {
		return append(out, t)
	}
	piece := text[start:end]
	first := end - len(strings.TrimLeft(piece, blank))
	last := start + len(strings.TrimRight(piece, blank))
	if first >= last {
		return out
	}
	return append(out, semantic.Token{Span: syntax.Span{Start: first, End: last}, Type: t.Type, Modifiers: t.Modifiers})
}
