package makefile

import (
	"fmt"
	"slices"
	"strings"

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
	replaced := f.overridden()
	for _, e := range f.Errors {
		switch e.Kind {
		case ErrDeferredUnterminated:
		case ErrUnterminated:
			if f.inReplacedRecipe(replaced, e.Span) {
				out = append(out, lint.HostFinding(lint.CodeMakeDeferredUnterminatedReference, e.Span, e.Message+"; a later recipe replaces this one, so make never expands it"))
				continue
			}
			out = append(out, lint.HostFinding(errorCodes[e.Kind], e.Span, e.Message))
		case ErrPatternUnterminated:
			if name, used := f.patternUsed(e.Span); used {
				out = append(out, lint.HostFinding(lint.CodeMakeUnterminatedReference, e.Span, strings.TrimSuffix(e.Message, patternUse)+fmt.Sprintf("; make uses this pattern rule for `%s`", name)))
				continue
			}
			out = append(out, lint.HostFinding(errorCodes[e.Kind], e.Span, e.Message))
		default:
			out = append(out, lint.HostFinding(errorCodes[e.Kind], e.Span, e.Message))
		}
	}
	return append(out, f.duplicateRecipes()...)
}

func (f *File) inReplacedRecipe(replaced map[*Rule]bool, at Span) bool {
	return slices.ContainsFunc(f.Rules, func(r *Rule) bool {
		return replaced[r] && slices.ContainsFunc(r.Recipe, func(l RecipeLine) bool { return l.Span.Start <= at.Start && at.Start <= l.Span.End })
	})
}

func (f *File) patternUsed(at Span) (string, bool) {
	i := slices.IndexFunc(f.Rules, func(r *Rule) bool { return r.Span.Start <= at.Start && at.End <= r.Span.End })
	if i < 0 || len(f.Rules[i].Prereqs) > 0 || len(f.Rules[i].OrderOnly) > 0 {
		return "", false
	}
	for _, name := range f.recipeless() {
		for _, t := range f.Rules[i].Targets {
			if !t.Expr.Dynamic() && model.MatchesRulePattern(t.Text, name) {
				return name, true
			}
		}
	}
	return "", false
}

func (f *File) recipeless() []string {
	var out []string
	for _, r := range f.Rules {
		if len(r.Recipe) > 0 || r.DoubleColon {
			continue
		}
		for _, t := range r.Targets {
			if _, special := specialTargets[t.Text]; !special && literal(t) {
				out = append(out, t.Text)
			}
		}
	}
	return out
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

type replacement struct {
	prior, later Word
	rule         *Rule
}

func (f *File) replacements() []replacement {
	type recipe struct {
		target Word
		rule   *Rule
	}
	var out []replacement
	alternatives := f.alternatives()
	earlier := map[string][]recipe{}
	for _, r := range f.Rules {
		if r.DoubleColon || len(r.Recipe) == 0 || len(r.Patterns) > 0 {
			continue
		}
		for _, t := range r.Targets {
			if !literal(t) {
				continue
			}
			prior := slices.IndexFunc(earlier[t.Text], func(e recipe) bool { return !alternatives.Exclusive(e.target.Span.Start, t.Span.Start) })
			if prior < 0 {
				earlier[t.Text] = append(earlier[t.Text], recipe{t, r})
				continue
			}
			out = append(out, replacement{prior: earlier[t.Text][prior].target, later: t, rule: earlier[t.Text][prior].rule})
			earlier[t.Text][prior] = recipe{t, r}
		}
	}
	return out
}

func (f *File) duplicateRecipes() []lint.Finding {
	var out []lint.Finding
	for _, r := range f.replacements() {
		out = append(out, lint.HostFinding(lint.CodeMakeDuplicateRecipe, r.later.Span, fmt.Sprintf("this recipe for `%s` replaces the one on line %d", r.later.Text, f.line(r.prior.Span.Start))))
	}
	return out
}

func (f *File) overridden() map[*Rule]bool {
	replaced := map[*Rule]int{}
	for _, r := range f.replacements() {
		replaced[r.rule]++
	}
	out := map[*Rule]bool{}
	for r, n := range replaced {
		out[r] = n == len(slices.DeleteFunc(slices.Clone(r.Targets), func(w Word) bool { return !literal(w) }))
	}
	return out
}
