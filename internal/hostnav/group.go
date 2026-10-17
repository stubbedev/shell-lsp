package hostnav

import (
	"cmp"
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Loader interface {
	Load(path string) (*workspace.File, error)
	LoadAs(path string, language host.Language) (*workspace.File, error)
}

type Env struct {
	Loader Loader
	Home   string
	Graph  func(ctx context.Context) (*Graph, error)
	Exists func(path string) bool
}

type importer struct {
	path   string
	module string
}

type Graph struct {
	importers map[string][]importer
}

func NewGraph(ctx context.Context, language host.Language, files navigation.WorkspaceFiles, env Env) (*Graph, error) {
	g := &Graph{importers: map[string][]importer{}}
	candidates, err := files(ctx)
	if err != nil {
		return nil, err
	}
	b := newBuilder(ctx, env, language)
	for _, c := range candidates {
		if c.Native == nil || c.Native.Language != language {
			continue
		}
		c = b.canonical(c)
		for _, r := range b.includes(c) {
			if r.target != nil {
				g.importers[r.target.Path] = append(g.importers[r.target.Path], importer{path: c.Path, module: r.include.Module})
			}
		}
	}
	for path, list := range g.importers {
		slices.SortFunc(list, func(a, b importer) int {
			return cmp.Or(strings.Compare(a.path, b.path), strings.Compare(a.module, b.module))
		})
		g.importers[path] = slices.Compact(list)
	}
	return g, nil
}

func (g *Graph) of(path string) []importer {
	if g == nil {
		return nil
	}
	return g.importers[path]
}

type parent struct {
	module *model.Module
	name   string
}

type Group struct {
	Self     *workspace.File
	Language host.Language
	Module   *model.Module
	self     int
	files    map[*model.Index]*workspace.File
	missing  []model.Include
	includes []resolvedInclude
	parents  []parent
	exists   func(path string) bool
}

type builder struct {
	ctx      context.Context
	env      Env
	language host.Language
	files    map[*model.Index]*workspace.File
	byPath   map[string]*workspace.File
	modules  map[string]*model.Module
	resolved map[string][]resolvedInclude
}

type resolvedInclude struct {
	include model.Include
	target  *workspace.File
}

type Host struct {
	file *workspace.File
}

func Of(file *workspace.File) (Host, bool) {
	if file.Native == nil {
		return Host{}, false
	}
	return Host{file: file}, true
}

func (h Host) File() *workspace.File { return h.file }

func (h Host) Language() host.Language { return h.file.Native.Language }

func newBuilder(ctx context.Context, env Env, language host.Language) *builder {
	return &builder{ctx: ctx, env: env, language: language, files: map[*model.Index]*workspace.File{}, byPath: map[string]*workspace.File{}, modules: map[string]*model.Module{}, resolved: map[string][]resolvedInclude{}}
}

func Build(ctx context.Context, h Host, env Env) (*Group, error) {
	var graph *Graph
	if env.Graph != nil {
		var err error
		if graph, err = env.Graph(ctx); err != nil {
			return nil, err
		}
	}
	b := newBuilder(ctx, env, h.Language())
	self := b.canonical(h.file)
	roots := b.roots(self, graph)
	var module *model.Module
	if len(roots) == 1 {
		module = b.module(roots[0])
	} else {
		module = b.merged(roots)
	}
	if slices.ContainsFunc(roots, func(root *workspace.File) bool { return b.fragment(root, graph) }) {
		module.Complete = false
	}
	g := &Group{Self: self, Language: b.language, Module: module, files: b.files, exists: env.Exists}
	g.self = slices.Index(module.Files, self.Native.Index)
	if g.self < 0 {
		panic("hostnav: " + self.URI + " is missing from its own module")
	}
	g.includes = b.includes(self)
	for _, r := range g.includes {
		if r.target == nil && !r.include.Optional && !r.include.Dynamic {
			g.missing = append(g.missing, r.include)
		}
	}
	for _, imp := range graph.of(self.Path) {
		if imp.module == "" {
			continue
		}
		if file := b.load(imp.path); file != nil {
			g.parents = append(g.parents, parent{module: b.module(file), name: imp.module})
		}
	}
	return g, nil
}

func (b *builder) fragment(root *workspace.File, graph *Graph) bool {
	return !b.language.EntryPoint(root.Path) && len(graph.of(root.Path)) == 0
}

func (b *builder) candidates(path string) []string {
	if strings.HasPrefix(path, "~/") && b.env.Home != "" {
		path = filepath.Join(b.env.Home, path[2:])
	}
	return []string{path}
}

func (b *builder) canonical(f *workspace.File) *workspace.File {
	if known, ok := b.byPath[f.Path]; ok {
		return known
	}
	b.byPath[f.Path] = f
	b.files[f.Native.Index] = f
	return f
}

func (b *builder) includes(f *workspace.File) []resolvedInclude {
	f = b.canonical(f)
	if done, ok := b.resolved[f.Path]; ok {
		return done
	}
	dir := filepath.Dir(f.Path)
	var out []resolvedInclude
	for _, inc := range f.Native.Index.Includes {
		r := resolvedInclude{include: inc}
		if !inc.Dynamic && f.Path != "" {
			r.target = b.find(dir, inc.Candidates)
		}
		out = append(out, r)
	}
	b.resolved[f.Path] = out
	return out
}

func (b *builder) find(dir string, candidates []string) *workspace.File {
	for _, c := range candidates {
		for _, path := range b.candidates(c) {
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if target, err := b.env.Loader.LoadAs(filepath.Clean(path), b.language); err == nil && target.Native != nil {
				return b.canonical(target)
			}
		}
	}
	return nil
}

func (b *builder) closure(root *workspace.File) []*workspace.File {
	root = b.canonical(root)
	out := []*workspace.File{root}
	seen := map[string]bool{root.Path: true}
	for i := 0; i < len(out); i++ {
		for _, r := range b.includes(out[i]) {
			if r.include.Module != "" || r.target == nil || seen[r.target.Path] {
				continue
			}
			seen[r.target.Path] = true
			out = append(out, r.target)
		}
	}
	return out
}

func (b *builder) module(root *workspace.File) *model.Module {
	if m, ok := b.modules[root.Path]; ok {
		return m
	}
	m := &model.Module{Complete: true, Builtins: b.language.Builtins(), Submodules: map[string]*model.Module{}}
	b.modules[root.Path] = m
	b.fill(m, b.closure(root))
	return m
}

func (b *builder) merged(roots []*workspace.File) *model.Module {
	m := &model.Module{Complete: true, Builtins: b.language.Builtins(), Submodules: map[string]*model.Module{}}
	seen := map[string]bool{}
	var files []*workspace.File
	for _, root := range roots {
		for _, f := range b.closure(root) {
			if !seen[f.Path] {
				seen[f.Path] = true
				files = append(files, f)
			}
		}
	}
	b.fill(m, files)
	return m
}

func (b *builder) fill(m *model.Module, files []*workspace.File) {
	for _, f := range files {
		m.Files = append(m.Files, f.Native.Index)
	}
	for _, f := range files {
		for _, r := range b.includes(f) {
			if r.target == nil && (!r.include.Optional || r.include.Dynamic) {
				m.Complete = false
			}
			if r.include.Module == "" {
				continue
			}
			if r.target == nil {
				if _, ok := m.Submodules[r.include.Module]; !ok {
					m.Submodules[r.include.Module] = nil
				}
				continue
			}
			m.Submodules[r.include.Module] = b.module(r.target)
		}
	}
}

func (b *builder) load(path string) *workspace.File {
	file, err := b.env.Loader.LoadAs(path, b.language)
	if err != nil || file.Native == nil {
		return nil
	}
	return b.canonical(file)
}

func (b *builder) roots(self *workspace.File, graph *Graph) []*workspace.File {
	var roots []*workspace.File
	seen := map[string]bool{self.Path: true}
	queue := []*workspace.File{self}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		above := 0
		for _, imp := range graph.of(f.Path) {
			if imp.module != "" {
				continue
			}
			up := b.load(imp.path)
			if up == nil {
				continue
			}
			above++
			if !seen[up.Path] {
				seen[up.Path] = true
				queue = append(queue, up)
			}
		}
		if above == 0 {
			roots = append(roots, f)
		}
	}
	if len(roots) == 0 {
		return []*workspace.File{self}
	}
	slices.SortFunc(roots, func(a, b *workspace.File) int { return strings.Compare(a.Path, b.Path) })
	return roots
}

func (g *Group) File(ix *model.Index) *workspace.File { return g.files[ix] }

func (g *Group) Index() *model.Index { return g.Module.Files[g.self] }
