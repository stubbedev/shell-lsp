package makefile

import (
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

var errorCodes = map[ErrorKind]lint.Code{
	ErrSyntax:            lint.CodeMakeSyntax,
	ErrConditional:       lint.CodeMakeConditional,
	ErrDefine:            lint.CodeMakeDefine,
	ErrRecipePrefix:      lint.CodeMakeRecipePrefix,
	ErrRecipeOutsideRule: lint.CodeMakeRecipeOutsideRule,
	ErrUnterminated:      lint.CodeMakeUnterminatedReference,
}

func (f *File) check() []lint.Finding {
	var out []lint.Finding
	for _, e := range f.Errors {
		out = append(out, lint.HostFinding(errorCodes[e.Kind], e.Span, e.Message))
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

func (f *File) duplicateRecipes() []lint.Finding {
	var out []lint.Finding
	first := map[string]Word{}
	for _, r := range f.Rules {
		if r.DoubleColon || len(r.Recipe) == 0 || len(r.Patterns) > 0 {
			continue
		}
		for _, t := range r.Targets {
			if !literal(t) {
				continue
			}
			if prior, ok := first[t.Text]; ok {
				out = append(out, lint.HostFinding(lint.CodeMakeDuplicateRecipe, t.Span, fmt.Sprintf("this recipe for `%s` replaces the one on line %d", t.Text, f.line(prior.Span.Start))))
				continue
			}
			first[t.Text] = t
		}
	}
	return out
}
