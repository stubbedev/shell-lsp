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
	Make Language = iota
	Just
	Nix
)

func (l Language) String() string { return definitions[l].name }

type definition struct {
	name        string
	languageIDs []string
	names       []string
	extensions  []string
	project     func(text string) (*projection.Map, dialect.Detection)
}

var definitions = [...]definition{
	Make: {
		name:        "Makefile",
		languageIDs: []string{"makefile", "make"},
		names:       []string{"Makefile", "makefile", "GNUmakefile"},
		extensions:  []string{".mk", ".mak"},
		project:     makefile.Project,
	},
	Just: {
		name:        "justfile",
		languageIDs: []string{"just"},
		names:       []string{"justfile", "Justfile", "JUSTFILE", ".justfile"},
		extensions:  []string{".just"},
		project:     justfile.Project,
	},
	Nix: {
		name:        "Nix file",
		languageIDs: []string{"nix"},
		extensions:  []string{".nix"},
		project:     nix.Project,
	},
}

func find(match func(definition) bool) (Language, bool) {
	for l := range Language(len(definitions)) {
		if match(definitions[l]) {
			return l, true
		}
	}
	return 0, false
}

func Of(uri, languageID string) (Language, bool) {
	if languageID != "" {
		return find(func(d definition) bool { return slices.Contains(d.languageIDs, languageID) })
	}
	return FromName(uri)
}

func FromName(name string) (Language, bool) {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	return find(func(d definition) bool {
		return slices.Contains(d.names, base) || slices.ContainsFunc(d.extensions, func(ext string) bool { return strings.HasSuffix(base, ext) })
	})
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
	m, detection := definitions[l].project(doc.Text())
	return Script{Doc: docstore.Project(doc, m), Detection: detection, scripts: m.Scripts()}
}

func (s Script) Parse(like *parser.Tree) *parser.Tree {
	d := s.Detection.Dialect
	tree := parser.ParseScripts(like, s.Doc.Text(), s.scripts, d, d.Language(), s.Doc)
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
	for i, within := range byScript {
		switch {
		case slices.ContainsFunc(within, written):
			within = slices.DeleteFunc(within, func(e parser.Error) bool { return !written(e) })
		case len(within) > 0:
			if own := s.ownErrors(s.scripts[i]); len(own) > 0 {
				within = own
			}
		}
		out = append(out, within...)
	}
	return out
}

func (s Script) ownErrors(script parser.Span) []parser.Error {
	text := []byte(s.Doc.Text())
	for i := script.Start; i < script.End; i++ {
		if text[i] != '\n' && s.Doc.Synthetic(i) {
			text[i] = ' '
		}
	}
	d := s.Detection.Dialect
	return parser.ParseScripts(nil, string(text), []parser.Span{script}, d, d.Language(), s.Doc).Errors
}
