package makefile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

var (
	referenceKinds = []model.Kind{model.KindVariable, model.KindFunction}
	targetKinds    = []model.Kind{model.KindTarget}
	variableKinds  = []model.Kind{model.KindVariable}
	directiveKinds = []model.Kind{model.KindKeyword}
)

func isNameByte(c byte) bool {
	return c == '_' || c == '-' || c == '.' || c == '/' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (f *File) wordAt(offset int) Span {
	start := offset
	for start > 0 && isNameByte(f.Text[start-1]) && f.Text[start-1] != '/' {
		start--
	}
	end := offset
	for end < len(f.Text) && isNameByte(f.Text[end]) {
		end++
	}
	return Span{Start: start, End: end}
}

func (f *File) Expect(offset int) (model.Expect, bool) {
	offset = min(offset, len(f.Text))
	word := f.wordAt(offset)
	expect := func(kinds []model.Kind) (model.Expect, bool) {
		return model.Expect{Kinds: kinds, Span: word, Scope: model.Global}, true
	}
	if slices.ContainsFunc(f.Comments, func(c Span) bool { return c.Start < offset && offset <= c.End }) {
		return model.Expect{}, false
	}
	for _, e := range f.exprs() {
		if n, ok := innermost(e, offset); ok {
			if n.Kind == NodeCall && !n.Name.Span.Touches(offset) {
				return model.Expect{}, false
			}
			return expect(referenceKinds)
		}
	}
	if f.inRecipe(offset) {
		return model.Expect{}, false
	}
	line := textline.Start(f.Text, offset)
	for _, d := range f.namingDirectives() {
		if d.Keyword.Span.End < word.Start && textline.Start(f.Text, d.Keyword.Span.Start) == line {
			return expect(variableKinds)
		}
	}
	for _, r := range f.Rules {
		if r.Colon < word.Start && word.Start <= r.Header.End {
			return expect(targetKinds)
		}
	}
	if strings.TrimSpace(f.Text[line:word.Start]) == "" {
		return expect(directiveKinds)
	}
	return model.Expect{}, false
}

func innermost(e Expr, offset int) (Node, bool) {
	for _, n := range e.Nodes {
		if n.Kind == NodeText || offset <= n.Span.Start+1 || offset > n.Span.End || offset == n.Span.End && closed(n) {
			continue
		}
		for _, sub := range append([]Expr{n.Name}, n.Args...) {
			if inner, ok := innermost(sub, offset); ok {
				return inner, true
			}
		}
		return n, true
	}
	return Node{}, false
}

func closed(n Node) bool {
	if len(n.Args) > 0 {
		return n.Args[len(n.Args)-1].Span.End < n.Span.End
	}
	return n.Name.Span.End < n.Span.End
}

func (f *File) exprs() []Expr {
	var out []Expr
	words := func(ws []Word) {
		for _, w := range ws {
			out = append(out, w.Expr)
		}
	}
	for _, a := range f.Assignments {
		out = append(out, a.Name.Expr, a.Value)
		words(a.Targets)
	}
	for _, r := range f.Rules {
		words(slices.Concat(r.Targets, r.Patterns, r.Prereqs, r.OrderOnly))
		for _, l := range r.Recipe {
			out = append(out, l.Expr)
		}
	}
	for _, inc := range f.Includes {
		words(inc.Files)
	}
	for _, d := range f.Directives {
		out = append(out, d.Args)
		words(d.Names)
	}
	for _, c := range f.Conditionals {
		for _, d := range slices.Concat([]Directive{c.Open}, c.Else) {
			out = append(out, d.Args)
			words(d.Names)
		}
	}
	return append(out, f.Expressions...)
}

func (f *File) inRecipe(offset int) bool {
	for _, r := range f.Rules {
		for _, l := range r.Recipe {
			if l.Span.Start <= offset && offset <= l.Span.End {
				return true
			}
		}
	}
	return false
}

var namingKeywords = []string{"ifdef", "ifndef", "export", "unexport", "undefine"}

func (f *File) namingDirectives() []Directive {
	var out []Directive
	for _, d := range f.Directives {
		if slices.Contains(namingKeywords, d.Keyword.Text) {
			out = append(out, *d)
		}
	}
	for _, c := range f.Conditionals {
		for _, d := range slices.Concat([]Directive{c.Open}, c.Else) {
			if slices.Contains(namingKeywords, d.Keyword.Text) {
				out = append(out, d)
			}
		}
	}
	return out
}

func ValidName(kind model.Kind, name string) bool {
	if name == "" || strings.ContainsAny(name, " \t\r\n") {
		return false
	}
	if kind == model.KindTarget {
		f := Parse(name + ":\n")
		return len(f.Errors) == 0 && len(f.Rules) == 1 && len(f.Rules[0].Targets) == 1 && literal(f.Rules[0].Targets[0]) && f.Rules[0].Targets[0].Text == name && len(f.Rules[0].Prereqs) == 0
	}
	f := Parse(name + " = x\n")
	return len(f.Errors) == 0 && len(f.Assignments) == 1 && f.Assignments[0].Op == "=" && !f.Assignments[0].Name.Expr.Dynamic() && f.Assignments[0].Name.Text == name
}
