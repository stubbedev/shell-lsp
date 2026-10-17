package justfile

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

const (
	defaultIndentation   = "    "
	maxLeadingBlankLines = 2
)

var ErrUnformattable = errors.New("cannot format the justfile")

type printer struct {
	f      *File
	sb     strings.Builder
	indent string
}

type element struct {
	start, end int
	item       *Item
	comment    *Comment
	trailing   *Comment
}

func Format(f *File) (string, error) {
	if len(f.Errors) > 0 {
		e := f.Errors[0]
		return "", fmt.Errorf("%w: line %d does not parse: %s", ErrUnformattable, f.line(e.Span.Start), e.Message)
	}
	for _, finding := range f.check() {
		if finding.Severity == lint.SeverityError {
			return "", fmt.Errorf("%w: just rejects line %d: %s", ErrUnformattable, f.line(finding.Span.Start), finding.Message)
		}
	}
	p := &printer{f: f, indent: defaultIndentation}
	if s, ok := f.setting("indentation"); ok && s.Value != nil && s.Value.Kind == ExprString {
		p.indent = s.Value.Str.Value
	}
	elements, err := p.elements()
	if err != nil {
		return "", err
	}
	if len(elements) == 0 {
		return strings.Repeat("\n", min(newlines(f.Text), 1)), nil
	}
	p.sb.WriteString(strings.Repeat("\n", min(newlines(f.Text[:elements[0].start]), maxLeadingBlankLines)))
	for i, el := range elements {
		if i > 0 && blankBetween(f.Text[elements[i-1].end:el.start]) {
			p.sb.WriteString("\n")
		}
		switch {
		case el.comment != nil && i+1 < len(elements) && p.documents(el, elements[i+1]):
			p.sb.WriteString("# " + docOf(*elements[i+1].item))
		case el.comment != nil:
			p.sb.WriteString(trimComment(el.comment.Text))
		default:
			p.item(*el.item)
		}
		if el.trailing != nil {
			p.sb.WriteString(" " + trimComment(el.trailing.Text))
		}
		p.sb.WriteString("\n")
	}
	return p.sb.String(), nil
}

func (f *File) line(offset int) int { return strings.Count(f.Text[:offset], "\n") + 1 }

func trimComment(text string) string { return strings.TrimRightFunc(text, unicode.IsSpace) }

func docOf(it Item) string {
	switch it.Kind {
	case ItemRecipe:
		return it.Recipe.Doc
	case ItemModule:
		return it.Module.Doc
	case ItemSetting, ItemAssignment, ItemAlias, ItemImport, ItemFunction, ItemUnexport:
	}
	return ""
}

func (p *printer) documents(comment, next element) bool {
	if next.item == nil || comment.start == 0 && strings.HasPrefix(p.f.Text, "#!") || strings.Count(p.f.Text[comment.end:next.start], "\n") != 1 {
		return false
	}
	_, attributed := attributeNamed(attributesOf(*next.item), "doc")
	return docOf(*next.item) != "" && !attributed
}

func newlines(text string) int {
	r := &reader{text: text, file: &File{}}
	n := 0
	for t := r.next(); t.kind != tokEOF; t = r.next() {
		if t.kind == tokNewline {
			n++
		}
	}
	return n
}

func blankBetween(s string) bool { return strings.Count(s, "\n") > 1 }

func attributesOf(it Item) []Attribute {
	switch it.Kind {
	case ItemSetting:
		return it.Setting.Attributes
	case ItemAssignment:
		return it.Assignment.Attributes
	case ItemAlias:
		return it.Alias.Attributes
	case ItemImport:
		return it.Import.Attributes
	case ItemModule:
		return it.Module.Attributes
	case ItemFunction:
		return it.Function.Attributes
	case ItemRecipe:
		return it.Recipe.Attributes
	case ItemUnexport:
	}
	return nil
}

func (p *printer) lineEnd(offset int) int {
	if i := strings.IndexByte(p.f.Text[offset:], '\n'); i >= 0 {
		return offset + i
	}
	return len(p.f.Text)
}

func (p *printer) elements() ([]element, error) {
	var out []element
	comments := slices.Clone(p.f.Comments)
	if len(comments) > 0 && strings.HasPrefix(p.f.Text, "#!") && comments[0].Span.Start == 0 {
		out = append(out, element{start: 0, end: comments[0].Span.End, comment: &comments[0]})
		comments = comments[1:]
	}
	used := make([]bool, len(comments))
	for i := range p.f.Items {
		it := &p.f.Items[i]
		span := it.Span()
		el := element{start: span.Start, end: span.End, item: it}
		for _, a := range attributesOf(*it) {
			el.start = min(el.start, p.lineStart(a.Span.Start))
		}
		header := span.End
		if it.Kind == ItemRecipe {
			header = it.Recipe.Header.End
		}
		for j, c := range comments {
			switch {
			case c.Span.Start >= el.start && c.Span.Start < header:
				return nil, fmt.Errorf("%w: `just --fmt` would drop the comment on line %d; move it to a line of its own above", ErrUnformattable, p.f.line(c.Span.Start))
			case it.Kind == ItemRecipe && c.Span.Start >= header && c.Span.Start <= p.lineEnd(header):
				return nil, fmt.Errorf("%w: `just --fmt` would drop the comment after the recipe header on line %d; move it to a line of its own above the recipe", ErrUnformattable, p.f.line(c.Span.Start))
			case it.Kind != ItemRecipe && c.Span.Start >= span.End && c.Span.Start <= p.lineEnd(span.End):
				el.trailing, used[j] = &comments[j], true
				el.end = c.Span.End
			}
		}
		out = append(out, el)
	}
	for j := range comments {
		if !used[j] {
			out = append(out, element{start: comments[j].Span.Start, end: comments[j].Span.End, comment: &comments[j]})
		}
	}
	slices.SortStableFunc(out, func(a, b element) int { return a.start - b.start })
	return out, nil
}

func (p *printer) lineStart(offset int) int {
	return strings.LastIndexByte(p.f.Text[:offset], '\n') + 1
}

func (p *printer) text(s Span) string { return p.f.Text[s.Start:s.End] }

func (p *printer) item(it Item) {
	attrs := slices.Clone(attributesOf(it))
	slices.SortStableFunc(attrs, p.compareAttributes)
	for _, a := range attrs {
		p.sb.WriteString("[" + p.attribute(a) + "]\n")
	}
	switch it.Kind {
	case ItemSetting:
		s := it.Setting
		p.sb.WriteString("set " + s.Name.Text)
		if s.Value != nil && (s.Value.Kind != ExprVariable || s.Value.Name.Text != "true") {
			p.sb.WriteString(" := " + p.expr(s.Value))
		}
	case ItemAssignment:
		a := it.Assignment
		switch {
		case a.Export:
			p.sb.WriteString("export ")
		case a.Eager:
			p.sb.WriteString("eager ")
		}
		p.sb.WriteString(a.Name.Text + " := " + p.expr(a.Value))
	case ItemUnexport:
		p.sb.WriteString("unexport " + it.Unexport.Name.Text)
	case ItemAlias:
		p.sb.WriteString("alias " + it.Alias.Name.Text + " := " + it.Alias.Target.String())
	case ItemImport:
		p.sb.WriteString("import" + optional(it.Import.Optional) + " " + p.expr(it.Import.Path))
	case ItemModule:
		m := it.Module
		p.sb.WriteString("mod" + optional(m.Optional) + " " + m.Name.Text)
		if m.Path != nil {
			p.sb.WriteString(" " + p.expr(m.Path))
		}
	case ItemFunction:
		fn := it.Function
		names := make([]string, len(fn.Params))
		for i, n := range fn.Params {
			names[i] = n.Text
		}
		p.sb.WriteString(fn.Name.Text + "(" + strings.Join(names, ", ") + ") := " + p.expr(fn.Body))
	case ItemRecipe:
		p.recipe(it.Recipe)
	}
}

func (p *printer) compareAttributes(a, b Attribute) int {
	if c := strings.Compare(a.Name.Text, b.Name.Text); c != 0 {
		return c
	}
	if a.Name.Text == "arg" {
		return cmp.Or(p.compareHelp(a.keyed("help"), b.keyed("help")), compareLong(a, b), strings.Compare(a.first(), b.first()))
	}
	return p.compareArguments(a.Args, b.Args)
}

func (p *printer) compareArguments(a, b []AttributeArg) int {
	for i := range min(len(a), len(b)) {
		if c := p.compareLiteral(a[i].Value, b[i].Value); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

func (p *printer) compareLiteral(a, b *Expr) int {
	if a == nil || b == nil || a.Kind != ExprString || b.Kind != ExprString {
		return compareBool(a != nil, b != nil)
	}
	return cmp.Or(strings.Compare(a.Str.Value, b.Str.Value), cmp.Compare(quoteRank(a.Str), quoteRank(b.Str)), p.comparePosition(a, b))
}

func quoteRank(s *StringLit) int {
	rank := 0
	if s.Raw() {
		rank = 2
	}
	if s.Indented() {
		rank++
	}
	return rank
}

func (p *printer) comparePosition(a, b *Expr) int {
	return cmp.Or(cmp.Compare(p.column(a.Span.Start), p.column(b.Span.Start)), cmp.Compare(a.Span.Start, b.Span.Start))
}

func (p *printer) compareHelp(a, b *Expr) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return p.comparePosition(a, b)
}

func (p *printer) column(offset int) int {
	return utf8.RuneCountInString(p.f.Text[p.lineStart(offset):offset])
}

func compareLong(a, b Attribute) int {
	la, oka := a.long()
	lb, okb := b.long()
	return cmp.Or(compareBool(oka, okb), strings.Compare(la, lb))
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func optional(yes bool) string {
	if yes {
		return "?"
	}
	return ""
}

func (p *printer) attribute(a Attribute) string {
	if len(a.Args) == 0 {
		return a.Name.Text
	}
	args := make([]string, len(a.Args))
	for i, arg := range a.Args {
		switch {
		case arg.Key.Text != "" && arg.Value != nil:
			args[i] = arg.Key.Text + "=" + p.expr(arg.Value)
		case arg.Key.Text != "":
			args[i] = arg.Key.Text
		default:
			args[i] = p.expr(arg.Value)
		}
	}
	return a.Name.Text + "(" + strings.Join(args, ", ") + ")"
}

func (p *printer) recipe(r *Recipe) {
	if r.Quiet {
		p.sb.WriteString("@")
	}
	p.sb.WriteString(r.Name.Text)
	for _, param := range r.Params {
		p.sb.WriteString(" ")
		switch param.Variadic {
		case Plus:
			p.sb.WriteString("+")
		case Star:
			p.sb.WriteString("*")
		case Singular:
		}
		if param.Export {
			p.sb.WriteString("$")
		}
		p.sb.WriteString(param.Name.Text)
		if param.Default != nil {
			p.sb.WriteString("=" + p.expr(param.Default))
		}
	}
	p.sb.WriteString(":")
	p.dependencies(r.Deps)
	if len(r.Subsequent) > 0 {
		p.sb.WriteString(" &&")
		p.dependencies(r.Subsequent)
	}
	for _, l := range r.Body {
		p.sb.WriteString("\n")
		if len(l.Fragments) == 0 {
			continue
		}
		p.sb.WriteString(p.indent + strings.TrimPrefix(p.text(l.Indent), r.Indent))
		p.fragments(l, r)
	}
}

func (p *printer) fragments(l Line, r *Recipe) {
	for _, frag := range l.Fragments {
		switch frag.Kind {
		case FragmentText, FragmentEscape:
			p.sb.WriteString(strings.ReplaceAll(p.text(frag.Span), "\n"+r.Indent, "\n"+p.indent))
		case FragmentInterpolation:
			p.sb.WriteString("{{ " + p.expr(frag.Interp) + " }}")
		}
	}
}

func (p *printer) dependencies(deps []Dependency) {
	for _, d := range deps {
		p.sb.WriteString(" ")
		if !d.Paren || len(d.Args) == 0 && !d.Star {
			p.sb.WriteString(d.Target.String())
			continue
		}
		if d.Star {
			p.sb.WriteString("*")
		}
		p.sb.WriteString("(" + d.Target.String())
		for _, a := range d.Args {
			p.sb.WriteString(" " + p.expr(a))
		}
		p.sb.WriteString(")")
	}
}

func (p *printer) expr(e *Expr) string {
	var out string
	switch e.Kind {
	case ExprString:
		out = p.stringLit(e)
	case ExprBacktick:
		out = p.text(e.Span)
	case ExprVariable:
		out = e.Name.Text
	case ExprCall:
		args := make([]string, len(e.Args))
		for i, a := range e.Args {
			args[i] = p.expr(a)
		}
		out = e.Name.Text + "(" + strings.Join(args, ", ") + ")"
	case ExprBinary:
		out = p.expr(e.Args[0]) + " " + e.Op + " " + p.expr(e.Args[1])
	case ExprNot:
		out = "!" + p.expr(e.Args[0])
	case ExprRoot:
		out = "/ " + p.expr(e.Args[0])
	case ExprIf:
		out = "if " + p.expr(e.Args[0]) + " " + p.block(e.Args[1])
		if len(e.Args) > 2 {
			out += " else "
			if e.Args[2].Kind == ExprIf {
				out += p.expr(e.Args[2])
			} else {
				out += p.block(e.Args[2])
			}
		}
	case ExprList:
		items := make([]string, len(e.Args))
		for i, a := range e.Args {
			items[i] = p.expr(a)
		}
		out = "[" + strings.Join(items, ", ") + "]"
	case ExprGroup:
		out = "(" + p.expr(e.Args[0]) + ")"
	case ExprBad:
		out = p.text(e.Span)
	}
	if e.Starred {
		return "*" + out
	}
	return out
}

func (p *printer) block(e *Expr) string { return "{ " + p.expr(e.Args[0]) + " }" }

func (p *printer) stringLit(e *Expr) string {
	lit := e.Str
	if lit.Prefix != 'f' {
		return p.text(e.Span)
	}
	var sb strings.Builder
	sb.WriteString(p.f.Text[e.Span.Start:lit.Inner.Start])
	for _, seg := range lit.Segments {
		if seg.Kind == FragmentInterpolation {
			sb.WriteString("{{ " + p.expr(seg.Interp) + " }}")
			continue
		}
		sb.WriteString(p.text(seg.Span))
	}
	sb.WriteString(p.f.Text[lit.Inner.End:e.Span.End])
	return sb.String()
}
