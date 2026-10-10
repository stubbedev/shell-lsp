package semantic

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
)

type Type uint8

const (
	TypeVariable Type = iota
	TypeParameter
	TypeFunction
	TypeKeyword
	TypeString
	TypeNumber
	TypeComment
	TypeOperator
	TypeProperty
)

var TypeNames = []string{"variable", "parameter", "function", "keyword", "string", "number", "comment", "operator", "property"}

type Modifier uint32

const (
	ModDeclaration Modifier = 1 << iota
	ModModification
	ModReadonly
	ModDefaultLibrary
	ModExternal
)

var ModifierNames = []string{"declaration", "modification", "readonly", "defaultLibrary", "external"}

type Token struct {
	Span      parser.Span
	Type      Type
	Modifiers Modifier
}

func Tokens(tree *parser.Tree, imports []parser.Import) []Token {
	g := generator{tree: tree, imports: imports, text: tree.Text()}
	g.tokens = make([]Token, 0, 2*(len(tree.Comments)+len(tree.Keywords)+len(tree.Definitions)+len(tree.References)+2*len(tree.Commands)))
	g.index()
	g.walk()
	return g.result()
}

type generator struct {
	tree    *parser.Tree
	imports []parser.Import
	text    string
	tokens  []Token
}

func (g *generator) add(span parser.Span, t Type, mods Modifier) {
	if span.End <= span.Start || span.End > len(g.text) {
		return
	}
	for start := span.Start; start < span.End; {
		end := span.End
		if i := strings.IndexByte(g.text[start:span.End], '\n'); i >= 0 {
			end = start + i
		}
		if end > start {
			g.tokens = append(g.tokens, Token{Span: parser.Span{Start: start, End: end}, Type: t, Modifiers: mods})
		}
		start = end + 1
	}
}

func (g *generator) index() {
	tree := g.tree
	for _, c := range tree.Comments {
		g.add(c.Span, TypeComment, 0)
	}
	for _, k := range tree.Keywords {
		g.add(k.Span, TypeKeyword, 0)
	}
	for _, fn := range tree.Functions {
		g.add(fn.NameSpan, TypeFunction, ModDeclaration)
	}
	for _, def := range tree.Definitions {
		mods := ModModification
		if def.Kind == parser.DefDeclare || def.Kind == parser.DefLoop {
			mods = ModDeclaration
		}
		if def.Attrs.Has(parser.AttrReadonly) {
			mods |= ModReadonly
		}
		g.add(def.NameSpan, TypeVariable, mods)
	}
	for _, ref := range tree.References {
		g.add(ref.Span, TypeVariable, g.referenceModifiers(ref))
	}
	for _, cmd := range tree.Commands {
		g.add(cmd.NameSpan, TypeFunction, g.commandModifiers(cmd.Name))
		for _, arg := range cmd.Args {
			if value := arg.Lit(); strings.HasPrefix(value, "-") && len(value) > 1 {
				if span, ok := tree.NodeSpan(arg); ok {
					g.add(span, TypeProperty, 0)
				}
			}
		}
	}
	for _, r := range tree.Redirects {
		g.add(r.OpSpan, TypeOperator, 0)
	}
}

func (g *generator) referenceModifiers(ref parser.Reference) Modifier {
	attrs := g.tree.VisibleAttrs(ref.Name, ref.Within)
	if g.tree.Visible(ref.Name, ref.Within).Len() == 0 {
		imp, _, ok := parser.ImportedGlobal(g.imports, ref.Name)
		if !ok {
			if _, special := shelldocs.Variable(ref.Name); special {
				return ModDefaultLibrary
			}
			return 0
		}
		attrs = imp.Tree.VisibleAttrs(ref.Name, parser.FileScope)
	}
	if attrs.Has(parser.AttrReadonly) {
		return ModReadonly
	}
	return 0
}

func (g *generator) commandModifiers(name string) Modifier {
	if _, ok := g.tree.FunctionNamed(name); ok {
		return 0
	}
	if _, _, ok := parser.ImportedFunction(g.imports, name); ok {
		return 0
	}
	if entry, ok := shelldocs.Builtin(name); ok && entry.Dialects.Has(g.tree.Dialect) {
		return ModDefaultLibrary
	}
	return ModExternal
}

func (g *generator) walk() {
	tree := g.tree
	var stack []syntax.Node
	parser.Walk(tree.File, func(node syntax.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		parent := syntax.Node(nil)
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, node)
		switch n := node.(type) {
		case *syntax.SglQuoted:
			if span, ok := tree.NodeSpan(n); ok {
				g.add(span, TypeString, 0)
			}
		case *syntax.DblQuoted:
			g.doubleQuoted(n)
		case *syntax.ParamExp:
			if n.Param != nil && !parser.IsName(n.Param.Value) {
				if span, ok := tree.NodeSpan(n.Param); ok {
					g.add(span, TypeParameter, 0)
				}
			}
		case *syntax.Redirect:
			g.heredoc(n)
			if n.N != nil {
				if span, ok := tree.NodeSpan(n.N); ok && parser.IsDigits(n.N.Value) {
					g.add(span, TypeNumber, 0)
				}
			}
		case *syntax.BinaryCmd:
			g.operator(n.OpPos, n.Op.String())
		case *syntax.BinaryArithm:
			g.operator(n.OpPos, n.Op.String())
		case *syntax.UnaryArithm:
			g.operator(n.OpPos, n.Op.String())
		case *syntax.BinaryTest:
			g.operator(n.OpPos, n.Op.String())
		case *syntax.UnaryTest:
			g.operator(n.OpPos, n.Op.String())
		case *syntax.Word:
			if lit := n.Lit(); isNumber(lit) && parser.IsArithmetic(parent) {
				if span, ok := tree.NodeSpan(n); ok {
					g.add(span, TypeNumber, 0)
				}
			}
		}
		return true
	})
}

func (g *generator) doubleQuoted(q *syntax.DblQuoted) {
	if !q.Left.IsValid() {
		return
	}
	left := parser.Offset(q.Left)
	width := 1
	if q.Dollar {
		width = 2
	}
	g.add(parser.Span{Start: left, End: left + width}, TypeString, 0)
	for _, part := range q.Parts {
		if lit, ok := part.(*syntax.Lit); ok {
			if span, ok := g.tree.NodeSpan(lit); ok {
				g.add(span, TypeString, 0)
			}
		}
	}
	if q.Right.IsValid() {
		right := parser.Offset(q.Right)
		g.add(parser.Span{Start: right, End: right + 1}, TypeString, 0)
	}
}

func (g *generator) heredoc(r *syntax.Redirect) {
	if r.Hdoc == nil || (r.Op != syntax.Hdoc && r.Op != syntax.DashHdoc) {
		return
	}
	body, ok := g.tree.NodeSpan(r.Hdoc)
	if !ok {
		return
	}
	bodyEnd := max(body.Start, strings.LastIndexByte(g.text[:body.End], '\n')+1)
	for _, part := range r.Hdoc.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		if span, ok := g.tree.NodeSpan(lit); ok {
			g.add(parser.Span{Start: span.Start, End: min(span.End, bodyEnd)}, TypeString, 0)
		}
	}
	if r.Word != nil {
		if span, ok := g.tree.NodeSpan(r.Word); ok {
			g.add(span, TypeString, 0)
		}
	}
}

func (g *generator) operator(pos syntax.Pos, op string) {
	if pos.IsValid() {
		start := parser.Offset(pos)
		g.add(parser.Span{Start: start, End: start + len(op)}, TypeOperator, 0)
	}
}

func (g *generator) result() []Token {
	order := make([]uint64, len(g.tokens))
	for i, t := range g.tokens {
		order[i] = uint64(t.Span.Start)<<32 | uint64(i) //nolint:gosec
	}
	slices.Sort(order)
	out := make([]Token, 0, len(g.tokens))
	end := -1
	for _, key := range order {
		t := g.tokens[uint32(key)] //nolint:gosec
		if t.Span.Start < end {
			continue
		}
		out = append(out, t)
		end = t.Span.End
	}
	return out
}

func isNumber(s string) bool {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return false
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if hashAt := strings.IndexByte(s, '#'); hashAt > 0 && parser.IsDigits(s[:hashAt]) {
		s = s[hashAt+1:]
	}
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
