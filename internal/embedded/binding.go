package embedded

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type ValueKind uint8

const (
	ValueOther ValueKind = iota
	ValueVariable
	ValueHole
)

type Binding struct {
	Name     string
	Flag     string
	Span     parser.Span
	Text     string
	Kind     ValueKind
	Read     string
	ReadSpan parser.Span
}

func bindingsOf(tree *parser.Tree, flags []flagged) []Binding {
	out := make([]Binding, 0, len(flags))
	for _, f := range flags {
		if f.value != nil {
			if b, ok := pairedBinding(tree, f); ok {
				out = append(out, b)
			}
			continue
		}
		if b, ok := assignedBinding(tree, f); ok {
			out = append(out, b)
		}
	}
	return out
}

func pairedBinding(tree *parser.Tree, f flagged) (Binding, bool) {
	name, start, ok := literalWord(f.name)
	if !ok {
		return Binding{}, false
	}
	b := Binding{Name: name, Flag: f.flag, Span: parser.Span{Start: start, End: start + len(name)}}
	value, ok := tree.NodeSpan(f.value)
	if !ok {
		return Binding{}, false
	}
	b.readValue(f.value.Parts)
	b.Text = tree.Text()[value.Start:value.End]
	return b, true
}

func assignedBinding(tree *parser.Tree, f flagged) (Binding, bool) {
	first, ok := f.name.Parts[0].(*syntax.Lit)
	if !ok || f.skip > len(first.Value) {
		return Binding{}, false
	}
	name, rest, assigned := strings.Cut(first.Value[f.skip:], "=")
	if !assigned || !parser.IsName(name) {
		return Binding{}, false
	}
	start := parser.Offset(first.ValuePos) + f.skip
	b := Binding{Name: name, Flag: f.flag, Span: parser.Span{Start: start, End: start + len(name)}}
	if rest == "" {
		b.readValue(f.name.Parts[1:])
	}
	word, ok := tree.NodeSpan(f.name)
	if !ok {
		return Binding{}, false
	}
	b.Text = tree.Text()[b.Span.End+len("=") : word.End]
	return b, true
}

func literalWord(w *syntax.Word) (string, int, bool) {
	if len(w.Parts) != 1 {
		return "", 0, false
	}
	switch p := w.Parts[0].(type) {
	case *syntax.Lit:
		return p.Value, parser.Offset(p.ValuePos), parser.IsName(p.Value)
	case *syntax.SglQuoted:
		return p.Value, parser.Offset(p.Left) + 1, !p.Dollar && parser.IsName(p.Value)
	case *syntax.DblQuoted:
		if lit, ok := soleLit(p); ok {
			return lit.Value, parser.Offset(lit.ValuePos), parser.IsName(lit.Value)
		}
	}
	return "", 0, false
}

func soleLit(q *syntax.DblQuoted) (*syntax.Lit, bool) {
	if q.Dollar || len(q.Parts) != 1 {
		return nil, false
	}
	lit, ok := q.Parts[0].(*syntax.Lit)
	return lit, ok
}

func (b *Binding) readValue(parts []syntax.WordPart) {
	if len(parts) == 1 {
		if q, ok := parts[0].(*syntax.DblQuoted); ok && !q.Dollar {
			parts = q.Parts
		}
	}
	if len(parts) != 1 {
		return
	}
	p, ok := parts[0].(*syntax.ParamExp)
	if !ok || p.Param == nil {
		return
	}
	if DynamicValue(p) {
		at := parser.Offset(p.Dollar)
		b.Kind, b.ReadSpan = ValueHole, parser.Span{Start: at, End: at + len(dynamicText)}
		return
	}
	at := parser.Offset(p.Param.ValuePos)
	b.Kind, b.Read, b.ReadSpan = ValueVariable, p.Param.Value, parser.Span{Start: at, End: at + len(p.Param.Value)}
}

func (p *Program) variable(tok lang.Token) (string, bool) {
	if tok.Kind != lang.Variable || tok.Modifiers&lang.Builtin != 0 {
		return "", false
	}
	sigil := languages[p.Language].sigil
	return strings.CutPrefix(p.Text[tok.Start:tok.End], sigil)
}

func (p *Program) VariableAt(source int) (string, bool) {
	at, ok := p.offset(source)
	if !ok {
		return "", false
	}
	tok, ok := p.Result.TokenAt(at)
	if !ok || tok.Modifiers&lang.Declaration != 0 {
		return "", false
	}
	return p.variable(tok)
}

func (p *Program) Reads(name string) []parser.Span {
	sigil := len(languages[p.Language].sigil)
	var out []parser.Span
	for _, tok := range p.Result.Tokens {
		if read, ok := p.variable(tok); ok && read == name && tok.Modifiers&lang.Declaration == 0 {
			out = append(out, p.Span(tok.Start+sigil, tok.End))
		}
	}
	return out
}

func (p *Program) Binding(name string) (Binding, bool) {
	for _, b := range p.Bindings {
		if b.Name == name {
			return b, true
		}
	}
	return Binding{}, false
}

func (p *Program) BindingAt(source int) (Binding, bool) {
	for _, b := range p.Bindings {
		if b.Span.Touches(source) {
			return b, true
		}
	}
	return Binding{}, false
}

func (p *Program) Shadows(name string) bool {
	for _, tok := range p.Result.Tokens {
		if tok.Modifiers&lang.Declaration == 0 {
			continue
		}
		if strings.TrimPrefix(p.Text[tok.Start:tok.End], languages[p.Language].sigil) == name {
			return true
		}
	}
	return false
}

func (p *Program) Taken(name string) bool {
	_, bound := p.Binding(name)
	return bound || len(p.Reads(name)) > 0
}

func (p *Program) RenameRefusal(name, text string, spans []parser.Span) string {
	if p.NamesArguments() {
		return fmt.Sprintf("this %s program also reads its arguments through `$ARGS`, whose keys are not followed", p.Language)
	}
	for _, span := range spans {
		if text[span.Start:span.End] != name {
			return fmt.Sprintf("`%s` is written across an antiquotation or an escape here", name)
		}
	}
	return ""
}

func (p *Program) NamesArguments() bool {
	if p.Language != Jq {
		return false
	}
	return slices.ContainsFunc(p.Result.Tokens, func(tok lang.Token) bool {
		if tok.Kind != lang.Variable {
			return false
		}
		text := p.Text[tok.Start:tok.End]
		return text == "$ARGS" || text == "$__prog_args"
	})
}

func (p *Program) boundHover(tok lang.Token) (Hover, bool) {
	name, ok := p.variable(tok)
	if !ok || p.Shadows(name) {
		return Hover{}, false
	}
	b, bound := p.Binding(name)
	if !bound {
		return Hover{}, false
	}
	return Hover{Markdown: fmt.Sprintf("`%s` is set by `%s %s` on the command line of this %s program, as `%s`.", name, b.Flag, b.Name, p.Language, b.Text), Span: p.Span(tok.Start, tok.End)}, true
}
