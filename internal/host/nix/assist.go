package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/similar"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

func (c *checker) undefinedFixes(u scope.Use) []lint.Fix {
	var out []lint.Fix
	span := u.Node.Span()
	names := c.table.NamesAt(span.Start).Visible
	candidates := func(yield func(string) bool) {
		for _, name := range names {
			if !yield(name.Name) {
				return
			}
		}
	}
	if closest, found := similar.Closest(u.Name, candidates); found {
		out = append(out, c.offer("Change to `"+closest+"`", lint.Review, textedit.Edit{Start: span.Start, End: span.End, NewText: closest})...)
	}
	if edit, found := c.addedFormal(u.Name, span.Start); found {
		out = append(out, c.offer("Add `"+u.Name+"` to the arguments of this function", lint.Review, edit)...)
	}
	return out
}

func (c *checker) addedFormal(name string, at int) (textedit.Edit, bool) {
	var pattern *syntax.PatternParam
	visit := func(n syntax.Node) bool {
		s := n.Span()
		if at < s.Start || at >= s.End {
			return false
		}
		if lambda, isLambda := n.(*syntax.Lambda); isLambda {
			if p, patterned := lambda.Param.(*syntax.PatternParam); patterned && p.Closed && lambda.Body.Span().Start <= at {
				pattern = p
			}
		}
		return true
	}
	syntax.Inspect(c.tree.Root, visit)
	for _, stray := range c.tree.Stray {
		syntax.Inspect(stray, visit)
	}
	if pattern == nil {
		return textedit.Edit{}, false
	}
	return formalInsertion(c.tree.Text, pattern, name), true
}

func formalInsertion(text string, pattern *syntax.PatternParam, name string) textedit.Edit {
	if len(pattern.Formals) == 0 {
		open := pattern.Span().Start
		if pattern.Alias != nil && pattern.AliasFirst {
			open = pattern.Alias.Span().End
		}
		brace := open + strings.IndexByte(text[open:], '{') + 1
		at := skipBlank(text, brace)
		if pattern.Ellipsis {
			return textedit.Edit{Start: at, End: at, NewText: name + ", "}
		}
		return textedit.Edit{Start: brace, End: brace, NewText: " " + name}
	}
	last := pattern.Formals[len(pattern.Formals)-1].Span()
	start := textline.Start(text, last.Start)
	if strings.TrimLeft(text[start:last.Start], " \t") == "" && strings.IndexByte(text[pattern.Span().Start:last.Start], '\n') >= 0 {
		return textedit.Edit{Start: last.End, End: last.End, NewText: ",\n" + text[start:last.Start] + name}
	}
	return textedit.Edit{Start: last.End, End: last.End, NewText: ", " + name}
}

func (d *Document) stringAt(selection model.Span) (*syntax.String, bool) {
	var found *syntax.String
	d.inspect(func(n syntax.Node) bool {
		s := n.Span()
		if s.Start > selection.Start || selection.End > s.End {
			return false
		}
		if str, isString := n.(*syntax.String); isString {
			found = str
		}
		return true
	})
	return found, found != nil && found.Closed
}

func (d *Document) rewriteString(selection model.Span) (Refactoring, bool) {
	str, ok := d.stringAt(selection)
	if !ok {
		return Refactoring{}, false
	}
	f := &file{src: d.tree.Text, tree: d.tree, table: d.table}
	var rewritten, title string
	switch str.Kind {
	case syntax.Quoted:
		rewritten, ok = f.asIndented(str)
		title = "Rewrite as an indented string"
	case syntax.Indented:
		rewritten, ok = f.asQuoted(str)
		title = "Rewrite as a quoted string"
	}
	if !ok || !f.sameValue(str, rewritten) {
		return Refactoring{}, false
	}
	span := str.Span()
	return d.guarded(title, RefactorRewrite, textedit.Edit{Start: span.Start, End: span.End, NewText: rewritten})
}

func (f *file) sameValue(str *syntax.String, rewritten string) bool {
	tree := syntax.Parse(rewritten)
	parsed, isString := tree.Root.(*syntax.String)
	if !isString || !parsed.Closed || len(tree.Errors) > 0 || len(tree.Stray) > 0 {
		return false
	}
	other := &file{src: rewritten, tree: tree, table: scope.Resolve(tree)}
	return f.standInBody(str).Text() == other.standInBody(parsed).Text()
}

const indentedStep = "  "

func (f *file) asIndented(str *syntax.String) (string, bool) {
	var body strings.Builder
	for _, part := range str.Parts {
		s := part.Span()
		switch part := part.(type) {
		case *syntax.Literal:
			if strings.IndexByte(part.Text, '\n') >= 0 {
				return "", false
			}
			body.WriteString(strings.ReplaceAll(part.Text, "''", "'''"))
		case *syntax.Escape:
			switch part.Text {
			case "\n":
				body.WriteByte('\n')
			case "\t":
				body.WriteString("''\\t")
			case "\r":
				body.WriteString("''\\r")
			case "$":
				body.WriteString("''$")
			case "${":
				body.WriteString("''${")
			default:
				body.WriteString(strings.ReplaceAll(part.Text, "''", "'''"))
			}
		case *syntax.Interpolation:
			body.WriteString(f.src[s.Start:s.End])
		}
	}
	text := body.String()
	if strings.IndexByte(text, '\n') < 0 {
		return "''" + text + "''", true
	}
	line := f.src[textline.Start(f.src, str.Span().Start):str.Span().Start]
	base := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	var out strings.Builder
	out.WriteString("''\n")
	ended := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			out.WriteString(base + indentedStep + l)
		}
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	if ended {
		out.WriteString("\n" + base)
	}
	out.WriteString("''")
	return out.String(), true
}

var quotedText = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "${", `\${`, "\n", `\n`, "\t", `\t`, "\r", `\r`)

func (f *file) asQuoted(str *syntax.String) (string, bool) {
	var holes []string
	for _, part := range str.Parts {
		if _, isHole := part.(*syntax.Interpolation); isHole {
			s := part.Span()
			holes = append(holes, f.src[s.Start:s.End])
		}
	}
	decoded := f.standInBody(str).Text()
	pieces := strings.Split(decoded, holeStandIn)
	if len(pieces) != len(holes)+1 {
		return "", false
	}
	var out strings.Builder
	out.WriteByte('"')
	for i, piece := range pieces {
		out.WriteString(quotedText.Replace(piece))
		if i < len(holes) {
			out.WriteString(holes[i])
		}
	}
	out.WriteByte('"')
	return out.String(), true
}

func (d *Document) inheritAt(selection model.Span) (*syntax.Inherit, bool) {
	var found *syntax.Inherit
	d.inspect(func(n syntax.Node) bool {
		s := n.Span()
		if s.Start > selection.Start || selection.End > s.End {
			return false
		}
		if stmt, isInherit := n.(*syntax.Inherit); isInherit {
			found = stmt
		}
		return true
	})
	return found, found != nil
}

func (d *Document) toBindings(selection model.Span) (Refactoring, bool) {
	stmt, ok := d.inheritAt(selection)
	if !ok || len(stmt.Attrs) == 0 {
		return Refactoring{}, false
	}
	from := ""
	if stmt.From != nil {
		from = d.text(syntax.Unparenthesized(stmt.From.Inner).Span()) + "."
		if _, atomic := selectable(stmt.From.Inner); !atomic {
			from = d.text(stmt.From.Span()) + "."
		}
	}
	bindings := make([]string, 0, len(stmt.Attrs))
	for _, attr := range stmt.Attrs {
		id, plain := attr.(*syntax.IdentAttr)
		if !plain {
			return Refactoring{}, false
		}
		bindings = append(bindings, id.Name+" = "+from+id.Name+";")
	}
	span := stmt.Span()
	line := d.tree.Text[textline.Start(d.tree.Text, span.Start):span.Start]
	separator := " "
	if strings.TrimLeft(line, " \t") == "" {
		separator = "\n" + line
	}
	title := "Convert to `" + bindings[0] + "`"
	if len(bindings) > 1 {
		title = "Convert this inherit to bindings"
	}
	r, offered := d.guarded(title, RefactorRewrite, textedit.Edit{Start: span.Start, End: span.End, NewText: strings.Join(bindings, separator)})
	r.repeats = true
	return r, offered
}

func selectable(e syntax.Expr) (syntax.Expr, bool) {
	inner := syntax.Unparenthesized(e)
	switch inner.(type) {
	case *syntax.Ident, *syntax.Select, *syntax.AttrSet:
		return inner, true
	}
	return inner, false
}

func (d *Document) toggledAttr(selection model.Span) (Refactoring, bool) {
	site, ok := d.bindingAt(selection)
	if !ok {
		return Refactoring{}, false
	}
	i := slices.IndexFunc(site.assign.Path, func(a syntax.Attr) bool {
		s := a.Span()
		return s.Start <= selection.Start && selection.End <= s.End
	})
	if i < 0 {
		return Refactoring{}, false
	}
	attr := site.assign.Path[i]
	span := attr.Span()
	switch attr := attr.(type) {
	case *syntax.IdentAttr:
		return d.guarded("Quote `"+attr.Name+"`", RefactorRewrite, textedit.Edit{Start: span.Start, End: span.End, NewText: `"` + attr.Name + `"`})
	case *syntax.StringAttr:
		name, static := syntax.AttrName(attr)
		if !static || !ValidName(model.KindAttribute, name) {
			return Refactoring{}, false
		}
		return d.guarded("Remove the quotes from `"+name+"`", RefactorRewrite, textedit.Edit{Start: span.Start, End: span.End, NewText: name})
	}
	return Refactoring{}, false
}
