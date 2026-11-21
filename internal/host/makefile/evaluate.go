package makefile

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

type value struct {
	text     string
	known    bool
	existing bool
}

func knownValue(text string) value { return value{text: text, known: true} }

func (v value) mayExist() bool {
	if v.known {
		return existingPaths(v.text)
	}
	return v.existing
}

func join(a, b value) value {
	if a.known && b.known {
		return knownValue(a.text + " " + b.text)
	}
	return value{existing: a.mayExist() && b.mayExist()}
}

func fileWords(s string) []string {
	return strings.FieldsFunc(strings.ReplaceAll(s, "\\\n", " "), func(r rune) bool { return strings.ContainsRune(whitespace+"\n", r) })
}

func existingPaths(s string) bool {
	return !slices.ContainsFunc(fileWords(s), func(w string) bool { return !existingPath(w) })
}

func existingPath(w string) bool {
	return strings.HasPrefix(w, "/") || strings.HasPrefix(w, "~") || !slices.ContainsFunc(strings.Split(w, "/"), func(part string) bool {
		return part != "" && part != "." && part != ".."
	})
}

func fileName(w string) string {
	for len(w) > 2 && strings.HasPrefix(w, "./") {
		rest := strings.TrimLeft(w[2:], "/")
		if rest == "" {
			return "./"
		}
		w = rest
	}
	return w
}

type expansion struct {
	name   string
	before int
}

func (p *reader) constant(n Node) (string, bool) {
	v := p.nodeValue(n, n.Span.Start, nil)
	return v.text, v.known
}

func (p *reader) constantText(e Expr) (string, bool) {
	v := p.exprValue(e, e.Span.Start, nil)
	return v.text, v.known
}

func (p *reader) nodeValue(n Node, before int, expanding []expansion) value {
	switch n.Kind {
	case NodeText:
		return knownValue(p.text[n.Span.Start:n.Span.End])
	case NodeSubstitution:
		return p.substitutionValue(n, before, expanding)
	case NodeCall:
		return p.callValue(n, before, expanding)
	case NodeReference:
		return p.referenceValue(n, before, expanding)
	}
	panic(fmt.Sprintf("makefile: node kind %d has no value", n.Kind))
}

func (p *reader) referenceValue(n Node, before int, expanding []expansion) value {
	name, ok := p.resolvedName(n.Name)
	inherited := slices.Contains(environmentVariables, name)
	if n.Unbalanced || !ok || n.Braced && strings.ContainsAny(name, "(){}") || defaulted(name) && !inherited {
		return value{}
	}
	self := expansion{name: name, before: before}
	if slices.Contains(expanding, self) {
		return value{}
	}
	expanding = append(slices.Clip(expanding), self)
	v, defined, immediate := knownValue(""), false, false
	if inherited {
		v = value{existing: true}
	}
	declared := func(before int) bool {
		return slices.ContainsFunc(p.file.Directives, func(d *Directive) bool {
			return d.Span.Start < before && slices.ContainsFunc(d.Names, func(w Word) bool { return w.Text == name })
		})
	}
	for _, a := range p.file.Assignments {
		if a.Name.Text != name || len(a.Targets) > 0 || a.Span.End > before {
			continue
		}
		defined = defined || declared(a.Span.Start)
		if a.Define {
			return value{}
		}
		switch {
		case a.Op == "?=" && slices.Contains(predefined, name):
			return value{}
		case a.Op == "?=" && inherited:
			v, defined = value{existing: v.mayExist() && p.assignedValue(a, false, before, expanding).mayExist()}, true
		case a.Op == "?=" && defined:
		case a.Op == "+=" && (defined || inherited):
			v, defined, inherited = join(v, p.assignedValue(a, immediate, before, expanding)), true, false
		case a.Op == "+=", a.Op == "?=", slices.Contains(simpleOps, a.Op):
			immediate = a.Op == ":=" || a.Op == "::="
			v, defined, inherited = p.assignedValue(a, immediate || a.Op == ":::=", before, expanding), true, false
		default:
			return value{}
		}
	}
	return v
}

func defaulted(name string) bool {
	_, builtin := Builtins.Lookup(model.KindVariable, name)
	return builtin && !automatic(name) && !slices.Contains(unsetByDefault, name)
}

func (p *reader) substitutionValue(n Node, before int, expanding []expansion) value {
	v := p.nodeValue(Node{Kind: NodeReference, Span: n.Span, Name: n.Name, Braced: n.Braced}, before, expanding)
	from := p.exprValue(n.Args[0], before, expanding)
	to := p.exprValue(n.Args[1], before, expanding)
	switch {
	case !from.known || !to.known || strings.ContainsRune(from.text, '\\') || strings.ContainsRune(to.text, '\\'):
		return value{}
	case !v.known:
		return value{existing: v.existing}
	}
	return knownValue(substituted(v.text, from.text, to.text))
}

func substituted(value, from, to string) string {
	if !strings.Contains(from, "%") {
		from, to = "%"+from, "%"+to
	}
	return patsubst(from, to, value)
}

func patsubst(pattern, replacement, value string) string {
	prefix, suffix, wild := strings.Cut(pattern, "%")
	head, tail, stemmed := strings.Cut(replacement, "%")
	words := fileWords(value)
	for i, w := range words {
		switch {
		case !wild:
			if w == pattern {
				words[i] = replacement
			}
		case len(w) < len(prefix)+len(suffix) || !strings.HasPrefix(w, prefix) || !strings.HasSuffix(w, suffix):
		case stemmed:
			words[i] = head + w[len(prefix):len(w)-len(suffix)] + tail
		default:
			words[i] = replacement
		}
	}
	return strings.Join(words, " ")
}

func (p *reader) assignedValue(a *Assignment, immediate bool, before int, expanding []expansion) value {
	if literal, ok := a.Value.Literal(p.text); ok {
		return knownValue(literal)
	}
	if immediate {
		before = a.Span.Start
	}
	return p.exprValue(a.Value, before, expanding)
}

func (p *reader) exprValue(e Expr, before int, expanding []expansion) value {
	segments := []string{""}
	for _, n := range e.Nodes {
		v := p.nodeValue(n, before, expanding)
		switch {
		case v.known:
			segments[len(segments)-1] += v.text
		case !v.existing:
			return value{}
		default:
			segments = append(segments, "")
		}
	}
	if len(segments) == 1 {
		return knownValue(segments[0])
	}
	return value{existing: separated(segments)}
}

func separated(segments []string) bool {
	blank := func(b byte) bool { return strings.IndexByte(whitespace+"\n", b) >= 0 }
	last := len(segments) - 1
	for i, s := range segments {
		switch {
		case s == "" && 0 < i && i < last:
			return false
		case s == "":
		case i > 0 && !blank(s[0]), i < last && !blank(s[len(s)-1]), !existingPaths(s):
			return false
		}
	}
	return true
}

type textFunction struct {
	args        int
	apply       func(args []string) (string, bool)
	keeps       bool
	directories bool
}

var foundFiles = []string{"abspath", "realpath", "wildcard"}

var textFunctions = map[string]textFunction{
	"addprefix": {args: 2, apply: func(a []string) (string, bool) {
		return eachWord(a[1], func(w string) string { return a[0] + w }), true
	}},
	"addsuffix": {args: 2, apply: func(a []string) (string, bool) {
		return eachWord(a[1], func(w string) string { return w + a[0] }), true
	}},
	"basename": {args: 1, apply: func(a []string) (string, bool) {
		return eachWord(a[0], func(w string) string { return w[:extension(w)] }), true
	}},
	"dir": {args: 1, directories: true, apply: func(a []string) (string, bool) {
		return eachWord(a[0], func(w string) string {
			if slash := strings.LastIndexByte(w, '/'); slash >= 0 {
				return w[:slash+1]
			}
			return "./"
		}), true
	}},
	"filter":     {args: 2, keeps: true, apply: func(a []string) (string, bool) { return filtered(a, true) }},
	"filter-out": {args: 2, keeps: true, apply: func(a []string) (string, bool) { return filtered(a, false) }},
	"firstword": {args: 1, keeps: true, apply: func(a []string) (string, bool) {
		words := fileWords(a[0])
		if len(words) == 0 {
			return "", true
		}
		return words[0], true
	}},
	"lastword": {args: 1, keeps: true, apply: func(a []string) (string, bool) {
		words := fileWords(a[0])
		if len(words) == 0 {
			return "", true
		}
		return words[len(words)-1], true
	}},
	"notdir": {args: 1, apply: func(a []string) (string, bool) {
		return eachWord(a[0], func(w string) string { return w[strings.LastIndexByte(w, '/')+1:] }), true
	}},
	"patsubst": {args: 3, apply: func(a []string) (string, bool) {
		return patsubst(a[0], a[1], a[2]), !strings.ContainsRune(a[0], '\\') && !strings.ContainsRune(a[1], '\\')
	}},
	"sort": {args: 1, keeps: true, apply: func(a []string) (string, bool) {
		words := fileWords(a[0])
		slices.Sort(words)
		return strings.Join(slices.Compact(words), " "), true
	}},
	"strip": {args: 1, keeps: true, apply: func(a []string) (string, bool) {
		return strings.Join(fileWords(a[0]), " "), true
	}},
	"subst": {args: 3, apply: func(a []string) (string, bool) {
		return strings.ReplaceAll(a[2], a[0], a[1]), a[0] != ""
	}},
	"suffix": {args: 1, apply: func(a []string) (string, bool) {
		var suffixes []string
		for _, w := range fileWords(a[0]) {
			if dot := extension(w); dot < len(w) {
				suffixes = append(suffixes, w[dot:])
			}
		}
		return strings.Join(suffixes, " "), true
	}},
	"words": {args: 1, apply: func(a []string) (string, bool) {
		return strconv.Itoa(len(fileWords(a[0]))), true
	}},
}

func eachWord(list string, f func(string) string) string {
	words := fileWords(list)
	for i, w := range words {
		words[i] = f(w)
	}
	return strings.Join(words, " ")
}

func extension(w string) int {
	if dot := strings.LastIndexByte(w, '.'); dot > strings.LastIndexByte(w, '/') {
		return dot
	}
	return len(w)
}

func filtered(a []string, keep bool) (string, bool) {
	if strings.ContainsRune(a[0], '\\') {
		return "", false
	}
	patterns := fileWords(a[0])
	words := slices.DeleteFunc(fileWords(a[1]), func(w string) bool {
		return slices.ContainsFunc(patterns, func(pattern string) bool { return model.MatchesPattern(pattern, w) }) != keep
	})
	return strings.Join(words, " "), true
}

func (p *reader) callValue(n Node, before int, expanding []expansion) value {
	name, _ := n.Name.Literal(p.text)
	if slices.Contains(foundFiles, name) {
		return value{existing: true}
	}
	fn, ok := textFunctions[name]
	if !ok || len(n.Args) != fn.args {
		return value{}
	}
	args := make([]string, len(n.Args))
	for i, e := range n.Args {
		v := p.exprValue(e, before, expanding)
		switch {
		case v.known:
			args[i] = v.text
		case fn.directories && i == len(n.Args)-1:
			return value{existing: true}
		case fn.keeps && i == len(n.Args)-1:
			return value{existing: v.existing}
		default:
			return value{}
		}
	}
	text, ok := fn.apply(args)
	if !ok {
		return value{}
	}
	return knownValue(text)
}
