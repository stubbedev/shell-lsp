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
	roomDivisor     = 16
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

	earlier Earlier

	quotesRead  map[*syntax.String]bool
	quotedHoles map[*syntax.Interpolation]bool
}

type Embedding struct {
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
	Findings  []lint.Finding
	Hosted    []HostedText
}

type HostedText struct {
	Name    string
	Family  string
	Placed  bool
	Content model.Span
	Via     *projection.Map
}

type Embedder func(name string) (func(text string) Embedding, bool)

type Analysis struct {
	Hosted    []HostedText
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
	Errors    []syntax.Error
	Scope     *scope.Table
	tree      *syntax.File
}

type Parsed struct {
	src     string
	tree    *syntax.File
	table   *scope.Table
	earlier Earlier
}

func Parse(src string) *Parsed {
	tree := syntax.Parse(src)
	tree.DropWhitespaceTokens()
	return &Parsed{src: src, tree: tree}
}

func (p *Parsed) resolved() *scope.Table {
	if p.table == nil {
		p.table = scope.Resolve(p.tree)
	}
	return p.table
}

func (p *Parsed) Text() string { return p.src }

func (p *Parsed) Includes() []model.Include {
	if paths, plain := p.plainPaths(); plain {
		return appendPathIncludes(nil, p.src, paths, nil)
	}
	ix := &model.Index{}
	newSymbolTable(&file{src: p.src, tree: p.tree, table: p.resolved()}, ix).collect()
	return ix.Includes
}

func (p *Parsed) plainPaths() ([]*syntax.Path, bool) {
	var paths []*syntax.Path
	plain := true
	visit := func(n syntax.Node) bool {
		if path, ok := n.(*syntax.Path); ok && loadable(path) {
			paths = append(paths, path)
			span := path.Span()
			plain = plain && strings.HasSuffix(p.src[span.Start:span.End], nixExtension)
		}
		return plain
	}
	syntax.Inspect(p.tree.Root, visit)
	for _, stray := range p.tree.Stray {
		syntax.Inspect(stray, visit)
	}
	return paths, plain
}

func Analyze(src string) Analysis { return AnalyzeWith(src, nil) }

func AnalyzeWith(src string, embed Embedder) Analysis { return Parse(src).Analyze(embed) }

type Earlier struct {
	Map           *projection.Map
	Refs, Symbols int
}

func EarlierOf(m *projection.Map, ix *model.Index) Earlier {
	if ix == nil {
		return Earlier{Map: m}
	}
	return Earlier{Map: m, Refs: withRoom(len(ix.Refs)), Symbols: withRoom(len(ix.Symbols))}
}

func withRoom(n int) int { return n + n/roomDivisor }

func (p *Parsed) After(earlier Earlier) *Parsed {
	p.earlier = earlier
	return p
}

func (p *Parsed) Analyze(embed Embedder) Analysis {
	f := &file{src: p.src, tree: p.tree, table: p.resolved(), embed: embed, earlier: p.earlier}
	f.discover()
	f.discoverWrapped()
	m, detection := f.projection()
	return Analysis{Hosted: f.hostedTexts(), Map: m, Detection: detection, Index: f.index(m), Errors: f.tree.Errors, Scope: f.table, tree: f.tree}
}

func AnalyzeDependency(src string) Analysis {
	tree := syntax.Parse(src)
	f := &file{src: src, tree: tree, table: scope.Resolve(tree)}
	ix := &model.Index{}
	table := newSymbolTable(f, ix)
	table.build()
	table.sinks()
	table.compact()
	linkCalls(tree, ix)
	m, detection := f.projection()
	tree.Tokens, tree.Comments = nil, nil
	f.table.ForgetUses()
	return Analysis{Map: m, Detection: detection, Index: ix, Scope: f.table, tree: tree}
}

func InStore(path string) bool { return strings.HasPrefix(path, storeDirectory+"/") }

func (f *file) projection() (*projection.Map, dialect.Detection) {
	detection, _ := dialect.Choose(f.found, func(s shellString) dialect.Detection { return s.detection }, tool)
	b := projection.NewBuilder(f.src)
	b.Reuse(f.earlier.Map)
	b.Shell(detection)
	if len(f.found) > 0 {
		b.Reserve(len(f.src) / srcBytesPerSpan)
	}
	units := scriptUnits{builder: b}
	for n, group := range groups(f.found) {
		b.Shell(group[0].detection)
		f.project(b, group, n, &units)
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

func (f *file) project(b *projection.Builder, group []shellString, n int, units *scriptUnits) {
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
	if first.directive == sourcedDirective {
		b.SourcedScript()
	} else {
		b.UnitScript(units.of(first))
	}
	last := 0
	for _, s := range group {
		last = f.body(b, s, opener, shellBody)
		opener = "\n"
	}
	b.Opaque(last, last, f.holeReads(group)+end)
}
