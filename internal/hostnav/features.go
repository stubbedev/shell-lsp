package hostnav

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func (g *Group) Findings() []lint.Finding {
	checks := g.Language.Checks()
	ix := g.Index()
	used := g.Module.Usage()
	for t := range g.shellUsage() {
		used[t] = true
	}
	out := slices.Clone(ix.Findings)
	out = append(out, g.Module.Findings(g.self, checks, used)...)
	for _, inc := range g.missing {
		out = append(out, model.MissingInclude(checks, inc))
	}
	return out
}

type Location struct {
	File *workspace.File
	Span parser.Span
}

func (g *Group) includeAt(offset int) (resolvedInclude, bool) {
	for _, r := range g.includes {
		if r.include.Span.Touches(offset) {
			return r, true
		}
	}
	return resolvedInclude{}, false
}

func (g *Group) Definition(offset int) []Location {
	if r, ok := g.includeAt(offset); ok {
		if r.target == nil {
			return nil
		}
		return []Location{{File: r.target}}
	}
	sym, ok := g.At(offset)
	if !ok {
		return nil
	}
	return g.definitions(sym)
}

func (g *Group) definitions(sym Symbol) []Location {
	if sym.Kind == model.KindModule {
		if root := g.moduleRoot(sym); root != nil {
			return []Location{{File: root}}
		}
	}
	var out []Location
	for _, d := range g.Declarations(sym) {
		out = append(out, Location{File: d.File, Span: d.Span})
	}
	return out
}

func (g *Group) moduleRoot(sym Symbol) *workspace.File {
	for _, t := range sym.targets {
		sub := t.Module.Submodules[t.Info().Name]
		if sub != nil && len(sub.Files) > 0 {
			return g.files[sub.Files[0]]
		}
	}
	return nil
}

func (g *Group) ShellDefinition(sym Symbol) []Location { return g.definitions(sym) }

func References(g *Group, sym Symbol, includeDeclaration bool) []Occurrence {
	occs := g.Occurrences(sym)
	if includeDeclaration {
		return occs
	}
	return slices.DeleteFunc(occs, func(o Occurrence) bool { return o.Declaration })
}

func (g *Group) Highlights(sym Symbol) []Occurrence {
	return slices.DeleteFunc(g.Occurrences(sym), func(o Occurrence) bool { return o.File.Path != g.Self.Path })
}

func (g *Group) PrepareRename(sym Symbol) error {
	if !sym.Defined() {
		return fmt.Errorf("%w: %s `%s` is built in", navigation.ErrNotRenamable, sym.Kind, sym.Name)
	}
	if !g.Language.Renames() {
		return fmt.Errorf("%w: %s `%s` is defined in a %s, which other code may read by its name; rename it with that language's own server", navigation.ErrNotRenamable, sym.Kind, sym.Name, g.Language)
	}
	if sym.Kind == model.KindModule && g.moduleRoot(sym) != nil && !g.explicitPath(sym) {
		return fmt.Errorf("%w: module `%s` is found by its name; give it an explicit path first", navigation.ErrNotRenamable, sym.Name)
	}
	return nil
}

func (g *Group) explicitPath(sym Symbol) bool {
	for _, t := range sym.targets {
		ix := t.Module.Files[t.File]
		for _, inc := range ix.Includes {
			if inc.Module == sym.Name && inc.Path != sym.Name {
				return true
			}
		}
	}
	return false
}

func (g *Group) Rename(sym Symbol, newName string) ([]Occurrence, error) {
	if err := g.PrepareRename(sym); err != nil {
		return nil, err
	}
	occs := g.Occurrences(sym)
	shell := slices.ContainsFunc(occs, func(o Occurrence) bool { return o.Shell })
	if !g.Language.ValidName(sym.Kind, newName) || shell && sym.Kind != g.Language.Recursion().Kind && !parser.IsName(newName) {
		return nil, fmt.Errorf("%w: %q is not a valid %s name", navigation.ErrInvalidName, newName, sym.Kind)
	}
	if newName == sym.Name {
		return nil, nil
	}
	t := sym.targets[0]
	if clash := t.Module.Resolve(t.File, model.Ref{Name: newName, Kind: sym.Kind, Scope: t.Info().Scope}); len(clash.Targets) > 0 {
		return nil, fmt.Errorf("%w: %s `%s` already exists", navigation.ErrNameConflict, clash.Targets[0].Info().Kind, newName)
	}
	return occs, nil
}

type Hover struct {
	Markdown string
	Span     parser.Span
}

func (g *Group) Hover(offset int) (Hover, bool) {
	if r, ok := g.includeAt(offset); ok {
		if r.target == nil {
			return Hover{Markdown: "`" + r.include.Path + "` is not found", Span: r.include.Span}, true
		}
		return Hover{Markdown: "`" + r.target.Path + "`", Span: r.include.Span}, true
	}
	sym, ok := g.At(offset)
	if !ok {
		return Hover{}, false
	}
	return g.Describe(sym), true
}

func (g *Group) Describe(sym Symbol) Hover {
	fence := g.Language.Fence()
	if b := sym.Builtin; b != nil {
		md := "```" + fence + "\n" + b.Signature + "\n```\n\n" + b.Doc
		if b.Deprecated != "" {
			md += "\n\n**Deprecated:** " + b.Deprecated + "."
		}
		return Hover{Markdown: md, Span: sym.Span}
	}
	var parts []string
	for _, t := range sym.targets {
		s := t.Info()
		md := "```" + fence + "\n" + s.Detail + "\n```"
		if s.Doc != "" {
			md += "\n\n" + s.Doc
		}
		md += "\n\n*" + s.Kind.String() + "*"
		if s.Exported {
			md += ", exported to the shell"
		}
		if file := g.files[t.Module.Files[t.File]]; file != nil && file.Path != g.Self.Path {
			md += " in `" + filepath.Base(file.Path) + "`"
		}
		parts = append(parts, md)
	}
	return Hover{Markdown: strings.Join(parts, "\n\n---\n\n"), Span: sym.Span}
}

type Item struct {
	Label  string
	Kind   model.Kind
	Detail string
	Doc    string
	Span   parser.Span
}

func (g *Group) Complete(offset int) []Item {
	expect, ok := g.Self.Native.Expect(offset)
	if !ok {
		return nil
	}
	module := g.Module
	prefix := g.Self.Doc.Source().Text()[expect.Span.Start:min(offset, expect.Span.End)]
	if i := strings.LastIndex(prefix, "::"); i >= 0 {
		for name := range strings.SplitSeq(prefix[:i], "::") {
			if module = module.Submodules[name]; module == nil {
				return nil
			}
		}
		expect.Span.Start += i + 2
		expect.Scope = model.Global
	}
	var out []Item
	seen := map[string]bool{}
	add := func(item Item) {
		key := item.Kind.String() + "\x00" + item.Label
		if !seen[key] {
			seen[key] = true
			item.Span = expect.Span
			out = append(out, item)
		}
	}
	for _, kind := range expect.Kinds {
		for f, ix := range module.Files {
			for _, s := range ix.Symbols {
				visible := s.Scope == model.Global || f == g.self && module == g.Module && s.Scope == expect.Scope
				if s.Kind == kind && visible {
					add(Item{Label: s.Name, Kind: s.Kind, Detail: s.Detail, Doc: s.Doc})
				}
			}
		}
		if kind == model.KindModule {
			for name, sub := range module.Submodules {
				if sub != nil {
					add(Item{Label: name, Kind: model.KindModule, Detail: "mod " + name})
				}
			}
		}
		for _, b := range module.Builtins.Of(kind) {
			add(Item{Label: b.Name, Kind: kind, Detail: b.Signature, Doc: b.Doc})
		}
	}
	slices.SortStableFunc(out, func(a, b Item) int { return strings.Compare(a.Label, b.Label) })
	return out
}

type Link struct {
	Span parser.Span
	Path string
}

func (g *Group) Links() []Link {
	var out []Link
	for _, r := range g.includes {
		if r.target != nil {
			out = append(out, Link{Span: r.include.Span, Path: r.target.Path})
		}
	}
	if g.exists == nil {
		return out
	}
	ix := g.Index()
	for _, p := range ix.Paths {
		path := filepath.Join(filepath.Dir(g.Self.Path), filepath.FromSlash(strings.TrimPrefix(p.Path, "/")))
		if g.exists(path) {
			out = append(out, Link{Span: p.Span, Path: path})
		}
	}
	for _, r := range ix.Refs {
		if r.Kind != model.KindTarget || r.Owner < 0 || g.Module.Resolve(g.self, r).Found() {
			continue
		}
		if path, ok := g.prerequisite(r.Name); ok {
			out = append(out, Link{Span: r.Span, Path: path})
		}
	}
	return out
}

func (g *Group) prerequisite(name string) (string, bool) {
	dir := filepath.Dir(g.Self.Path)
	candidates := []string{name}
	if !filepath.IsAbs(name) {
		var search []model.SearchPath
		for _, ix := range g.Module.Files {
			search = append(search, ix.Search...)
		}
		for _, wanted := range []bool{true, false} {
			for _, s := range search {
				if (s.Pattern != "") != wanted || wanted && !model.MatchesPattern(s.Pattern, name) {
					continue
				}
				for _, d := range s.Dirs {
					candidates = append(candidates, filepath.Join(d, name))
				}
			}
		}
	}
	for _, c := range candidates {
		if !filepath.IsAbs(c) {
			c = filepath.Join(dir, c)
		}
		if g.exists(c) {
			return c, true
		}
	}
	return "", false
}
