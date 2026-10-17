package justfile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

const maxDetail = 80

var Checks = model.Checks{Of: rules, Deprecated: lint.CodeJustDeprecated, Missing: lint.CodeJustMissingFile}

func rules(kind model.Kind) model.Rules {
	switch kind {
	case model.KindRecipe:
		return model.Rules{Unknown: lint.CodeJustUnknownRecipe, Arity: lint.CodeJustDependencyArguments, Circular: lint.CodeJustCircular}
	case model.KindAlias:
		return model.Rules{Circular: lint.CodeJustCircular}
	case model.KindVariable:
		return model.Rules{Unknown: lint.CodeJustUnknownVariable, Circular: lint.CodeJustCircular, Unused: lint.CodeJustUnusedVariable}
	case model.KindParameter:
		return model.Rules{Unused: lint.CodeJustUnusedParameter}
	case model.KindFunction:
		return model.Rules{Unknown: lint.CodeJustUnknownFunction, Arity: lint.CodeJustFunctionArguments}
	case model.KindModule:
		return model.Rules{Unknown: lint.CodeJustUnknownModule}
	case model.KindAttribute:
		return model.Rules{Arity: lint.CodeJustAttributeArguments}
	case model.KindSetting, model.KindTarget, model.KindKeyword:
		return model.Rules{}
	}
	panic(fmt.Sprintf("justfile: kind %s has no rules", kind))
}

type indexer struct {
	f      *File
	ix     *model.Index
	scopes map[int]model.Scope
	shells map[*Recipe]bool
}

func (f *File) Index(shells []*Recipe) *model.Index {
	ix := &model.Index{ExportAll: f.Enabled("export"), Broken: len(f.Errors) > 0}
	in := &indexer{f: f, ix: ix, scopes: f.itemScopes(), shells: map[*Recipe]bool{}}
	for _, r := range shells {
		in.shells[r] = true
	}
	positional := f.Enabled("positional-arguments")
	for _, it := range f.Items {
		switch it.Kind {
		case ItemSetting:
			in.setting(it.Setting)
		case ItemAssignment:
			in.assignment(it.Assignment)
		case ItemUnexport:
			in.keyword(it.Unexport.Span.Start, "unexport")
		case ItemAlias:
			in.alias(it.Alias)
		case ItemImport:
			in.importItem(it.Import)
		case ItemModule:
			in.module(it.Module)
		case ItemFunction:
			in.function(it.Function)
		case ItemRecipe:
			in.recipe(it.Recipe, positional)
		}
	}
	in.attributes(f.Stray, model.NoSymbol)
	for _, c := range f.Comments {
		ix.Tokens = append(ix.Tokens, semantic.Token{Span: c.Span, Type: semantic.TypeComment})
	}
	in.commentFolds()
	ix.Findings = f.check()
	slices.SortFunc(ix.Tokens, func(a, b semantic.Token) int { return a.Span.Start - b.Span.Start })
	return ix
}

func (in *indexer) text(s Span) string { return in.f.Text[s.Start:s.End] }

func detail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxDetail {
		return s[:maxDetail-1] + "…"
	}
	return s
}

func (in *indexer) symbol(s model.Symbol) int {
	in.ix.Symbols = append(in.ix.Symbols, s)
	return len(in.ix.Symbols) - 1
}

func (in *indexer) token(span Span, t semantic.Type, mods semantic.Modifier) {
	if span.End > span.Start {
		in.ix.Tokens = append(in.ix.Tokens, semantic.Token{Span: span, Type: t, Modifiers: mods})
	}
}

func (in *indexer) keyword(at int, word string) {
	in.token(Span{Start: at, End: at + len(word)}, semantic.TypeKeyword, 0)
}

func (in *indexer) attributes(attrs []Attribute, owner int) {
	for _, a := range attrs {
		args := len(a.Args)
		if a.Partial {
			args = model.NoArgs
		}
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: a.Name.Text, Kind: model.KindAttribute, Span: a.Name.Span, Args: args, Owner: owner})
		in.token(a.Name.Span, semantic.TypeDecorator, 0)
		for _, arg := range a.Args {
			if arg.Key.Text != "" {
				in.token(arg.Key.Span, semantic.TypeProperty, 0)
			}
			in.expr(arg.Value, model.Global, owner, false)
		}
	}
}

func (in *indexer) setting(s *Setting) {
	in.keyword(s.Span.Start, "set")
	in.attributes(s.Attributes, model.NoSymbol)
	in.ix.Refs = append(in.ix.Refs, model.Ref{Name: s.Name.Text, Kind: model.KindSetting, Span: s.Name.Span, Args: model.NoArgs, Owner: model.NoSymbol})
	in.token(s.Name.Span, semantic.TypeProperty, 0)
	if s.Value != nil && s.Value.Kind == ExprVariable && (s.Value.Name.Text == "true" || s.Value.Name.Text == "false") {
		in.token(s.Value.Span, semantic.TypeKeyword, 0)
		return
	}
	in.expr(s.Value, model.Global, model.NoSymbol, false)
}

func (in *indexer) assignment(a *Assignment) {
	if a.Keyword.End > a.Keyword.Start {
		in.token(a.Keyword, semantic.TypeKeyword, 0)
	}
	i := in.symbol(model.Symbol{
		Name: a.Name.Text, Kind: model.KindVariable, Span: a.Span, Selection: a.Name.Span, Parent: model.NoParent,
		Detail: detail(in.text(Span{Start: a.Name.Span.Start, End: a.Span.End})), Doc: a.Doc,
		Exported: a.Export || in.ix.ExportAll, Unused: !a.Export && !in.ix.ExportAll && !strings.HasPrefix(a.Name.Text, "_"), Arity: model.NoArity,
	})
	in.attributes(a.Attributes, i)
	in.token(a.Name.Span, semantic.TypeVariable, semantic.ModDeclaration)
	in.expr(a.Value, model.Global, i, true)
	in.multiline(a.Span)
}

func (in *indexer) multiline(span Span) {
	if strings.Contains(in.text(span), "\n") {
		in.ix.Folds = append(in.ix.Folds, model.Fold{Span: span})
	}
}

func (in *indexer) alias(a *Alias) {
	in.keyword(a.Span.Start, "alias")
	i := in.symbol(model.Symbol{
		Name: a.Name.Text, Kind: model.KindAlias, Span: a.Span, Selection: a.Name.Span, Parent: model.NoParent,
		Detail: detail(in.text(a.Span)), Arity: model.NoArity,
	})
	in.attributes(a.Attributes, i)
	in.token(a.Name.Span, semantic.TypeFunction, semantic.ModDeclaration)
	in.path(a.Target, i, 0, true)
}

func (in *indexer) path(p Path, owner, args int, edge bool) {
	qualifier := make([]string, 0, len(p)-1)
	for _, part := range p[:len(p)-1] {
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: part.Text, Kind: model.KindModule, Span: part.Span, Qualifier: slices.Clone(qualifier), Args: model.NoArgs, Owner: owner})
		in.token(part.Span, semantic.TypeNamespace, 0)
		qualifier = append(qualifier, part.Text)
	}
	last := p[len(p)-1]
	in.ix.Refs = append(in.ix.Refs, model.Ref{Name: last.Text, Kind: model.KindRecipe, Span: last.Span, Qualifier: qualifier, Args: args, Owner: owner, Edge: edge})
	in.token(last.Span, semantic.TypeFunction, 0)
}

func (in *indexer) importItem(imp *Import) {
	in.keyword(imp.Span.Start, "import")
	in.attributes(imp.Attributes, model.NoSymbol)
	in.expr(imp.Path, model.Global, model.NoSymbol, false)
	inc := model.Include{Span: imp.Path.Span, Path: imp.Path.Str.Value, Optional: imp.Optional, Dynamic: dynamicPath(imp.Path.Str)}
	inc.Candidates = []string{inc.Path}
	in.ix.Includes = append(in.ix.Includes, inc)
}

func dynamicPath(s *StringLit) bool {
	return s.Prefix == 'f' || s.Prefix == 'x' && strings.ContainsRune(s.Value, '$')
}

var moduleFiles = []string{"mod.just", "justfile", "Justfile", "JUSTFILE", ".justfile"}

func (in *indexer) module(m *Module) {
	in.keyword(m.Span.Start, "mod")
	i := in.symbol(model.Symbol{
		Name: m.Name.Text, Kind: model.KindModule, Span: m.Span, Selection: m.Name.Span, Parent: model.NoParent,
		Detail: detail(in.text(m.Span)), Doc: m.Doc, Arity: model.NoArity,
	})
	in.attributes(m.Attributes, i)
	in.token(m.Name.Span, semantic.TypeNamespace, semantic.ModDeclaration)
	inc := model.Include{Span: m.Name.Span, Path: m.Name.Text, Optional: m.Optional, Module: m.Name.Text}
	if m.Path != nil {
		in.expr(m.Path, model.Global, i, false)
		inc.Span, inc.Path, inc.Dynamic = m.Path.Span, m.Path.Str.Value, dynamicPath(m.Path.Str)
		inc.Candidates = []string{inc.Path}
		for _, name := range moduleFiles {
			inc.Candidates = append(inc.Candidates, inc.Path+"/"+name)
		}
	} else {
		inc.Candidates = []string{m.Name.Text + ".just"}
		for _, name := range moduleFiles {
			inc.Candidates = append(inc.Candidates, m.Name.Text+"/"+name)
		}
	}
	in.ix.Includes = append(in.ix.Includes, inc)
}

func (in *indexer) function(fn *Function) {
	scope := in.scopes[fn.Span.Start]
	i := in.symbol(model.Symbol{
		Name: fn.Name.Text, Kind: model.KindFunction, Span: fn.Span, Selection: fn.Name.Span, Parent: model.NoParent,
		Detail: detail(in.text(Span{Start: fn.Name.Span.Start, End: fn.Body.Span.Start})), Arity: model.Arity{Min: len(fn.Params), Max: len(fn.Params)},
	})
	in.attributes(fn.Attributes, i)
	in.token(fn.Name.Span, semantic.TypeFunction, semantic.ModDeclaration)
	for _, p := range fn.Params {
		in.symbol(model.Symbol{Name: p.Text, Kind: model.KindParameter, Span: p.Span, Selection: p.Span, Scope: scope, Parent: i, Detail: p.Text, Unused: true, Arity: model.NoArity})
		in.token(p.Span, semantic.TypeParameter, semantic.ModDeclaration)
	}
	in.expr(fn.Body, scope, i, false)
}

func (r *Recipe) Arity() model.Arity {
	a := model.Arity{}
	for _, p := range r.Params {
		switch {
		case p.Variadic != Singular:
			a.Max = model.Variadic
			if p.Variadic == Plus && p.Default == nil {
				a.Min++
			}
		case p.Default == nil:
			a.Min++
			a.Max++
		default:
			a.Max++
		}
	}
	return a
}

func (r *Recipe) signatureEnd() int {
	if len(r.Params) == 0 {
		return r.Name.Span.End
	}
	return r.Params[len(r.Params)-1].Span.End
}

func (in *indexer) recipe(r *Recipe, positional bool) {
	scope := in.scopes[r.Span.Start]
	_, positionalAttr := attributeNamed(r.Attributes, "positional-arguments")
	i := in.symbol(model.Symbol{
		Name: r.Name.Text, Kind: model.KindRecipe, Span: r.Span, Selection: r.Name.Span, Parent: model.NoParent,
		Detail: detail(in.text(Span{Start: r.Name.Span.Start, End: r.signatureEnd()})), Doc: recipeDoc(r), Arity: r.Arity(),
	})
	in.attributes(r.Attributes, i)
	if r.Quiet {
		in.token(Span{Start: r.Header.Start, End: r.Header.Start + 1}, semantic.TypeOperator, 0)
	}
	in.token(r.Name.Span, semantic.TypeFunction, semantic.ModDeclaration)
	for _, p := range r.Params {
		exported := p.Export || in.ix.ExportAll
		in.symbol(model.Symbol{
			Name: p.Name.Text, Kind: model.KindParameter, Span: p.Span, Selection: p.Name.Span, Scope: scope, Parent: i,
			Detail: detail(in.text(p.Span)), Exported: exported, Unused: !positional && !positionalAttr && (!exported || in.shells[r]), Arity: model.NoArity,
		})
		in.token(p.Name.Span, semantic.TypeParameter, semantic.ModDeclaration)
		first := len(in.ix.Refs)
		in.expr(p.Default, scope, i, false)
		for j := first; j < len(in.ix.Refs); j++ {
			in.ix.Refs[j].Horizon = p.Span.Start
		}
	}
	for _, d := range slices.Concat(r.Deps, r.Subsequent) {
		args := 0
		if d.Paren {
			args = len(d.Args)
		}
		in.path(d.Target, i, args, true)
		for _, a := range d.Args {
			in.expr(a, scope, i, false)
		}
	}
	if len(r.Body) == 0 {
		return
	}
	body := Span{Start: r.Body[0].Span.Start, End: r.Span.End}
	in.ix.Shells = append(in.ix.Shells, model.Shell{Span: body, Scope: scope})
	in.ix.Folds = append(in.ix.Folds, model.Fold{Span: r.Span})
	if a := r.Arity(); a.Min == 0 {
		in.ix.Runnables = append(in.ix.Runnables, model.Runnable{Name: r.Name.Text, Span: r.Name.Span})
	}
	sigils := in.f.Sigils()
	for _, l := range r.Body {
		end := l.Sigils.Start
		for end < l.Sigils.End && strings.IndexByte(sigils, in.f.Text[end]) >= 0 {
			end++
		}
		if !r.Shebang {
			in.token(Span{Start: l.Sigils.Start, End: end}, semantic.TypeOperator, 0)
		}
		for _, frag := range l.Fragments {
			if frag.Kind != FragmentInterpolation {
				continue
			}
			in.delimiters(frag.Span, frag.Closed)
			in.expr(frag.Interp, scope, i, false)
		}
	}
}

func recipeDoc(r *Recipe) string {
	if attr, ok := attributeNamed(r.Attributes, "doc"); ok {
		return strings.TrimSpace(attr.first())
	}
	return r.Doc
}

func (in *indexer) expr(e *Expr, scope model.Scope, owner int, edge bool) {
	Walk(e, func(x *Expr) {
		switch x.Kind {
		case ExprVariable:
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: x.Name.Text, Kind: model.KindVariable, Span: x.Span, Scope: scope, Args: model.NoArgs, Owner: owner, Edge: edge})
			in.token(x.Span, semantic.TypeVariable, 0)
		case ExprCall:
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: x.Name.Text, Kind: model.KindFunction, Span: x.Name.Span, Scope: scope, Args: len(x.Args), Owner: owner})
			in.token(x.Name.Span, semantic.TypeFunction, 0)
		case ExprString, ExprBacktick:
			in.stringTokens(x)
		case ExprIf:
			in.keyword(x.Span.Start, "if")
			if x.Else.End > x.Else.Start {
				in.token(x.Else, semantic.TypeKeyword, 0)
			}
		case ExprBinary, ExprNot, ExprRoot, ExprList, ExprGroup, ExprBad:
		}
	})
}

func (in *indexer) stringTokens(x *Expr) {
	if len(x.Str.Segments) == 0 || x.Kind == ExprBacktick {
		in.token(x.Span, semantic.TypeString, 0)
		return
	}
	open := Span{Start: x.Span.Start, End: x.Str.Inner.Start}
	in.token(open, semantic.TypeString, 0)
	for _, seg := range x.Str.Segments {
		if seg.Kind == FragmentInterpolation {
			in.delimiters(seg.Span, true)
			continue
		}
		in.token(seg.Span, semantic.TypeString, 0)
	}
	if !x.Str.Unclosed {
		in.token(Span{Start: x.Str.Inner.End, End: x.Span.End}, semantic.TypeString, 0)
	}
}

func (in *indexer) delimiters(span Span, closed bool) {
	in.token(Span{Start: span.Start, End: span.Start + 2}, semantic.TypeOperator, 0)
	if closed {
		in.token(Span{Start: span.End - 2, End: span.End}, semantic.TypeOperator, 0)
	}
}

func (in *indexer) commentFolds() {
	comments := in.f.Comments
	for i := 0; i < len(comments); {
		j := i + 1
		for j < len(comments) && strings.Count(in.f.Text[comments[j-1].Span.End:comments[j].Span.Start], "\n") == 1 && strings.TrimSpace(in.f.Text[comments[j-1].Span.End:comments[j].Span.Start]) == "" {
			j++
		}
		if j-i > 1 {
			in.ix.Folds = append(in.ix.Folds, model.Fold{Span: Span{Start: comments[i].Span.Start, End: comments[j-1].Span.End}, Comment: true})
		}
		i = j
	}
}
