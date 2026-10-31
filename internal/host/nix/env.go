package nix

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/parser"
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
	open   int
	source envSource
}

type binding struct {
	path    string
	name    model.Span
	start   int
	end     int
	value   int
	inherit bool
}

func (f *file) attribute(top *frame, i int) {
	if top.kind != attrsFrame {
		return
	}
	t := f.tokens[i]
	switch {
	case t.keyword("let"):
		top.lets++
		return
	case t.keyword("in") && top.lets > 0:
		top.lets--
		return
	case top.lets > 0:
		return
	case t.keyword("with") || t.keyword("assert"):
		top.pending++
	case t.is(";"):
		if top.pending > 0 {
			top.pending--
			return
		}
		for _, b := range top.current {
			f.bindings[top.open][b].end = t.end
		}
		top.current, top.expect, top.inherit = nil, true, noBrace
	case top.inherit != noBrace:
		if t.kind == identToken && !strings.Contains(t.text, ".") {
			top.current = append(top.current, f.bind(top.open, binding{path: t.text, name: model.Span{Start: t.start, End: t.end}, start: top.inherit, end: t.end, value: noBrace, inherit: true}))
		}
	case !top.expect:
	case t.keyword("inherit"):
		top.expect, top.inherit = false, t.start
	case i+1 < len(f.tokens) && f.tokens[i+1].is("="):
		top.expect = false
		if path, name, ok := f.attrName(t); ok {
			top.current = []int{f.bind(top.open, binding{path: path, name: name, start: t.start, end: name.End, value: noBrace})}
		}
	default:
		top.expect = false
	}
}

func (f *file) bind(open int, b binding) int {
	f.bindings[open] = append(f.bindings[open], b)
	return len(f.bindings[open]) - 1
}

func (f *file) attrName(t token) (string, model.Span, bool) {
	switch {
	case t.kind == identToken:
		name := t.name()
		return t.text, model.Span{Start: t.end - len(name), End: t.end}, true
	case t.kind == stringToken && !t.indented && t.end-t.start >= 2 && f.src[t.end-1] == '"':
		inner := f.src[t.start+1 : t.end-1]
		if strings.ContainsAny(inner, `\$`) {
			return "", model.Span{}, false
		}
		return inner, model.Span{Start: t.start + 1, End: t.end - 1}, true
	}
	return "", model.Span{}, false
}

func (f *file) children(open int) []binding {
	if open == noBrace {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(f.bindings[open]), func(b binding) bool { return strings.Contains(b.path, ".") })
}

func (f *file) nested(bindings []binding, key string) []binding {
	var out []binding
	for _, b := range bindings {
		head, rest, dotted := strings.Cut(b.path, ".")
		switch {
		case head != key:
		case !dotted:
			out = append(out, f.children(b.value)...)
		case !strings.Contains(rest, "."):
			out = append(out, b)
		}
	}
	return out
}

func (f *file) vars(env environment) []binding {
	bindings := f.bindings[env.open]
	out := f.nested(bindings, envChildren[env.source])
	if env.source != derivationEnv {
		return out
	}
	for _, b := range bindings {
		if !strings.Contains(b.path, ".") && !strings.HasPrefix(b.path, "__") && !slices.Contains(unexported, b.path) {
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, func(a, b binding) int { return cmp.Compare(a.name.Start, b.name.Start) })
	return out
}

func (f *file) detail(b binding) string {
	text := f.src[b.start:b.end]
	if !strings.Contains(text, "\n") {
		return text
	}
	name := f.src[b.name.Start:b.name.End]
	if b.inherit {
		return "inherit " + name + ";"
	}
	return f.src[b.start:b.name.End] + " = …;"
}

func (f *file) index(found []shellString) *model.Index {
	ix := &model.Index{}
	scopes := map[environment]model.Scope{}
	for _, s := range found {
		if s.env.open == noBrace {
			continue
		}
		scope, ok := scopes[s.env]
		if !ok {
			scope = model.Scope(len(scopes) + 1)
			scopes[s.env] = scope
			for _, b := range f.vars(s.env) {
				name := f.src[b.name.Start:b.name.End]
				if !parser.IsName(name) {
					continue
				}
				ix.Symbols = append(ix.Symbols, model.Symbol{
					Name: name, Kind: model.KindVariable, Span: model.Span{Start: b.start, End: b.end}, Selection: b.name,
					Scope: scope, Parent: model.NoParent, Detail: f.detail(b), Doc: envDocs[s.env.source], Exported: true,
				})
			}
		}
		start, end := contentBounds(f.src, s.tok)
		for _, piece := range f.pieces(start, end) {
			ix.Shells = append(ix.Shells, model.Shell{Span: piece, Scope: scope})
		}
	}
	slices.SortFunc(ix.Shells, func(a, b model.Shell) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	return ix
}

func (f *file) pieces(start, end int) []model.Span {
	holes := slices.DeleteFunc(slices.Clone(f.holes), func(h model.Span) bool { return h.Start < start || h.End > end })
	slices.SortFunc(holes, func(a, b model.Span) int { return cmp.Compare(a.Start, b.Start) })
	var out []model.Span
	for _, h := range append(holes, model.Span{Start: end, End: end}) {
		if h.Start > start {
			out = append(out, model.Span{Start: start, End: h.Start})
		}
		start = max(start, h.End)
	}
	return out
}
