package nix

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

type envSource uint8

const (
	derivationEnv envSource = iota
	unitEnv
	appEnv
)

var envDocs = [...]string{
	derivationEnv: "Set in the environment by the derivation.",
	unitEnv:       "Set in the environment by the unit's `environment`.",
	appEnv:        "Set in the environment by `runtimeEnv`.",
}

var (
	unexported  = []string{"meta", "passthru", "env"}
	envChildren = [...]string{derivationEnv: "env", unitEnv: "environment", appEnv: "runtimeEnv"}
)

var Checks = model.Checks{Of: func(model.Kind) model.Rules { return model.Rules{} }}

type environment struct {
	set    *syntax.AttrSet
	source envSource
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

func nested(bindings []binding, key string) []binding {
	var out []binding
	for _, b := range bindings {
		switch {
		case b.path[0] != key:
		case len(b.path) == 1:
			out = append(out, children(b.value)...)
		case len(b.path) == 2:
			out = append(out, b)
		}
	}
	return out
}

func vars(env environment) []binding {
	bindings := bindingsOf(env.set)
	out := nested(bindings, envChildren[env.source])
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

func (f *file) detail(b binding) string {
	text := f.src[b.span.Start:b.span.End]
	if !strings.Contains(text, "\n") {
		return text
	}
	name := f.src[b.name.Start:b.name.End]
	if b.inherit {
		return "inherit " + name + ";"
	}
	return f.src[b.span.Start:b.name.End] + " = …;"
}

func (f *file) index(found []shellString) *model.Index {
	ix := &model.Index{}
	for _, e := range f.tree.Errors {
		ix.Findings = append(ix.Findings, lint.HostFinding(lint.CodeNixSyntax, e.Span, e.Message))
	}
	ix.Findings = append(ix.Findings, check(f.tree, f.table)...)
	table := newSymbolTable(f, ix)
	scopes := map[environment]model.Scope{}
	for _, s := range found {
		if s.env.set == nil {
			continue
		}
		scope, ok := scopes[s.env]
		if !ok {
			scope = table.environment(s.env)
			scopes[s.env] = scope
		}
		for _, piece := range pieces(s.str) {
			ix.Shells = append(ix.Shells, model.Shell{Span: piece, Scope: scope})
		}
	}
	slices.SortFunc(ix.Shells, func(a, b model.Shell) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	table.build()
	ix.Folds = folds(f.tree)
	return ix
}

func pieces(str *syntax.String) []model.Span {
	start := str.Content.Start
	var out []model.Span
	for _, part := range str.Parts {
		if _, ok := part.(*syntax.Interpolation); !ok {
			continue
		}
		hole := part.Span()
		if hole.Start > start {
			out = append(out, model.Span{Start: start, End: hole.Start})
		}
		start = hole.End
	}
	if str.Content.End > start {
		out = append(out, model.Span{Start: start, End: str.Content.End})
	}
	return out
}
