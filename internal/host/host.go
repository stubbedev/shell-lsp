package host

import (
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
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
	Docker
)

func (l Language) String() string { return definitions[l].name }

type definition struct {
	name        string
	languageIDs []string
	names       []string
	extensions  []string
	prefixes    []string
	companions  []string
	analyze     func(text string) analysis
	builtins    model.Builtins
	checks      model.Checks
	validName   func(kind model.Kind, name string) bool
	fence       string
	invocation  func(path, name string) []string
	recursion   Recursion
	formatter   func(text string) (string, error)
	shellOnly   bool
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
	dockerfile *dockerfile.File
}

func analyzeNix(text string) analysis {
	a := nix.Analyze(text)
	return analysis{projection: a.Map, detection: a.Detection, index: a.Index, enclosing: a.Enclosing}
}

func analyzeMake(text string) analysis {
	a := makefile.Analyze(text)
	return analysis{projection: a.Map, detection: a.Detection, index: a.Index, expect: a.File.Expect, invocation: a.File.Invocation, enclosing: a.File.Enclosing, format: func() (string, error) { return makefile.Format(a.File) }}
}

func analyzeDocker(text string) analysis {
	a := dockerfile.Analyze(text)
	return analysis{projection: a.Map, detection: a.Detection, index: a.Index, expect: a.File.Expect, invocation: a.File.Invocation, enclosing: a.File.Enclosing, dockerfile: a.File}
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
		analyze:     analyzeNix,
		checks:      nix.Checks,
		fence:       "nix",
		shellOnly:   true,
	},
	Docker: {
		name:        "Dockerfile",
		languageIDs: []string{"dockerfile"},
		names:       []string{"Dockerfile", "Containerfile"},
		extensions:  []string{".Dockerfile", ".dockerfile", ".Containerfile"},
		prefixes:    []string{"Dockerfile.", "Containerfile."},
		companions:  []string{".dockerignore"},
		analyze:     analyzeDocker,
		builtins:    dockerfile.Builtins,
		checks:      dockerfile.Checks,
		validName:   dockerfile.ValidName,
		fence:       "dockerfile",
		invocation: func(path, name string) []string {
			return []string{"docker", "build", "--file", path, "--target", name, filepath.Dir(path)}
		},
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
		if slices.ContainsFunc(d.companions, func(ext string) bool { return strings.HasSuffix(base, ext) }) {
			return false
		}
		return slices.Contains(d.names, base) || slices.ContainsFunc(d.extensions, func(ext string) bool { return strings.HasSuffix(base, ext) }) ||
			slices.ContainsFunc(d.prefixes, func(prefix string) bool { return strings.HasPrefix(base, prefix) })
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
	Variants  []Variant
}

type Variant struct {
	Detection dialect.Detection
	scripts   []parser.Span
}

type Native struct {
	Language   Language
	Index      *model.Index
	expect     func(offset int) (model.Expect, bool)
	invocation func(offset int) (model.Invocation, bool)
	enclosing  func(offset int) []parser.Span
	format     func() (string, error)
	dockerfile *dockerfile.File
}

func (n *Native) Dockerfile() (*dockerfile.File, bool) { return n.dockerfile, n.dockerfile != nil }

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

func (l Language) ShellOnly() bool { return definitions[l].shellOnly }

func (l Language) Renames() bool { return definitions[l].validName != nil }

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
	script := Script{Doc: docstore.Project(doc, a.projection), Detection: a.detection}
	script.group(a.projection.Scripts(), a.projection.Shells())
	if a.index != nil {
		script.Native = &Native{Language: l, Index: a.index, expect: a.expect, invocation: a.invocation, enclosing: a.enclosing, format: a.format, dockerfile: a.dockerfile}
	}
	return script
}

func (s *Script) group(scripts []parser.Span, shells []dialect.Detection) {
	primary := slices.ContainsFunc(shells, func(d dialect.Detection) bool { return d.Shell == s.Detection.Shell })
	for i, span := range scripts {
		d := shells[i]
		if !primary {
			s.Detection, primary = d, true
		}
		if d.Shell == s.Detection.Shell {
			s.scripts = append(s.scripts, span)
			continue
		}
		v := slices.IndexFunc(s.Variants, func(v Variant) bool { return v.Detection.Shell == d.Shell })
		if v < 0 {
			s.Variants = append(s.Variants, Variant{Detection: d})
			v = len(s.Variants) - 1
		}
		s.Variants[v].scripts = append(s.Variants[v].scripts, span)
	}
}

func (s Script) Spans() []parser.Span { return s.scripts }

func (v Variant) Spans() []parser.Span { return v.scripts }

func (s Script) Parse(like *parser.Tree) *parser.Tree {
	return s.parse(like, s.scripts, s.Detection.Shell)
}

func (s Script) ParseVariant(v Variant, like *parser.Tree) *parser.Tree {
	return s.parse(like, v.scripts, v.Detection.Shell)
}

func (s Script) parse(like *parser.Tree, scripts []parser.Span, shell dialect.Shell) *parser.Tree {
	tree := parser.ParseScripts(like, s.Doc.Text(), scripts, shell, s.Doc)
	tree.Errors = s.authored(scripts, shell, tree.Errors)
	return tree
}

func (s Script) authored(scripts []parser.Span, shell dialect.Shell, errs []parser.Error) []parser.Error {
	written := func(e parser.Error) bool { return !s.Doc.Synthetic(e.Span.Start) }
	byScript := make([][]parser.Error, len(scripts))
	for _, e := range errs {
		i := min(sort.Search(len(scripts), func(i int) bool { return scripts[i].End > e.Span.Start }), len(scripts)-1)
		byScript[i] = append(byScript[i], e)
	}
	out := make([]parser.Error, 0, len(errs))
	for i, within := range byScript {
		switch {
		case slices.ContainsFunc(within, written):
			within = slices.DeleteFunc(within, func(e parser.Error) bool { return !written(e) })
		case len(within) > 0:
			if own := s.ownErrors(scripts[i], shell); len(own) > 0 {
				within = own
			}
		}
		out = append(out, within...)
	}
	return out
}

func (s Script) ownErrors(script parser.Span, shell dialect.Shell) []parser.Error {
	text := []byte(s.Doc.Text())
	for i := script.Start; i < script.End; i++ {
		if text[i] != '\n' && s.Doc.Synthetic(i) {
			text[i] = ' '
		}
	}
	return parser.ParseScripts(nil, string(text), []parser.Span{script}, shell, s.Doc).Errors
}
