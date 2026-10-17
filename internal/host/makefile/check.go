package makefile

import (
	"fmt"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

var errorCodes = map[ErrorKind]lint.Code{
	ErrSyntax:            lint.CodeMakeSyntax,
	ErrConditional:       lint.CodeMakeConditional,
	ErrDefine:            lint.CodeMakeDefine,
	ErrRecipePrefix:      lint.CodeMakeRecipePrefix,
	ErrRecipeOutsideRule: lint.CodeMakeRecipeOutsideRule,
	ErrUnterminated:      lint.CodeMakeUnterminatedReference,

	ErrPatternUnterminated: lint.CodeMakeDeferredUnterminatedReference,
	ErrPatternMismatch:     lint.CodeMakePatternMismatch,
}

func (f *File) check() []lint.Finding {
	var out []lint.Finding
	for _, e := range f.Errors {
		if e.Kind != ErrDeferredUnterminated {
			out = append(out, lint.HostFinding(errorCodes[e.Kind], e.Span, e.Message))
		}
	}
	return append(out, f.duplicateRecipes()...)
}

func (f *File) line(offset int) int {
	n := 1
	for _, c := range f.Text[:offset] {
		if c == '\n' {
			n++
		}
	}
	return n
}

func (f *File) alternatives() model.Alternatives {
	var out model.Alternatives
	for _, c := range f.Conditionals {
		if len(c.Else) == 0 || c.Broken || c.End == nil {
			continue
		}
		directives := slices.Concat([]Directive{c.Open}, c.Else, []Directive{*c.End})
		arms := make(model.Branches, 0, len(directives)-1)
		for i, d := range directives[:len(directives)-1] {
			arms = append(arms, Span{Start: d.Span.End, End: directives[i+1].Span.Start})
		}
		out = append(out, arms)
	}
	return out
}

func (f *File) duplicateRecipes() []lint.Finding {
	var out []lint.Finding
	alternatives := f.alternatives()
	earlier := map[string][]Word{}
	for _, r := range f.Rules {
		if r.DoubleColon || len(r.Recipe) == 0 || len(r.Patterns) > 0 {
			continue
		}
		for _, t := range r.Targets {
			if !literal(t) {
				continue
			}
			prior := slices.IndexFunc(earlier[t.Text], func(w Word) bool { return !alternatives.Exclusive(w.Span.Start, t.Span.Start) })
			if prior >= 0 {
				out = append(out, lint.HostFinding(lint.CodeMakeDuplicateRecipe, t.Span, fmt.Sprintf("this recipe for `%s` replaces the one on line %d", t.Text, f.line(earlier[t.Text][prior].Span.Start))))
				continue
			}
			earlier[t.Text] = append(earlier[t.Text], t)
		}
	}
	return out
}
