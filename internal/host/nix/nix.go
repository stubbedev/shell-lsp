package nix

import (
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

var phaseDirective = directive.Render(directive.Disable, "SC2154") + "\n"

const (
	srcBytesPerSpan = 32
	tool            = "nix"
	strictMode      = "set -o errexit -o nounset -o pipefail\n"
	phaseFunction   = "nixPhase"
)

type file struct {
	src   string
	tree  *syntax.File
	table *scope.Table
	found []shellString
	path  []string
	embed Embedder

	quotesRead  map[*syntax.String]bool
	quotedHoles map[*syntax.Interpolation]bool
}

type Embedding struct {
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
	Findings  []lint.Finding
}

type Embedder func(name string) (func(text string) Embedding, bool)

type Analysis struct {
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
	Errors    []syntax.Error
	Scope     *scope.Table
	tree      *syntax.File
}

func Analyze(src string) Analysis { return AnalyzeWith(src, nil) }

func AnalyzeWith(src string, embed Embedder) Analysis {
	tree := syntax.Parse(src)
	f := &file{src: src, tree: tree, table: scope.Resolve(tree), embed: embed}
	f.discover()
	f.discoverWrapped()
	m, detection := f.projection()
	return Analysis{Map: m, Detection: detection, Index: f.index(m), Errors: f.tree.Errors, Scope: f.table, tree: f.tree}
}

func AnalyzeDependency(src string) Analysis {
	tree := syntax.Parse(src)
	f := &file{src: src, tree: tree, table: scope.Resolve(tree)}
	ix := &model.Index{}
	table := newSymbolTable(f, ix)
	table.build()
	table.compact()
	linkCalls(tree, ix)
	m, detection := f.projection()
	tree.Tokens, tree.Comments = nil, nil
	f.table.ForgetUses()
	return Analysis{Map: m, Detection: detection, Index: ix, Scope: f.table, tree: tree}
}

func InStore(path string) bool { return strings.HasPrefix(path, storeDirectory+"/") }

func Includes(src string) []model.Include {
	tree := syntax.Parse(src)
	ix := &model.Index{}
	newSymbolTable(&file{src: src, tree: tree, table: scope.Resolve(tree)}, ix).build()
	return ix.Includes
}

func (f *file) projection() (*projection.Map, dialect.Detection) {
	detection, _ := dialect.Choose(f.found, func(s shellString) dialect.Detection { return s.detection }, tool)
	b := projection.NewBuilder(f.src)
	b.Shell(detection)
	if len(f.found) > 0 {
		b.Reserve(len(f.src) / srcBytesPerSpan)
	}
	for n, group := range groups(f.found) {
		b.Shell(group[0].detection)
		f.project(b, group, n)
	}
	return b.Map(), detection
}

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
	if first.hosted != nil {
		b.Embed(first.hosted.inner.Map, first.hosted.via)
		return
	}
	prelude := first.prelude + f.spacelessPrelude(group)
	opener, end := first.directive+"(\n"+prelude, closing
	if first.derivation {
		name := phaseFunction + strconv.Itoa(n)
		opener = phaseDirective + first.directive + name + "() {\n" + strictMode + prelude
		end = "\n:\n}\n" + name + "\n"
	}
	b.Script()
	last := 0
	for _, s := range group {
		last = f.body(b, s, opener, shellBody)
		opener = "\n"
	}
	b.Opaque(last, last, end)
}
