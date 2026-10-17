package hostnav

import (
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
	Files  navigation.WorkspaceFiles
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

func Native(file *workspace.File) bool { return file.Native != nil }

func Build(ctx context.Context, self *workspace.File, env Env) (*Group, error) {
	if self.Native == nil {
		panic("hostnav: " + self.URI + " has no native analysis")
	}
	b := &builder{ctx: ctx, env: env, language: self.Native.Language, files: map[*model.Index]*workspace.File{}, byPath: map[string]*workspace.File{}, modules: map[string]*model.Module{}, resolved: map[string][]resolvedInclude{}}
	self = b.canonical(self)
	importers, parents, err := b.importers(self)
	if err != nil {
		return nil, err
	}
	roots := b.roots(self, importers)
	var module *model.Module
	if len(roots) == 1 {
		module = b.module(roots[0])
	} else {
		module = b.merged(roots)
	}
	g := &Group{Self: self, Language: b.language, Module: module, files: b.files}
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
	for _, p := range parents {
		g.parents = append(g.parents, parent{module: b.module(p.file), name: p.name})
	}
	return g, nil
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

type modParent struct {
	file *workspace.File
	name string
}

func (b *builder) importers(self *workspace.File) (map[string][]*workspace.File, []modParent, error) {
	out := map[string][]*workspace.File{}
	var parents []modParent
	if b.env.Files == nil {
		return out, nil, nil
	}
	candidates, err := b.env.Files(b.ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range candidates {
		if c.Native == nil || c.Native.Language != b.language {
			continue
		}
		c = b.canonical(c)
		for _, r := range b.includes(c) {
			if r.target == nil {
				continue
			}
			if r.include.Module != "" {
				if r.target.Path == self.Path {
					parents = append(parents, modParent{file: c, name: r.include.Module})
				}
				continue
			}
			out[r.target.Path] = append(out[r.target.Path], c)
		}
	}
	return out, parents, nil
}

func (b *builder) roots(self *workspace.File, importers map[string][]*workspace.File) []*workspace.File {
	var roots []*workspace.File
	seen := map[string]bool{self.Path: true}
	queue := []*workspace.File{self}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		above := importers[f.Path]
		if len(above) == 0 {
			roots = append(roots, f)
			continue
		}
		for _, up := range above {
			if !seen[up.Path] {
				seen[up.Path] = true
				queue = append(queue, up)
			}
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
