package hostnav

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/similar"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type Fix struct {
	Title     string
	Finding   lint.Finding
	Edits     []textedit.Edit
	Create    string
	Preferred bool
}

var unknownNames = map[lint.Code]model.Kind{
	lint.CodeJustUnknownRecipe:     model.KindRecipe,
	lint.CodeJustUnknownVariable:   model.KindVariable,
	lint.CodeJustUnknownFunction:   model.KindFunction,
	lint.CodeJustUnknownModule:     model.KindModule,
	lint.CodeJustUnknownSetting:    model.KindSetting,
	lint.CodeJustUnknownAttribute:  model.KindAttribute,
	lint.CodeMakeUndefinedVariable: model.KindVariable,
	lint.CodeMakeUnknownFunction:   model.KindFunction,
	lint.CodeMakePhonyWithoutRule:  model.KindTarget,
}

var stubbed = []lint.Code{lint.CodeJustUnknownRecipe, lint.CodeMakePhonyWithoutRule}

func (g *Group) Fixes(within parser.Span) []Fix {
	var out []Fix
	text := g.Self.Doc.Source().Text()
	for _, f := range g.Findings() {
		if f.Span.End < within.Start || f.Span.Start > within.End {
			continue
		}
		name := text[f.Span.Start:f.Span.End]
		if kind, ok := unknownNames[f.Code]; ok {
			if closest, found := similar.Closest(name, slices.Values(g.namesOf(kind, f.Span.Start))); found {
				out = append(out, Fix{Title: "Change to `" + closest + "`", Finding: f, Edits: []textedit.Edit{{Start: f.Span.Start, End: f.Span.End, NewText: closest}}, Preferred: true})
			}
		}
		switch {
		case slices.Contains(stubbed, f.Code) && g.Language.ValidName(unknownNames[f.Code], name):
			out = append(out, Fix{Title: "Add " + unknownNames[f.Code].String() + " `" + name + "`", Finding: f, Edits: []textedit.Edit{g.appended(name + ":\n")}})
		case f.Code == lint.CodeJustMissingFile || f.Code == lint.CodeMakeMissingInclude:
			if path, ok := g.creatable(f.Span); ok {
				out = append(out, Fix{Title: "Create `" + filepath.Base(path) + "`", Finding: f, Create: path})
			}
		case f.Code == lint.CodeMakeRecipePrefix && !g.setsRecipePrefix():
			start := strings.LastIndexByte(text[:f.Span.Start], '\n') + 1
			end := start + len(text[start:]) - len(strings.TrimLeft(text[start:], " \t"))
			out = append(out, Fix{Title: "Start the recipe line with a tab", Finding: f, Edits: []textedit.Edit{{Start: start, End: end, NewText: "\t"}}, Preferred: true})
		}
	}
	return out
}

func (g *Group) namesOf(kind model.Kind, at int) []string {
	ns := kind.Namespace()
	scope, _ := g.Index().ScopeAt(at)
	var out []string
	for f, ix := range g.Module.Files {
		for _, s := range ix.Symbols {
			if s.Kind.Namespace() == ns && (s.Scope == model.Global || f == g.self && s.Scope == scope) {
				out = append(out, s.Name)
			}
		}
	}
	for _, b := range g.Module.Builtins.Of(kind) {
		out = append(out, b.Name)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (g *Group) appended(text string) textedit.Edit {
	source := g.Self.Doc.Source().Text()
	prefix := "\n"
	if source != "" && !strings.HasSuffix(source, "\n") {
		prefix = "\n\n"
	}
	return textedit.Edit{Start: len(source), End: len(source), NewText: prefix + text}
}

func (g *Group) creatable(span parser.Span) (string, bool) {
	for _, inc := range g.missing {
		if inc.Span != span || inc.Dynamic || len(inc.Candidates) == 0 {
			continue
		}
		path := inc.Candidates[0]
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(g.Self.Path), path)
		}
		return path, true
	}
	return "", false
}

func (g *Group) setsRecipePrefix() bool {
	return slices.ContainsFunc(g.Index().Symbols, func(s model.Symbol) bool { return s.Name == ".RECIPEPREFIX" })
}
