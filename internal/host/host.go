package host

import (
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/justfile"
	"github.com/stubbedev/shell-lsp/internal/host/makefile"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

type Language uint8

const (
	Make Language = iota + 1
	Just
	Nix
)

func (l Language) String() string {
	switch l {
	case Make:
		return "Makefile"
	case Just:
		return "justfile"
	case Nix:
		return "Nix file"
	}
	return "unknown"
}

type definition struct {
	language    Language
	languageIDs []string
	names       []string
	extensions  []string
	project     func(text string) (*projection.Map, dialect.Detection)
}

var definitions = []definition{
	{
		language:    Make,
		languageIDs: []string{"makefile", "make"},
		names:       []string{"Makefile", "makefile", "GNUmakefile"},
		extensions:  []string{".mk", ".mak"},
		project:     makefile.Project,
	},
	{
		language:    Just,
		languageIDs: []string{"just"},
		names:       []string{"justfile", "Justfile", "JUSTFILE", ".justfile"},
		extensions:  []string{".just"},
		project:     justfile.Project,
	},
	{
		language:    Nix,
		languageIDs: []string{"nix"},
		extensions:  []string{".nix"},
		project:     nix.Project,
	},
}

func (l Language) definition() definition {
	i := slices.IndexFunc(definitions, func(d definition) bool { return d.language == l })
	return definitions[i]
}

func Of(uri, languageID string) (Language, bool) {
	if languageID != "" {
		i := slices.IndexFunc(definitions, func(d definition) bool { return slices.Contains(d.languageIDs, languageID) })
		if i < 0 {
			return 0, false
		}
		return definitions[i].language, true
	}
	return FromName(uri)
}

func FromName(name string) (Language, bool) {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	for _, d := range definitions {
		if slices.Contains(d.names, base) || slices.ContainsFunc(d.extensions, func(ext string) bool { return strings.HasSuffix(base, ext) }) {
			return d.language, true
		}
	}
	return 0, false
}

func Names() []string {
	var out []string
	for _, d := range definitions {
		out = append(out, d.names...)
	}
	return out
}

func Extensions() []string {
	var out []string
	for _, d := range definitions {
		out = append(out, d.extensions...)
	}
	return out
}

type Script struct {
	Doc       *docstore.Document
	Detection dialect.Detection
	scripts   []parser.Span
}

func (l Language) Project(doc *docstore.Document) Script {
	m, detection := l.definition().project(doc.Text())
	return Script{Doc: docstore.Project(doc, m), Detection: detection, scripts: m.Scripts()}
}

func (s Script) Parse(like *parser.Tree) *parser.Tree {
	d := s.Detection.Dialect
	tree := parser.ParseScripts(like, s.Doc.Text(), s.scripts, d, parser.Language(d), s.Doc)
	tree.Errors = s.authored(tree.Errors)
	return tree
}

func (s Script) authored(errs []parser.Error) []parser.Error {
	written := func(e parser.Error) bool { return !s.Doc.Synthetic(e.Span.Start) }
	byScript := make([][]parser.Error, len(s.scripts))
	for _, e := range errs {
		i := min(sort.Search(len(s.scripts), func(i int) bool { return s.scripts[i].End > e.Span.Start }), len(s.scripts)-1)
		byScript[i] = append(byScript[i], e)
	}
	out := make([]parser.Error, 0, len(errs))
	for _, within := range byScript {
		if slices.ContainsFunc(within, written) {
			within = slices.DeleteFunc(within, func(e parser.Error) bool { return !written(e) })
		}
		out = append(out, within...)
	}
	return out
}
