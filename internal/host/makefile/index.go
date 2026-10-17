package makefile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

const maxDetail = 80

var Checks = model.Checks{Of: rules, Missing: lint.CodeMakeMissingInclude, NotPhony: lint.CodeMakeNotPhony}

func rules(kind model.Kind) model.Rules {
	switch kind {
	case model.KindVariable:
		return model.Rules{Unknown: lint.CodeMakeUndefinedVariable, Circular: lint.CodeMakeRecursiveVariable, Expanded: model.OnUse, Unused: lint.CodeMakeUnusedVariable}
	case model.KindFunction:
		return model.Rules{Unknown: lint.CodeMakeUnknownFunction, UnknownWhen: model.Regardless, Arity: lint.CodeMakeFunctionArguments}
	case model.KindTarget:
		return model.Rules{Unknown: lint.CodeMakePhonyWithoutRule}
	case model.KindRecipe, model.KindAlias, model.KindParameter, model.KindModule, model.KindSetting, model.KindAttribute, model.KindKeyword, model.KindStage:
		return model.Rules{}
	}
	panic(fmt.Sprintf("makefile: kind %s has no rules", kind))
}

type Analysis struct {
	File      *File
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
}

func Analyze(text string) Analysis {
	f := Parse(text)
	m, detection, shells := project(f)
	return Analysis{File: f, Map: m, Detection: detection, Index: f.Index(shells)}
}

type indexer struct {
	f            *File
	ix           *model.Index
	scopes       map[string]model.Scope
	exported     map[string]bool
	dynamic      bool
	arguments    []int
	called       map[string]bool
	redefined    []string
	overridden   map[*Rule]bool
	usedPatterns map[*Rule]bool
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

func (f *File) Index(shells map[*Rule]bool) *model.Index {
	ix := &model.Index{ExportAll: f.ExportAll, Broken: slices.ContainsFunc(f.Errors, func(e Error) bool {
		return e.Kind != ErrUnterminated && e.Kind != ErrDeferredUnterminated && e.Kind != ErrPatternUnterminated && e.Kind != ErrPatternMismatch
	})}
	ix.Branches = f.alternatives()
	ix.Inherits = map[model.Scope][]model.Scope{}
	in := &indexer{overridden: f.overridden(), usedPatterns: f.usedPatterns(), f: f, ix: ix, scopes: map[string]model.Scope{}, exported: f.exportedNames(), called: map[string]bool{}}
	for _, a := range f.Assignments {
		in.assignment(a)
	}
	for _, r := range f.Rules {
		in.rule(r, shells[r])
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
	in.dropCallArguments()
	ix.Findings = f.check()
	in.deferred()
	slices.SortFunc(ix.Tokens, func(a, b semantic.Token) int { return a.Span.Start - b.Span.Start })
	return ix
}

func (in *indexer) dropCallArguments() {
	drop := map[int]bool{}
	for _, i := range in.arguments {
		if r := in.ix.Refs[i]; r.Owner >= 0 && in.called[in.ix.Symbols[r.Owner].Name] {
			drop[i] = true
		}
	}
	refs := in.ix.Refs[:0]
	for i, r := range in.ix.Refs {
		if !drop[i] {
			refs = append(refs, r)
		}
	}
	in.ix.Refs = refs
}

func (in *indexer) deferred() {
	for _, e := range in.f.Errors {
		if e.Kind != ErrDeferredUnterminated {
			continue
		}
		owner := slices.IndexFunc(in.ix.Symbols, func(s model.Symbol) bool {
			return s.Kind == model.KindVariable && s.Span.Start <= e.Span.Start && e.Span.End <= s.Span.End
		})
		if owner < 0 {
			panic(fmt.Sprintf("makefile: no assignment holds the deferred reference at %d", e.Span.Start))
		}
		until := 0
		if i := slices.IndexFunc(in.f.Assignments, func(a *Assignment) bool { return a.Span.Start <= e.Span.Start && e.Span.End <= a.Span.End }); i >= 0 {
			until = in.f.Assignments[i].JoinedAt
		}
		in.ix.Deferred = append(in.ix.Deferred, model.Deferred{Symbol: owner, Span: e.Span, Message: e.Message, Used: lint.CodeMakeUnterminatedReference, Unused: lint.CodeMakeDeferredUnterminatedReference, Until: until})
	}
}

var standardTargets = []string{"check", "clean", "dist", "distclean", "install", "install-strip", "installcheck", "installdirs", "maintainer-clean", "mostlyclean", "uninstall"}

func (in *indexer) namesTarget(r *Rule) bool {
	return slices.ContainsFunc(r.Recipe, func(l RecipeLine) bool {
		found := false
		Walk(l.Expr, func(n Node) bool {
			name, ok := n.Name.Literal(in.f.Text)
			found = found || n.Kind == NodeReference && ok && (name == "@" || name == "@F")
			return !found
		})
		return found
	})
}

func (in *indexer) unescaped(arg Expr) (*File, []int, bool) {
	text, ok := arg.Literal(in.f.Text)
	if !ok {
		return nil, nil, false
	}
	var sb strings.Builder
	at := make([]int, 0, len(text)+1)
	for i := 0; i < len(text); i++ {
		at = append(at, arg.Span.Start+i)
		sb.WriteByte(text[i])
		if text[i] == '$' && i+1 < len(text) && text[i+1] == '$' {
			i++
		}
	}
	at = append(at, arg.Span.Start+len(text))
	sub := Parse(sb.String())
	return sub, at, len(sub.Errors) == 0
}

func (in *indexer) redefines(arg Expr) []string {
	sub, _, ok := in.unescaped(arg)
	if !ok {
		in.ix.Opaque = true
		return nil
	}
	names := make([]string, 0, len(sub.Assignments))
	for _, a := range sub.Assignments {
		if len(a.Targets) == 0 && literal(a.Name) {
			names = append(names, a.Name.Text)
		}
	}
	return names
}

func (in *indexer) evaluated(arg Expr) {
	sub, at, ok := in.unescaped(arg)
	if !ok || len(sub.Includes) > 0 {
		in.ix.Opaque = true
		return
	}
	shift := func(s Span) Span { return Span{Start: at[s.Start], End: at[s.End]} }
	ix := sub.Index(nil)
	if ix.Opaque || slices.ContainsFunc(ix.Symbols, func(s model.Symbol) bool { return s.Scope != model.Global }) || slices.ContainsFunc(ix.Refs, func(r model.Ref) bool { return r.Scope != model.Global }) {
		in.ix.Opaque = true
		return
	}
	base := len(in.ix.Symbols)
	for _, sym := range ix.Symbols {
		sym.Span, sym.Selection, sym.Parent = shift(sym.Span), shift(sym.Selection), model.NoParent
		in.ix.Symbols = append(in.ix.Symbols, sym)
	}
	for _, r := range ix.Refs {
		r.Span = shift(r.Span)
		if r.Owner >= 0 {
			r.Owner += base
		}
		for i, a := range r.ArgSpans {
			r.ArgSpans[i] = shift(a)
		}
		in.ix.Refs = append(in.ix.Refs, r)
	}
}

func searchDirs(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ':' || r == ' ' || r == '\t' })
}

func (in *indexer) ruleTargets() []string {
	var out []string
	for _, r := range in.f.Rules {
		for _, t := range r.Targets {
			if literal(t) {
				out = append(out, t.Text)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
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

func readByMake(name string) bool {
	_, builtin := Builtins.Lookup(model.KindVariable, name)
	return builtin && !automatic(name) || strings.HasSuffix(name, "FLAGS") || strings.HasPrefix(name, ".")
}

func automatic(name string) bool {
	return slices.ContainsFunc(automaticVariables, func(v model.Builtin) bool {
		return name == v.Name || name == v.Name+"D" || name == v.Name+"F"
	})
}

func (in *indexer) assignment(a *Assignment) {
	if a.Name.Expr.Dynamic() {
		in.ix.Opaque = true
	}
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
			if !t.Expr.Dynamic() && hasPercent(t.Text) {
				for _, name := range in.ruleTargets() {
					if name != t.Text && model.MatchesRulePattern(t.Text, name) {
						scopes = append(scopes, in.scope(name))
					}
				}
			}
			in.walk(t.Expr, model.Global, model.NoSymbol, false, nil)
			if literal(t) {
				in.ix.Refs = append(in.ix.Refs, model.Ref{Name: t.Text, Kind: model.KindTarget, Span: t.Span, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true, Declares: true})
				in.token(t.Span, semantic.TypeFunction, 0)
			}
		}
	}
	owner := model.NoSymbol
	firstSymbol := len(in.ix.Symbols)
	for _, scope := range scopes {
		i := in.symbol(model.Symbol{
			Name: a.Name.Text, Kind: model.KindVariable, Span: a.Span, Selection: a.Name.Span, Scope: scope, Parent: model.NoParent,
			Detail: detail(header), Exported: exported, Unused: !exported && !readByMake(a.Name.Text) && scope == model.Global, Appends: a.Op == "+=", Arity: model.NoArity,
		})
		if owner == model.NoSymbol {
			owner = i
		}
	}
	if value, ok := a.Value.Literal(in.f.Text); ok && owner != model.NoSymbol && !a.Define && a.Op != "+=" && a.Op != "!=" {
		in.ix.Symbols[owner].Value, in.ix.Symbols[owner].Constant = strings.TrimSpace(value), true
	}
	if value, ok := a.Value.Literal(in.f.Text); ok && a.Name.Text == "VPATH" && len(a.Targets) == 0 {
		in.ix.Search = append(in.ix.Search, model.SearchPath{Dirs: searchDirs(value)})
	}
	in.token(a.Name.Span, semantic.TypeVariable, semantic.ModDeclaration)
	edge := a.Deferred
	in.walk(a.Name.Expr, model.Global, owner, false, nil)
	if a.Op == "+=" && len(a.Targets) > 0 && owner != model.NoSymbol {
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: a.Name.Text, Kind: model.KindVariable, Span: a.Name.Span, Scope: model.Global, Args: model.NoArgs, Owner: owner, Edge: true, Soft: true, Base: true})
	}
	first := len(in.ix.Refs)
	in.walk(a.Value, scopes[0], owner, edge, nil)
	if a.Swallowed {
		for i := first; i < len(in.ix.Refs); i++ {
			in.ix.Refs[i].Inert = true
		}
	}
	if !a.Deferred && len(a.Targets) > 0 {
		for i := first; i < len(in.ix.Refs); i++ {
			in.ix.Refs[i].Horizon = a.Span.Start
		}
	}
	if a.Op == "?=" && (slices.Contains(predefined, a.Name.Text) || in.definedBefore(a)) {
		for i := first; i < len(in.ix.Refs); i++ {
			in.ix.Refs[i].Inert = true
		}
		for i := firstSymbol; i < len(in.ix.Symbols); i++ {
			in.ix.Symbols[i].Inert = true
		}
	}
	in.redefined = nil
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
	case n.Unbalanced:
		in.dynamic = true
	case !literal:
		in.dynamic = true
		in.walk(n.Name, scope, owner, edge, bound)
	case name == "" || slices.Contains(bound, name):
	case name != raw && !strings.ContainsAny(name, " \t"):
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: raw, Kind: model.KindVariable, Span: in.nameSpan(n.Name, name), Scope: scope, Args: model.NoArgs, Owner: owner})
	case strings.ContainsAny(name, " \t"):
		word := firstWord(name)
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: word, Kind: model.KindFunction, Span: in.nameSpan(n.Name, word), Scope: scope, Args: model.NoArgs, Owner: owner})
	default:
		span := in.nameSpan(n.Name, name)
		edge := edge && !slices.Contains(in.redefined, name)
		if edge && strings.Trim(name, "0123456789") == "" {
			in.arguments = append(in.arguments, len(in.ix.Refs))
		}
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
	call := model.Ref{Name: name, Kind: model.KindFunction, Span: n.Name.Span, Scope: scope, Args: len(n.Args), Owner: owner, Edge: edge}
	for _, a := range n.Args {
		call.ArgSpans = append(call.ArgSpans, a.Span)
	}
	in.ix.Refs = append(in.ix.Refs, call)
	in.token(n.Name.Span, semantic.TypeFunction, semantic.ModDefaultLibrary)
	if name == "eval" && len(n.Args) == 1 {
		if edge {
			defer func() { in.redefined = append(in.redefined, in.redefines(n.Args[0])...) }()
		} else {
			in.evaluated(n.Args[0])
		}
	}
	args := n.Args
	switch {
	case slices.Contains(namingFunctions, name):
		if variable, ok := args[0].Literal(in.f.Text); ok && strings.TrimSpace(variable) != "" {
			variable = strings.TrimSpace(variable)
			if name == "call" {
				in.called[variable] = true
			}
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

func literal(w Word) bool { return !w.Expr.Dynamic() && !hasPercent(w.Text) }

func (in *indexer) rule(r *Rule, shell bool) {
	scope := model.Global
	owner := model.NoSymbol
	phony, declaring := false, false
	for _, t := range r.Targets {
		in.walk(t.Expr, model.Global, model.NoSymbol, false, nil)
		if !literal(t) {
			continue
		}
		if _, special := specialTargets[t.Text]; special {
			phony, declaring = phony || t.Text == ".PHONY", true
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
		in.walk(w.Expr, model.Global, owner, false, nil)
	}
	for _, w := range slices.Concat(r.Prereqs, r.OrderOnly) {
		if scope != model.Global && literal(w) {
			child := in.scope(w.Text)
			in.ix.Inherits[child] = append(in.ix.Inherits[child], scope)
		}
		in.walk(w.Expr, model.Global, owner, false, nil)
		if literal(w) && phony {
			in.ix.Phony = append(in.ix.Phony, w.Text)
		}
		if literal(w) {
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: w.Text, Kind: model.KindTarget, Span: w.Span, Args: model.NoArgs, Owner: owner, Soft: !phony, Declares: declaring})
			in.token(w.Span, semantic.TypeFunction, 0)
		}
	}
	if len(r.Recipe) == 0 {
		return
	}
	if owner != model.NoSymbol && slices.Contains(standardTargets, in.ix.Symbols[owner].Name) && !in.namesTarget(r) {
		in.ix.Fileless = append(in.ix.Fileless, owner)
	}
	in.ix.Recipes = true
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
		first := len(in.ix.Refs)
		in.walk(l.Expr, scope, owner, false, nil)
		for j := first; j < len(in.ix.Refs); j++ {
			in.ix.Refs[j].Late = true
			in.ix.Refs[j].Edge = r.Pattern() && !in.usedPatterns[r]
			in.ix.Refs[j].Inert = in.overridden[r] || r.NoTargets
		}
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
	if args, ok := d.Args.Literal(in.f.Text); ok && d.Keyword.Text == "vpath" {
		if fields := strings.Fields(args); len(fields) > 1 {
			in.ix.Search = append(in.ix.Search, model.SearchPath{Pattern: fields[0], Dirs: searchDirs(strings.Join(fields[1:], " "))})
		}
	}
	if len(d.Names) == 0 {
		in.walk(d.Args, model.Global, model.NoSymbol, false, nil)
		return
	}
	for _, n := range d.Names {
		if literal(n) {
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: n.Text, Kind: model.KindVariable, Span: n.Span, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true, Declares: true})
			in.token(n.Span, semantic.TypeVariable, 0)
			continue
		}
		in.walk(n.Expr, model.Global, model.NoSymbol, false, nil)
	}
}

func (in *indexer) definedBefore(a *Assignment) bool {
	return slices.ContainsFunc(in.f.Assignments, func(prior *Assignment) bool {
		return prior.Span.Start < a.Span.Start && prior.Name.Text == a.Name.Text && (len(prior.Targets) == 0 || len(a.Targets) > 0)
	}) || slices.ContainsFunc(in.f.Directives, func(d *Directive) bool {
		return d.Span.Start < a.Span.Start && slices.ContainsFunc(d.Names, func(w Word) bool { return w.Text == a.Name.Text })
	})
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
