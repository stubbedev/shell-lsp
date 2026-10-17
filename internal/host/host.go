package host

import (
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/justfile"
	"github.com/stubbedev/shell-lsp/internal/host/makefile"
	"github.com/stubbedev/shell-lsp/internal/host/model"
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
	analyze     func(text string) analysis
	builtins    model.Builtins
	checks      model.Checks
	validName   func(kind model.Kind, name string) bool
	fence       string
	invocation  func(path, name string) []string
	recursion   Recursion
	formatter   func(text string) (string, error)
}

type Recursion struct {
	Command   string
	Ref       model.Ref
	FileFlags []string
	Kind      model.Kind
	Separator string
}

type analysis struct {
	projection *projection.Map
	detection  dialect.Detection
	index      *model.Index
	expect     func(offset int) (model.Expect, bool)
	invocation func(offset int) (model.Invocation, bool)
	enclosing  func(offset int) []parser.Span
	format     func() (string, error)
}

func projectOnly(project func(string) (*projection.Map, dialect.Detection)) func(string) analysis {
	return func(text string) analysis {
		m, detection := project(text)
		return analysis{projection: m, detection: detection}
	}
}

func analyzeMake(text string) analysis {
	a := makefile.Analyze(text)
	return analysis{projection: a.Map, detection: a.Detection, index: a.Index, expect: a.File.Expect, invocation: a.File.Invocation, enclosing: a.File.Enclosing, format: func() (string, error) { return makefile.Format(a.File) }}
}

func analyzeJust(text string) analysis {
	a := justfile.Analyze(text)
	return analysis{projection: a.Map, detection: a.Detection, index: a.Index, expect: a.File.Expect, invocation: a.File.Invocation, enclosing: a.File.Enclosing, format: func() (string, error) { return justfile.Format(a.File) }}
}

var definitions = [...]definition{
	Make: {
		name:        "Makefile",
		languageIDs: []string{"makefile", "make"},
		names:       []string{"Makefile", "makefile", "GNUmakefile"},
		extensions:  []string{".mk", ".mak"},
		analyze:     analyzeMake,
		builtins:    makefile.Builtins,
		checks:      makefile.Checks,
		validName:   makefile.ValidName,
		fence:       "make",
		invocation: func(path, name string) []string {
			return []string{"make", "-f", path, name}
		},
		formatter: func(text string) (string, error) { return makefile.Format(makefile.Parse(text)) },
		recursion: Recursion{Command: "make", Ref: model.Ref{Name: "MAKE", Kind: model.KindVariable}, FileFlags: []string{"-f", "--file", "--makefile", "-C", "--directory"}, Kind: model.KindTarget},
	},
	Just: {
		name:        "justfile",
		languageIDs: []string{"just"},
		names:       []string{"justfile", "Justfile", "JUSTFILE", ".justfile"},
		extensions:  []string{".just"},
		analyze:     analyzeJust,
		builtins:    justfile.Builtins,
		checks:      justfile.Checks,
		validName:   justfile.ValidName,
		fence:       "just",
		invocation: func(path, name string) []string {
			return []string{"just", "--justfile", path, "--working-directory", filepath.Dir(path), name}
		},
		formatter: func(text string) (string, error) { return justfile.Format(justfile.Parse(text)) },
		recursion: Recursion{Command: "just", Ref: model.Ref{Name: "just_executable", Kind: model.KindFunction}, FileFlags: []string{"-f", "--justfile", "-d", "--working-directory"}, Kind: model.KindRecipe, Separator: "::"},
	},
	Nix: {
		name:        "Nix file",
		languageIDs: []string{"nix"},
		extensions:  []string{".nix"},
		analyze:     projectOnly(nix.Project),
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
	if l, ok := FromName(uri); ok {
		return l, true
	}
	return LanguageOf(languageID)
}

func LanguageOf(languageID string) (Language, bool) {
	return find(func(d definition) bool { return slices.Contains(d.languageIDs, languageID) })
}

func (l Language) EntryPoint(name string) bool {
	return slices.Contains(definitions[l].names, baseName(name))
}

func (l Language) LanguageID() string { return definitions[l].languageIDs[0] }

func baseName(name string) string { return path.Base(strings.ReplaceAll(name, "\\", "/")) }

func FromName(name string) (Language, bool) {
	base := baseName(name)
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
	Native    *Native
	scripts   []parser.Span
}

type Native struct {
	Language   Language
	Index      *model.Index
	expect     func(offset int) (model.Expect, bool)
	invocation func(offset int) (model.Invocation, bool)
	enclosing  func(offset int) []parser.Span
	format     func() (string, error)
}

func (n *Native) Enclosing(offset int) []parser.Span { return n.enclosing(offset) }

func (n *Native) Expect(offset int) (model.Expect, bool) { return n.expect(offset) }

func (n *Native) Invocation(offset int) (model.Invocation, bool) { return n.invocation(offset) }

func (n *Native) Formats() bool { return n.format != nil }

func (n *Native) Format() (string, error) {
	if n.format == nil {
		panic("host: " + n.Language.String() + " has no formatter")
	}
	return n.format()
}

func (l Language) Builtins() model.Builtins { return definitions[l].builtins }

func (l Language) Checks() model.Checks { return definitions[l].checks }

func (l Language) ValidName(kind model.Kind, name string) bool {
	return definitions[l].validName(kind, name)
}

func (l Language) Fence() string { return definitions[l].fence }

func (l Language) Invocation(path, name string) []string {
	return definitions[l].invocation(path, name)
}

func (l Language) Recursion() Recursion { return definitions[l].recursion }

func (l Language) Formats() bool { return definitions[l].formatter != nil }

func (l Language) Format(text string) (string, error) {
	if !l.Formats() {
		panic("host: " + l.String() + " has no formatter")
	}
	return definitions[l].formatter(text)
}

func (l Language) Project(doc *docstore.Document) Script {
	a := definitions[l].analyze(doc.Text())
	script := Script{Doc: docstore.Project(doc, a.projection), Detection: a.detection, scripts: a.projection.Scripts()}
	if a.index != nil {
		script.Native = &Native{Language: l, Index: a.index, expect: a.expect, invocation: a.invocation, enclosing: a.enclosing, format: a.format}
	}
	return script
}

func (s Script) Parse(like *parser.Tree) *parser.Tree {
	tree := parser.ParseScripts(like, s.Doc.Text(), s.scripts, s.Detection.Shell, s.Doc)
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
	return parser.ParseScripts(nil, string(text), []parser.Span{script}, s.Detection.Shell, s.Doc).Errors
}
