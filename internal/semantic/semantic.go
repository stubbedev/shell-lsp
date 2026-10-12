package semantic

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
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
	TypeRegexp
)

var TypeNames = [...]string{
	TypeVariable:  "variable",
	TypeParameter: "parameter",
	TypeFunction:  "function",
	TypeKeyword:   "keyword",
	TypeString:    "string",
	TypeNumber:    "number",
	TypeComment:   "comment",
	TypeOperator:  "operator",
	TypeProperty:  "property",
	TypeRegexp:    "regexp",
}

type Modifier uint32

const (
	ModDeclaration Modifier = 1 << iota
	ModModification
	ModReadonly
	ModDefaultLibrary
	ModExternal
)

var ModifierNames = bitNames(map[Modifier]string{
	ModDeclaration:    "declaration",
	ModModification:   "modification",
	ModReadonly:       "readonly",
	ModDefaultLibrary: "defaultLibrary",
	ModExternal:       "external",
})

func bitNames(named map[Modifier]string) []string {
	names := make([]string, len(named))
	for bit := range names {
		name, ok := named[1<<bit]
		if !ok {
			panic(fmt.Sprintf("semantic: modifier bit %d has no name", bit))
		}
		names[bit] = name
	}
	return names
}

type Token struct {
	Span      parser.Span
	Type      Type
	Modifiers Modifier
}

func Tokens(tree *parser.Tree, imports []parser.Import, programs []embedded.Program) []Token {
	g := generator{tree: tree, imports: imports, text: tree.Text()}
	g.tokens = make([]Token, 0, 2*(len(tree.Comments)+len(tree.Keywords)+len(tree.Definitions)+len(tree.References)+2*len(tree.Commands))+embeddedTokens(programs))
	g.index()
	g.walk()
	g.embed(programs)
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
		g.comment(c.Span)
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
	for _, doc := range tree.Heredocs {
		g.heredoc(doc)
	}
}

func (g *generator) comment(span parser.Span) {
	text := g.text[span.Start:span.End]
	d, ok := directive.Parse(text)
	if !ok {
		g.add(span, TypeComment, 0)
		return
	}
	end, _ := directive.Header(text)
	parts := []Token{{Span: parser.Span{Start: end - len(directive.Word), End: end}, Type: TypeKeyword}}
	for _, pair := range d.Pairs {
		equals := pair.KeyOffset + len(pair.Name)
		parts = append(parts,
			Token{Span: parser.Span{Start: pair.KeyOffset, End: equals}, Type: TypeProperty},
			Token{Span: parser.Span{Start: equals, End: equals + 1}, Type: TypeOperator},
			Token{Span: parser.Span{Start: pair.ValueOffset, End: pair.ValueEnd}, Type: TypeString},
		)
	}
	at := 0
	for _, part := range parts {
		g.commentGap(span.Start, text[at:part.Span.Start], at)
		g.add(parser.Span{Start: span.Start + part.Span.Start, End: span.Start + part.Span.End}, part.Type, 0)
		at = part.Span.End
	}
	g.commentGap(span.Start, text[at:], at)
}

func (g *generator) commentGap(base int, gap string, at int) {
	if strings.TrimSpace(gap) != "" {
		g.add(parser.Span{Start: base + at, End: base + at + len(gap)}, TypeComment, 0)
	}
}

func (g *generator) referenceModifiers(ref parser.Reference) Modifier {
	attrs := g.tree.VisibleAttrs(ref.Name, ref.Within)
	if g.tree.Visible(ref.Name, ref.Within).Len() == 0 {
		imp, _, ok := parser.ImportedGlobal(g.imports, ref.Name)
		if !ok {
			if shelldocs.KnownParameters(g.tree.Dialect).Has(ref.Name) {
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

func (g *generator) heredoc(doc parser.Heredoc) {
	r := doc.Redirect
	if r.Hdoc == nil {
		return
	}
	for _, part := range r.Hdoc.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		if span, ok := g.tree.NodeSpan(lit); ok {
			g.add(parser.Span{Start: span.Start, End: min(span.End, doc.Body.End)}, TypeString, 0)
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

var embeddedTypes = [...]Type{
	lang.Keyword:   TypeKeyword,
	lang.String:    TypeString,
	lang.Number:    TypeNumber,
	lang.Regexp:    TypeRegexp,
	lang.Variable:  TypeVariable,
	lang.Parameter: TypeParameter,
	lang.Function:  TypeFunction,
	lang.Property:  TypeProperty,
	lang.Operator:  TypeOperator,
	lang.Comment:   TypeComment,
}

func embeddedModifiers(mods lang.Modifier) Modifier {
	var out Modifier
	if mods&lang.Builtin != 0 {
		out |= ModDefaultLibrary
	}
	if mods&lang.Declaration != 0 {
		out |= ModDeclaration
	}
	return out
}

func embeddedTokens(programs []embedded.Program) int {
	n := 0
	for i := range programs {
		n += len(programs[i].Result.Tokens)
	}
	return n
}

func (g *generator) embed(programs []embedded.Program) {
	if len(programs) == 0 {
		return
	}
	var regions []parser.Span
	for i := range programs {
		programs[i].Regions(func(s parser.Span) { regions = append(regions, s) })
	}
	slices.SortFunc(regions, func(a, b parser.Span) int { return cmp.Compare(a.Start, b.Start) })
	g.tokens = clip(g.tokens, regions)
	for i := range programs {
		p := &programs[i]
		for _, t := range p.Result.Tokens {
			kind, mods := embeddedTypes[t.Kind], embeddedModifiers(t.Modifiers)
			p.Spans(t.Start, t.End, func(s parser.Span) { g.add(s, kind, mods) })
		}
	}
}

func clip(tokens []Token, regions []parser.Span) []Token {
	out := tokens[:0]
	var pieces []Token
	for _, t := range tokens {
		first := sort.Search(len(regions), func(i int) bool { return regions[i].End > t.Span.Start })
		if first == len(regions) || regions[first].Start >= t.Span.End {
			out = append(out, t)
			continue
		}
		for _, r := range regions[first:] {
			if r.Start >= t.Span.End {
				break
			}
			if t.Span.Start < r.Start {
				pieces = append(pieces, Token{Span: parser.Span{Start: t.Span.Start, End: r.Start}, Type: t.Type, Modifiers: t.Modifiers})
			}
			t.Span.Start = max(t.Span.Start, r.End)
		}
		if t.Span.Start < t.Span.End {
			pieces = append(pieces, t)
		}
	}
	return append(out, pieces...)
}

func (g *generator) result() []Token {
	permute(g.tokens, orderByStart(g.tokens, len(g.text)))
	out := g.tokens[:0]
	end := -1
	for _, t := range g.tokens {
		if t.Span.Start < end {
			continue
		}
		out = append(out, t)
		end = t.Span.End
	}
	return out
}

const placed = ^uint64(0)

func permute(tokens []Token, order []uint64) {
	for i := range order {
		if order[i] == placed {
			continue
		}
		held := tokens[i]
		at := i
		for {
			from := int(uint32(order[at])) //nolint:gosec
			order[at] = placed
			if from == i {
				tokens[at] = held
				break
			}
			tokens[at] = tokens[from]
			at = from
		}
	}
}

func orderByStart(tokens []Token, maxStart int) []uint64 {
	order := make([]uint64, len(tokens))
	for i, t := range tokens {
		order[i] = uint64(t.Span.Start)<<32 | uint64(i) //nolint:gosec
	}
	scratch := make([]uint64, len(tokens))
	for shift := 32; maxStart>>(shift-32) > 0; shift += 8 {
		var offsets [257]int
		for _, key := range order {
			offsets[key>>shift&0xff+1]++
		}
		for digit := 1; digit < len(offsets); digit++ {
			offsets[digit] += offsets[digit-1]
		}
		for _, key := range order {
			digit := key >> shift & 0xff
			scratch[offsets[digit]] = key
			offsets[digit]++
		}
		order, scratch = scratch, order
	}
	return order
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
