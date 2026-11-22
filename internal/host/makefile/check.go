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

func (f *File) check(replacements []replacement, replaced map[*Rule]bool) []lint.Finding {
	var out []lint.Finding
	for _, e := range f.Errors {
		switch e.Kind {
		case ErrDeferredUnterminated:
		case ErrUnterminated:
			switch r := f.recipeRule(e.Span); {
			case r != nil && replaced[r]:
				out = append(out, lint.HostFinding(lint.CodeMakeDeferredUnterminatedReference, e.Span, e.Message+"; a later recipe replaces this one, so make never expands it"))
			case r != nil && r.MayBeUpToDate:
				out = append(out, lint.HostFinding(lint.CodeMakeDeferredUnterminatedReference, e.Span, e.Message+"; every target of this rule may already exist, so make may never run this recipe"))
			default:
				out = append(out, lint.HostFinding(errorCodes[e.Kind], e.Span, e.Message))
			}
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
	return append(out, f.duplicateRecipes(replacements)...)
}

func (f *File) recipeRule(at Span) *Rule {
	i := slices.IndexFunc(f.Rules, func(r *Rule) bool {
		return slices.ContainsFunc(r.Recipe, func(l RecipeLine) bool { return l.Span.Start <= at.Start && at.Start <= l.Span.End })
	})
	if i < 0 {
		return nil
	}
	return f.Rules[i]
}

func (f *File) patternUsed(at Span) (string, bool) {
	i := slices.IndexFunc(f.Rules, func(r *Rule) bool { return r.Span.Start <= at.Start && at.End <= r.Span.End })
	if i < 0 {
		return "", false
	}
	return f.usedPattern(i)
}

func (f *File) usedPatterns() map[*Rule]bool {
	out := map[*Rule]bool{}
	for i, r := range f.Rules {
		if _, used := f.usedPattern(i); used {
			out[r] = true
		}
	}
	return out
}

func (f *File) usedPattern(i int) (string, bool) {
	if !f.Rules[i].Pattern() || len(f.Rules[i].Prereqs) > 0 || len(f.Rules[i].OrderOnly) > 0 || f.redefinedPattern(i) {
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

func (f *File) redefinedPattern(i int) bool {
	texts := func(ws []Word) []string {
		out := make([]string, len(ws))
		for k, w := range ws {
			out[k] = w.Text
		}
		return out
	}
	r := f.Rules[i]
	return slices.ContainsFunc(f.Rules[i+1:], func(later *Rule) bool {
		return later.Pattern() && slices.Equal(texts(later.Targets), texts(r.Targets)) && slices.Equal(texts(later.Prereqs), texts(r.Prereqs))
	})
}

func (f *File) recipeless() []string {
	var out []string
	for _, r := range f.Rules {
		if len(r.Recipe) > 0 || r.DoubleColon {
			continue
		}
		for _, t := range r.Targets {
			if !strings.HasPrefix(t.Text, ".") && literal(t) {
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
	prior, later namedFile
	rule         *Rule
}

func (f *File) replacements() []replacement {
	type recipe struct {
		target namedFile
		rule   *Rule
	}
	var out []replacement
	alternatives := f.alternatives()
	evaluator := f.evaluator()
	earlier := map[string][]recipe{}
	for _, r := range f.Rules {
		if r.DoubleColon || len(r.Recipe) == 0 {
			continue
		}
		named, _ := evaluator.namedFiles(r.Targets)
		for _, n := range named {
			prior := slices.IndexFunc(earlier[n.name], func(e recipe) bool { return !alternatives.Exclusive(e.target.word.Span.Start, n.word.Span.Start) })
			if prior < 0 {
				earlier[n.name] = append(earlier[n.name], recipe{n, r})
				continue
			}
			out = append(out, replacement{prior: earlier[n.name][prior].target, later: n, rule: earlier[n.name][prior].rule})
			earlier[n.name][prior] = recipe{n, r}
		}
	}
	return out
}

func (f *File) duplicateRecipes(replacements []replacement) []lint.Finding {
	out := make([]lint.Finding, 0, len(replacements))
	for _, r := range replacements {
		if r.prior.assumed || r.later.assumed {
			continue
		}
		out = append(out, lint.HostFinding(lint.CodeMakeDuplicateRecipe, r.later.word.Span, fmt.Sprintf("this recipe for `%s` replaces the one on line %d", r.later.name, f.line(r.prior.word.Span.Start))))
	}
	return out
}

func (f *File) overridden(replacements []replacement) map[*Rule]bool {
	replaced := map[*Rule]int{}
	for _, r := range replacements {
		replaced[r.rule]++
	}
	evaluator := f.evaluator()
	out := make(map[*Rule]bool, len(replaced))
	for r, n := range replaced {
		named, known := evaluator.namedFiles(r.Targets)
		out[r] = known && n == len(named)
	}
	return out
}
