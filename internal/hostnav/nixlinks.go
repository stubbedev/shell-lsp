package hostnav

import (
	"path/filepath"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/assist"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/filelint"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func (g *Group) nixDocument() *nix.Document { return g.Self.Native.Nix() }

func (g *Group) NixClasses() []nix.OptionClass {
	project, ok := g.Module.Links().(*nix.Project)
	if !ok {
		return nil
	}
	return project.OptionClasses(g.Self.Path)
}

func checksUnusedNixDirectives(settings filelint.Settings) bool {
	enabled := func(names []string) bool {
		return slices.Contains(names, nix.UnusedDirectivesCheck()) || slices.Contains(names, directive.All)
	}
	return settings.Profile == lint.ProfileStrict || enabled(settings.Optional) || enabled(settings.RC.Enabled)
}

func UnusedNixDirectives(h Host, settings filelint.Settings) []lint.Finding {
	if h.Language() != host.Nix || !checksUnusedNixDirectives(settings) {
		return nil
	}
	return nix.UnusedDirectiveFindings(h.File().Doc.Source().Text())
}

func (g *Group) SourceLinks() []assist.Link {
	if g.Language != host.Nix {
		return nil
	}
	doc := g.nixDocument()
	var out []assist.Link
	for _, p := range doc.PathLiterals() {
		if path, ok := g.nixFile(p); ok {
			out = append(out, assist.Link{Span: p.Span, Target: fileuri.FromPath(path)})
		}
	}
	text := g.Self.Doc.Source().Text()
	for _, run := range doc.Prose() {
		for url := range assist.WebURLs(text[run.Span.Start:run.Span.End]) {
			span := parser.Span{Start: run.Span.Start + url.Start, End: run.Span.Start + url.End}
			if !run.Continues || span.End != run.Span.End {
				out = append(out, assist.Link{Span: span, Target: text[span.Start:span.End]})
			}
		}
	}
	return out
}

func (g *Group) nixPath(p nix.PathLiteral) string {
	path := filepath.FromSlash(p.Path)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(filepath.Dir(g.Self.Path), path)
}

func (g *Group) nixFile(p nix.PathLiteral) (string, bool) {
	if g.exists == nil {
		return "", false
	}
	if path := g.nixPath(p); g.exists(path) {
		return path, true
	}
	return g.nixDirectoryEntry(p)
}

func (g *Group) nixDirectoryEntry(p nix.PathLiteral) (string, bool) {
	if g.exists == nil || !p.Loaded {
		return "", false
	}
	path := g.nixPath(p)
	if g.exists(path) {
		return "", false
	}
	entry := filepath.Join(path, nix.DirectoryEntry)
	return entry, g.exists(entry)
}
