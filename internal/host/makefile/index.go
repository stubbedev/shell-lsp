package makefile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

const maxDetail = 80

var Checks = model.Checks{
	Unknown: map[model.Namespace]lint.Code{
		model.NamespaceValues:    lint.CodeMakeUndefinedVariable,
		model.NamespaceFunctions: lint.CodeMakeUnknownFunction,
		model.NamespaceTargets:   lint.CodeMakePhonyWithoutRule,
	},
	Always: map[model.Namespace]bool{model.NamespaceFunctions: true},
	Arity:  map[model.Kind]lint.Code{model.KindFunction: lint.CodeMakeFunctionArguments},
	Circular: map[model.Kind]lint.Code{
		model.KindVariable: lint.CodeMakeRecursiveVariable,
	},
	Unused:  map[model.Kind]lint.Code{model.KindVariable: lint.CodeMakeUnusedVariable},
	Missing: lint.CodeMakeMissingInclude,
}

type Analysis struct {
	File      *File
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
}

func Analyze(text string) Analysis {
	f := Parse(text)
	m, detection, shell := project(f)
	return Analysis{File: f, Map: m, Detection: detection, Index: f.Index(shell)}
}

type indexer struct {
	f        *File
	ix       *model.Index
	scopes   map[string]model.Scope
	exported map[string]bool
	dynamic  bool
}

func (f *File) exported() []string {
	var names []string
	marked := f.exportedNames()
	for _, a := range f.Assignments {
		if (f.ExportAll || marked[a.Name.Text]) && shellName(a.Name.Text) {
			names = append(names, a.Name.Text)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func (f *File) exportedNames() map[string]bool {
	out := map[string]bool{}
	for _, a := range f.Assignments {
		if slices.ContainsFunc(a.Modifiers, func(w Word) bool { return w.Text == "export" }) {
			out[a.Name.Text] = true
		}
	}
	for _, d := range f.Directives {
		for _, n := range d.Names {
			switch d.Keyword.Text {
			case "export":
				out[n.Text] = true
			case "unexport":
				delete(out, n.Text)
			}
		}
	}
	return out
}

func shellName(name string) bool {
	if name == "" || name[0] != '_' && (name[0] < 'a' || name[0] > 'z') && (name[0] < 'A' || name[0] > 'Z') {
		return false
	}
	return strings.Trim(name, "_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") == ""
}

func detail(s string) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\\\n", " ")), " ")
	if len(s) > maxDetail {
		return s[:maxDetail-1] + "…"
	}
	return s
}

func (f *File) Index(shell bool) *model.Index {
	ix := &model.Index{ExportAll: f.ExportAll, Broken: slices.ContainsFunc(f.Errors, func(e Error) bool { return e.Kind != ErrUnterminated })}
	in := &indexer{f: f, ix: ix, scopes: map[string]model.Scope{}, exported: f.exportedNames()}
	for _, a := range f.Assignments {
		in.assignment(a)
	}
	for _, r := range f.Rules {
		in.rule(r, shell)
	}
	for _, inc := range f.Includes {
		in.include(inc)
	}
	for _, d := range f.Directives {
		in.directive(d)
	}
	for _, c := range f.Conditionals {
		in.conditional(c)
	}
	for _, e := range f.Expressions {
		in.walk(e, model.Global, model.NoSymbol, false, nil)
	}
	for _, c := range f.Comments {
		in.token(c, semantic.TypeComment, 0)
	}
	in.commentFolds()
	if in.dynamic {
		for i := range ix.Symbols {
			ix.Symbols[i].Unused = false
		}
	}
	ix.Findings = f.check()
	slices.SortFunc(ix.Tokens, func(a, b semantic.Token) int { return a.Span.Start - b.Span.Start })
	return ix
}

func (in *indexer) text(s Span) string { return in.f.Text[s.Start:s.End] }

func (in *indexer) token(span Span, t semantic.Type, mods semantic.Modifier) {
	if span.End > span.Start {
		in.ix.Tokens = append(in.ix.Tokens, semantic.Token{Span: span, Type: t, Modifiers: mods})
	}
}

func (in *indexer) scope(target string) model.Scope {
	if s, ok := in.scopes[target]; ok {
		return s
	}
	s := model.Scope(len(in.scopes) + 1)
	in.scopes[target] = s
	return s
}

func (in *indexer) symbol(s model.Symbol) int {
	in.ix.Symbols = append(in.ix.Symbols, s)
	return len(in.ix.Symbols) - 1
}

var recursiveOps = []string{"=", "?=", ""}

func readByMake(name string) bool {
	_, builtin := Builtins.Lookup(model.KindVariable, name)
	return builtin || strings.HasSuffix(name, "FLAGS") || strings.HasPrefix(name, ".")
}

func (in *indexer) assignment(a *Assignment) {
	for _, m := range a.Modifiers {
		in.token(m.Span, semantic.TypeKeyword, 0)
	}
	if a.Define {
		in.token(a.Keyword, semantic.TypeKeyword, 0)
		in.token(a.End, semantic.TypeKeyword, 0)
		in.ix.Folds = append(in.ix.Folds, model.Fold{Span: a.Span})
	}
	exported := in.exported[a.Name.Text] || in.f.ExportAll
	header := in.text(Span{Start: a.Name.Span.Start, End: a.Value.Span.End})
	if a.Define {
		header = "define " + a.Name.Text + " " + a.Op
	}
	scopes := []model.Scope{model.Global}
	if len(a.Targets) > 0 {
		scopes = scopes[:0]
		for _, t := range a.Targets {
			scopes = append(scopes, in.scope(t.Text))
			in.walk(t.Expr, model.Global, model.NoSymbol, false, nil)
			if literal(t) {
				in.ix.Refs = append(in.ix.Refs, model.Ref{Name: t.Text, Kind: model.KindTarget, Span: t.Span, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true})
				in.token(t.Span, semantic.TypeFunction, 0)
			}
		}
	}
	owner := model.NoSymbol
	for _, scope := range scopes {
		i := in.symbol(model.Symbol{
			Name: a.Name.Text, Kind: model.KindVariable, Span: a.Span, Selection: a.Name.Span, Scope: scope, Parent: model.NoParent,
			Detail: detail(header), Exported: exported, Unused: !exported && !readByMake(a.Name.Text) && scope == model.Global, Arity: model.NoArity,
		})
		if owner == model.NoSymbol {
			owner = i
		}
	}
	in.token(a.Name.Span, semantic.TypeVariable, semantic.ModDeclaration)
	edge := slices.Contains(recursiveOps, a.Op) && len(a.Targets) == 0
	in.walk(a.Name.Expr, model.Global, owner, false, nil)
	in.walk(a.Value, scopes[0], owner, edge, nil)
}

var namingFunctions = []string{"call", "value", "origin", "flavor"}

func (in *indexer) walk(e Expr, scope model.Scope, owner int, edge bool, bound []string) {
	for _, n := range e.Nodes {
		switch n.Kind {
		case NodeText:
		case NodeCall:
			in.call(n, scope, owner, edge, bound)
		case NodeReference, NodeSubstitution:
			in.reference(n, scope, owner, edge, bound)
			for _, a := range n.Args {
				in.walk(a, scope, owner, edge, bound)
			}
		}
	}
}

func (in *indexer) nameSpan(e Expr, name string) Span {
	at := e.Span.Start + strings.Index(in.text(e.Span), name)
	return Span{Start: at, End: at + len(name)}
}

func (in *indexer) reference(n Node, scope model.Scope, owner int, edge bool, bound []string) {
	raw, literal := n.Name.Literal(in.f.Text)
	name := strings.TrimSpace(raw)
	switch {
	case !literal:
		in.dynamic = true
		in.walk(n.Name, scope, owner, edge, bound)
	case name == "" || slices.Contains(bound, name) || strings.Trim(name, "0123456789") == "":
	case strings.ContainsAny(name, " \t"):
		word := firstWord(name)
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: word, Kind: model.KindFunction, Span: in.nameSpan(n.Name, word), Scope: scope, Args: model.NoArgs, Owner: owner})
	default:
		span := in.nameSpan(n.Name, name)
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: name, Kind: model.KindVariable, Span: span, Scope: scope, Args: model.NoArgs, Owner: owner, Edge: edge})
		mods := semantic.Modifier(0)
		if _, builtin := Builtins.Lookup(model.KindVariable, name); builtin {
			mods = semantic.ModDefaultLibrary
		}
		in.token(span, semantic.TypeVariable, mods)
	}
}

func (in *indexer) call(n Node, scope model.Scope, owner int, edge bool, bound []string) {
	name, _ := n.Name.Literal(in.f.Text)
	in.ix.Refs = append(in.ix.Refs, model.Ref{Name: name, Kind: model.KindFunction, Span: n.Name.Span, Scope: scope, Args: len(n.Args), Owner: owner})
	in.token(n.Name.Span, semantic.TypeFunction, semantic.ModDefaultLibrary)
	args := n.Args
	switch {
	case slices.Contains(namingFunctions, name):
		if variable, ok := args[0].Literal(in.f.Text); ok && strings.TrimSpace(variable) != "" {
			variable = strings.TrimSpace(variable)
			span := in.nameSpan(args[0], variable)
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: variable, Kind: model.KindVariable, Span: span, Scope: scope, Args: model.NoArgs, Owner: owner, Edge: edge})
			in.token(span, semantic.TypeVariable, 0)
			args = args[1:]
		}
	case (name == "foreach" || name == "let") && len(args) >= 3:
		names, _ := args[0].Literal(in.f.Text)
		for _, a := range args[0:1] {
			in.walk(a, scope, owner, edge, bound)
		}
		in.walk(args[1], scope, owner, edge, bound)
		inner := slices.Concat(bound, strings.Fields(names))
		for _, a := range args[2:] {
			in.walk(a, scope, owner, edge, inner)
		}
		return
	}
	for _, a := range args {
		in.walk(a, scope, owner, edge, bound)
	}
}

func literal(w Word) bool { return !w.Expr.Dynamic() && !strings.Contains(w.Text, "%") }

func (in *indexer) rule(r *Rule, shell bool) {
	scope := model.Global
	owner := model.NoSymbol
	phony := false
	for _, t := range r.Targets {
		in.walk(t.Expr, model.Global, model.NoSymbol, false, nil)
		if !literal(t) {
			continue
		}
		if _, special := specialTargets[t.Text]; special {
			phony = phony || t.Text == ".PHONY"
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: t.Text, Kind: model.KindTarget, Span: t.Span, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true})
			in.token(t.Span, semantic.TypeKeyword, 0)
			continue
		}
		i := in.symbol(model.Symbol{Name: t.Text, Kind: model.KindTarget, Span: r.Span, Selection: t.Span, Parent: model.NoParent, Detail: detail(in.text(r.Header)), Arity: model.NoArity})
		in.token(t.Span, semantic.TypeFunction, semantic.ModDeclaration)
		if owner == model.NoSymbol {
			owner, scope = i, in.scope(t.Text)
		}
		in.ix.Runnables = append(in.ix.Runnables, model.Runnable{Name: t.Text, Span: t.Span})
	}
	for _, w := range r.Patterns {
		in.walk(w.Expr, scope, owner, false, nil)
	}
	for _, w := range slices.Concat(r.Prereqs, r.OrderOnly) {
		in.walk(w.Expr, scope, owner, false, nil)
		if literal(w) {
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: w.Text, Kind: model.KindTarget, Span: w.Span, Args: model.NoArgs, Owner: owner, Soft: !phony})
			in.token(w.Span, semantic.TypeFunction, 0)
		}
	}
	if len(r.Recipe) == 0 {
		return
	}
	body := Span{Start: r.Recipe[0].Span.Start, End: r.Recipe[len(r.Recipe)-1].Span.End}
	if shell {
		in.ix.Shells = append(in.ix.Shells, model.Shell{Span: body, Scope: scope})
	}
	if strings.Contains(in.text(r.Span), "\n") {
		in.ix.Folds = append(in.ix.Folds, model.Fold{Span: r.Span})
	}
	for _, l := range r.Recipe {
		text := in.text(l.Span)
		flags := len(text) - len(strings.TrimLeft(text, recipeFlags))
		in.token(Span{Start: l.Span.Start, End: l.Span.Start + len(strings.TrimRight(text[:flags], " \t"))}, semantic.TypeOperator, 0)
		in.walk(l.Expr, scope, owner, false, nil)
	}
}

func (in *indexer) include(inc *Include) {
	in.token(inc.Keyword.Span, semantic.TypeKeyword, 0)
	for _, w := range inc.Files {
		in.walk(w.Expr, model.Global, model.NoSymbol, false, nil)
		path, dynamic := in.expand(w.Expr)
		in.ix.Includes = append(in.ix.Includes, model.Include{Span: w.Span, Path: path, Candidates: []string{path}, Optional: inc.Kind == IncludeOptional, Dynamic: dynamic})
	}
}

func (in *indexer) expand(e Expr) (string, bool) {
	var sb strings.Builder
	for _, n := range e.Nodes {
		if n.Kind == NodeText {
			sb.WriteString(in.text(n.Span))
			continue
		}
		name, literal := n.Name.Literal(in.f.Text)
		if n.Kind != NodeReference || !literal {
			return in.text(e.Span), true
		}
		value, ok := in.literalValue(strings.TrimSpace(name))
		if !ok {
			return in.text(e.Span), true
		}
		sb.WriteString(value)
	}
	return sb.String(), false
}

func (in *indexer) literalValue(name string) (string, bool) {
	var found []string
	for _, a := range in.f.Assignments {
		if a.Name.Text == name && len(a.Targets) == 0 && !a.Define {
			found = append(found, strings.TrimSpace(in.text(a.Value.Span)))
			if a.Op == "+=" || a.Op == "!=" {
				return "", false
			}
		}
	}
	if len(found) != 1 || strings.ContainsAny(found[0], "$ \t") {
		return "", false
	}
	return found[0], true
}

func (in *indexer) directive(d *Directive) {
	in.token(d.Keyword.Span, semantic.TypeKeyword, 0)
	if len(d.Names) == 0 {
		in.walk(d.Args, model.Global, model.NoSymbol, false, nil)
		return
	}
	for _, n := range d.Names {
		if literal(n) {
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: n.Text, Kind: model.KindVariable, Span: n.Span, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true})
			in.token(n.Span, semantic.TypeVariable, 0)
			continue
		}
		in.walk(n.Expr, model.Global, model.NoSymbol, false, nil)
	}
}

func (in *indexer) conditional(c *Conditional) {
	for _, d := range slices.Concat([]Directive{c.Open}, c.Else) {
		in.token(d.Keyword.Span, semantic.TypeKeyword, 0)
		if len(d.Names) == 1 && literal(d.Names[0]) {
			n := d.Names[0]
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: n.Text, Kind: model.KindVariable, Span: n.Span, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true})
			in.token(n.Span, semantic.TypeVariable, 0)
			continue
		}
		for _, n := range d.Names {
			in.walk(n.Expr, model.Global, model.NoSymbol, false, nil)
		}
		in.walk(d.Args, model.Global, model.NoSymbol, false, nil)
	}
	if c.End != nil {
		in.token(c.End.Keyword.Span, semantic.TypeKeyword, 0)
	}
	if strings.Contains(in.text(c.Span), "\n") {
		in.ix.Folds = append(in.ix.Folds, model.Fold{Span: c.Span})
	}
}

func (in *indexer) commentFolds() {
	comments := in.f.Comments
	for i := 0; i < len(comments); {
		j := i + 1
		for j < len(comments) && strings.TrimSpace(in.f.Text[comments[j-1].End:comments[j].Start]) == "" && strings.Count(in.f.Text[comments[j-1].End:comments[j].Start], "\n") == 1 {
			j++
		}
		if j-i > 1 {
			in.ix.Folds = append(in.ix.Folds, model.Fold{Span: Span{Start: comments[i].Start, End: comments[j-1].End}, Comment: true})
		}
		i = j
	}
}
