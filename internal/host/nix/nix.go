package nix

import (
	"strconv"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

var phaseDirective = directive.Render(directive.Disable, "SC2154") + "\n"

const (
	tool          = "nix"
	strictMode    = "set -o errexit -o nounset -o pipefail\n"
	phaseFunction = "nixPhase"
)

type file struct {
	src   string
	tree  *syntax.File
	table *scope.Table
	found []shellString
}

type Analysis struct {
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
	Errors    []syntax.Error
	Scope     *scope.Table
	strings   []model.Span
}

func Analyze(src string) Analysis {
	tree := syntax.Parse(src)
	f := &file{src: src, tree: tree, table: scope.Resolve(tree)}
	f.discover()
	detection, _ := dialect.Choose(f.found, func(s shellString) dialect.Detection { return s.detection }, tool)
	b := projection.NewBuilder(src)
	b.Shell(detection)
	for n, group := range groups(f.found) {
		b.Shell(group[0].detection)
		f.project(b, group, n)
	}
	a := Analysis{Map: b.Map(), Detection: detection, Index: f.index(f.found), Errors: f.tree.Errors, Scope: f.table}
	if len(f.found) > 0 {
		a.strings = make([]model.Span, 0, 2*len(f.found))
	}
	for _, s := range f.found {
		a.strings = append(a.strings, s.str.Span(), s.str.Content)
	}
	return a
}

func (a Analysis) Enclosing(offset int) []model.Span { return model.Nested(offset, a.strings) }

func groups(found []shellString) [][]shellString {
	var out [][]shellString
	for _, s := range found {
		if n := len(out); n > 0 && s.joined && out[n-1][0].context.sameBinding(s.context) {
			out[n-1] = append(out[n-1], s)
			continue
		}
		out = append(out, []shellString{s})
	}
	return out
}

func (f *file) project(b *projection.Builder, group []shellString, n int) {
	first := group[0]
	opener, end := first.directive+"(\n"+first.prelude, closing
	if first.derivation {
		name := phaseFunction + strconv.Itoa(n)
		opener = phaseDirective + first.directive + name + "() {\n" + strictMode + first.prelude
		end = "\n:\n}\n" + name + "\n"
	}
	b.Script()
	last := 0
	for _, s := range group {
		last = f.body(b, s, opener)
		opener = "\n"
	}
	b.Opaque(last, last, end)
}
