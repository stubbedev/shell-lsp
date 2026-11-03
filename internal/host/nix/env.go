package nix

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

type envSource uint8

const (
	derivationEnv envSource = iota
	unitEnv
	appEnv
	wrapperEnv
)

const envPathSeparator = "\x00"

var envDocs = [...]string{
	derivationEnv: "Set in the environment by the derivation.",
	unitEnv:       "Set in the environment by the unit's `environment`.",
	appEnv:        "Set in the environment by `runtimeEnv`.",
	wrapperEnv:    "Set in the environment by the function that runs this script.",
}

var (
	unexported  = []string{"meta", "passthru", "env"}
	envChildren = [...]string{derivationEnv: "env", unitEnv: "environment", appEnv: "runtimeEnv", wrapperEnv: ""}
)

const (
	getExeName   = "getExe"
	toStringName = "toString"
)

var (
	programGetters = []string{getExeName, "getExe'", "getBin"}
	inputLists     = []string{"runtimeInputs", "buildInputs", "nativeBuildInputs", "packages", "path"}
)

var Checks = model.Checks{Of: func(model.Kind) model.Rules { return model.Rules{} }}

type environment struct {
	set    *syntax.AttrSet
	source envSource
	child  string
	prefix string
	expr   syntax.Expr
	own    *syntax.String
}

func (env environment) attrPath() []string {
	var out []string
	if env.prefix != "" {
		out = strings.Split(env.prefix, envPathSeparator)
	}
	if child := cmp.Or(env.child, envChildren[env.source]); child != "" {
		out = append(out, child)
	}
	return out
}

type binding struct {
	path    []string
	name    model.Span
	span    model.Span
	value   *syntax.AttrSet
	inherit bool
}

func bindingsOf(set *syntax.AttrSet) []binding {
	var out []binding
	for _, b := range set.Bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			if bound, ok := assigned(b); ok {
				out = append(out, bound)
			}
		case *syntax.Inherit:
			for _, a := range b.Attrs {
				if id, ok := a.(*syntax.IdentAttr); ok {
					out = append(out, binding{path: []string{id.Name}, name: id.Span(), span: b.Span(), inherit: true})
				}
			}
		}
	}
	return out
}

func assigned(a *syntax.Assign) (binding, bool) {
	path := make([]string, len(a.Path))
	var name model.Span
	for i, attr := range a.Path {
		text, span, ok := syntax.StaticName(attr)
		if !ok {
			return binding{}, false
		}
		path[i], name = text, span
	}
	value, _ := a.Value.(*syntax.AttrSet)
	return binding{path: path, name: name, span: a.Span(), value: value}, true
}

func children(set *syntax.AttrSet) []binding {
	if set == nil {
		return nil
	}
	return slices.DeleteFunc(bindingsOf(set), func(b binding) bool { return len(b.path) > 1 })
}

func nested(bindings []binding, path []string) []binding {
	var out []binding
	for _, b := range bindings {
		switch {
		case len(b.path) < len(path) || !slices.Equal(b.path[:len(path)], path):
		case len(b.path) == len(path):
			out = append(out, children(b.value)...)
		case len(b.path) == len(path)+1:
			out = append(out, b)
		}
	}
	return out
}

func vars(env environment) []binding {
	bindings := bindingsOf(env.set)
	out := nested(bindings, env.attrPath())
	if env.source != derivationEnv {
		return out
	}
	for _, b := range bindings {
		if len(b.path) == 1 && !strings.HasPrefix(b.path[0], "__") && !slices.Contains(unexported, b.path[0]) {
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, func(a, b binding) int { return cmp.Compare(a.name.Start, b.name.Start) })
	return out
}

func (f *file) described(b binding) string {
	detail, elided := f.detail(b)
	return model.Symbol{Detail: detail, Elided: elided}.Description()
}

func (f *file) detail(b binding) (string, model.Elision) {
	text := f.src[b.span.Start:b.span.End]
	if !strings.Contains(text, "\n") {
		return text, model.NotElided
	}
	if b.inherit {
		return f.src[b.name.Start:b.name.End], model.ElidedInherit
	}
	return f.src[b.span.Start:b.name.End], model.ElidedValue
}

func (f *file) index(m *projection.Map) *model.Index {
	found := f.found
	ix := &model.Index{}
	for _, e := range f.tree.Errors {
		ix.Findings = append(ix.Findings, lint.HostFinding(lint.CodeNixSyntax, e.Span, e.Message))
	}
	ix.Findings = append(ix.Findings, check(f.tree, f.table, nil)...)
	table := newSymbolTable(f, ix)
	for _, s := range found {
		if s.hosted != nil {
			table.reserveHosted(s)
			continue
		}
		env := s.env
		if env.set == nil && env.expr == nil && !s.derivation {
			env.own = s.str
		}
		scope := table.environment(env)
		inputs := f.inputs(s.env)
		for _, piece := range f.pieces(s.str) {
			ix.Shells = append(ix.Shells, model.Shell{Span: piece, Scope: scope, Phase: s.phase, Inputs: inputs})
		}
		ix.Holes = append(ix.Holes, f.holes(s.str)...)
	}
	slices.SortFunc(ix.Shells, func(a, b model.Shell) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	slices.SortFunc(ix.Holes, func(a, b model.Hole) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	table.build()
	table.embedHeredocs(m)
	table.embedHosted(found)
	linkCalls(f.tree, ix)
	ix.Runnables = outputs(f.tree, f.table)
	ix.DeferFolds(func() []model.Fold { return folds(f.tree) })
	table.compact()
	return ix
}

func (f *file) holes(str *syntax.String) []model.Hole {
	var out []model.Hole
	f.eachHole(str, func(hole *syntax.Interpolation) {
		if head, exe, named := f.programOf(hole.Value); named {
			out = append(out, model.Hole{Span: hole.Span(), Head: head, Exe: exe})
		}
	})
	return out
}

func (f *file) programOf(e syntax.Expr) (model.Span, string, bool) {
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Ident:
		return e.Span(), "", true
	case *syntax.Select:
		if e.Default == nil {
			return nameSpan(e.Path[len(e.Path)-1]), "", true
		}
	case *syntax.Apply:
		getter := f.callName(e.Fn)
		if !slices.Contains(programGetters, getter) && !slices.Contains(shellEscapes, getter) && getter != toStringName {
			return model.Span{}, "", false
		}
		head, _, named := f.programOf(e.Args[0])
		exe := ""
		if getter == getExeName {
			exe = mainProgram(e.Args[0])
		}
		if len(e.Args) > 1 {
			if literal, ok := syntax.Unparenthesized(e.Args[1]).(*syntax.String); ok && len(literal.Parts) == 1 {
				if text, plain := literal.Parts[0].(*syntax.Literal); plain {
					exe = text.Text
				}
			}
		}
		return head, exe, named
	}
	return model.Span{}, "", false
}

func (f *file) inputs(env environment) []model.Input {
	if env.set == nil {
		return nil
	}
	var out []model.Input
	for _, b := range env.set.Bindings {
		a, assigned := b.(*syntax.Assign)
		if !assigned || !slices.Contains(inputLists, lastIdent(a.Path)) {
			continue
		}
		list := lastIdent(a.Path)
		value := syntax.Unparenthesized(a.Value)
		if with, opened := value.(*syntax.With); opened {
			value = syntax.Unparenthesized(with.Body)
		}
		items, isList := value.(*syntax.List)
		if !isList {
			continue
		}
		for _, item := range items.Items {
			if head, _, named := f.programOf(item); named {
				out = append(out, model.Input{Name: f.src[head.Start:head.End], Span: item.Span(), Head: head, List: list})
			}
		}
	}
	return out
}

func mainProgram(e syntax.Expr) string {
	name := ""
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Ident:
		name = e.Name
	case *syntax.Select:
		if e.Default == nil {
			name = lastIdent(e.Path)
		}
	}
	return cmp.Or(mainPrograms[name], name)
}
