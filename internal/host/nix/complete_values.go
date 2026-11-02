package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

const builtinsName = "builtins"

type Files func(path string) (string, bool)

func NoFiles(string) (string, bool) { return "", false }

type origin struct {
	assign  *syntax.Assign
	depth   int
	inherit *syntax.Inherit
	attr    syntax.Attr
}

type reading struct {
	*Document
	files   Files
	formals map[*syntax.Ident]*syntax.Formal
	project *Project
	unit    *symbolTable
}

func newReading(d *Document, chain []syntax.Node, files Files) *reading {
	r := &reading{Document: d, files: files, formals: map[*syntax.Ident]*syntax.Formal{}}
	for _, n := range chain {
		lambda, ok := n.(*syntax.Lambda)
		if !ok {
			continue
		}
		pattern, ok := lambda.Param.(*syntax.PatternParam)
		if !ok {
			continue
		}
		for _, f := range pattern.Formals {
			r.formals[f.Name] = f
		}
	}
	return r
}

func (r *reading) linked() (*Project, *symbolTable) {
	if r.project == nil {
		r.project, r.unit = r.localFlow()
		r.project.peek = r.files
	}
	return r.project, r.unit
}

func (r *reading) eval(e syntax.Expr) []flowValue {
	p, u := r.linked()
	return p.eval(u, e)
}

func closedValues(values []flowValue) bool {
	return len(values) > 0 && !slices.ContainsFunc(values, func(v flowValue) bool {
		switch v.(type) {
		case flowSet, flowLambda, flowBuiltin, flowBuiltins:
			return false
		}
		return true
	})
}

func (r *reading) originsOf(d *scope.Def) []origin {
	out := make([]origin, 0, len(d.Bindings))
	for _, b := range d.Bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			out = append(out, origin{assign: b, depth: 1})
		case *syntax.Inherit:
			for _, attr := range b.Attrs {
				if owner, bound := r.table.Binder(attr); bound && owner == d {
					out = append(out, origin{inherit: b, attr: attr})
				}
			}
		}
	}
	return out
}

func (r *reading) defItem(d *scope.Def) Item {
	it := Item{Label: d.Name, Kind: r.defKind(d), Detail: d.Name}
	if binder, ok := d.Binders[0].(*syntax.Ident); ok {
		if f := r.formals[binder]; f != nil {
			it.Detail = formalDetail(r.tree.Text, f)
			it.Doc = commentAbove(r.tree, f.Span().Start)
		}
		return it
	}
	origins := r.originsOf(d)
	if len(origins) > 0 {
		it.Detail, it.Doc = r.originDetail(origins[0]), r.originDoc(origins[0])
	}
	return it
}

func (r *reading) defKind(d *scope.Def) model.Kind {
	switch d.Kind {
	case scope.ArgDef, scope.FormalDef, scope.AliasDef:
		return model.KindParameter
	case scope.LetDef, scope.InheritDef:
		if bindsFunction(r.originsOf(d)) {
			return model.KindFunction
		}
		return model.KindVariable
	case scope.RecDef:
	}
	if bindsFunction(r.originsOf(d)) {
		return model.KindFunction
	}
	return model.KindAttribute
}

func bindsFunction(origins []origin) bool {
	if len(origins) == 0 || origins[0].assign == nil || origins[0].depth < len(origins[0].assign.Path) {
		return false
	}
	_, ok := syntax.Unparenthesized(origins[0].assign.Value).(*syntax.Lambda)
	return ok
}

func (r *reading) originDetail(o origin) string {
	f := file{src: r.tree.Text}
	if o.assign != nil {
		return f.detail(binding{name: nameSpan(o.assign.Path[o.depth-1]), span: o.assign.Span()})
	}
	return f.detail(binding{name: nameSpan(o.attr), span: o.inherit.Span(), inherit: true})
}

func (r *reading) originDoc(o origin) string {
	if o.assign != nil {
		return commentAbove(r.tree, o.assign.Span().Start)
	}
	return commentAbove(r.tree, o.inherit.Span().Start)
}

func formalDetail(text string, f *syntax.Formal) string {
	span := f.Span()
	detail := text[span.Start:span.End]
	if strings.Contains(detail, "\n") {
		return f.Name.Name
	}
	return detail
}

func commentAbove(tree *syntax.File, offset int) string {
	text := tree.Text
	var lines []string
	for at := offset; ; {
		c, ok := tree.CommentBefore(at)
		if !ok || strings.TrimSpace(text[textline.Start(text, c.Span.Start):c.Span.Start]) != "" || strings.Count(text[c.Span.End:at], "\n") > 1 {
			break
		}
		at = c.Span.Start
		if c.Kind == syntax.LineComment {
			if _, isDirective := parsedDirective(tree, c); isDirective {
				continue
			}
			lines = append(lines, strings.TrimSpace(text[c.Body.Start:c.Body.End]))
			continue
		}
		lines = append(lines, strings.TrimSpace(text[c.Body.Start:c.Body.End]))
		break
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n")
}
