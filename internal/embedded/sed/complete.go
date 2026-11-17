package sed

import (
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
)

type Context uint8

const (
	Commands Context = iota + 1
	Operators
	Labels
)

type offer struct {
	context Context
	start   int
}

type Candidate struct {
	Label string
	Doc   lang.Doc
}

type Completion struct {
	Context    Context
	Start      int
	Candidates []Candidate
}

func Complete(src string, syntax Syntax, at int) (Completion, bool) {
	p := newParser(src[:at], syntax)
	lang.Run(p.run)
	if p.offered.context == 0 {
		return Completion{}, false
	}
	var all []Candidate
	switch p.offered.context {
	case Commands:
		all = documented(commands)
	case Operators:
		all = operatorsOf(syntax)
	case Labels:
		all = labelsIn(src, syntax)
	}
	prefix := src[p.offered.start:at]
	candidates := slices.DeleteFunc(all, func(c Candidate) bool { return !strings.HasPrefix(c.Label, prefix) })
	return Completion{Context: p.offered.context, Start: p.offered.start, Candidates: candidates}, true
}

func documented(docs lang.Docs) []Candidate {
	out := make([]Candidate, 0, len(docs))
	for _, name := range slices.Sorted(maps.Keys(docs)) {
		out = append(out, Candidate{Label: name, Doc: docs[name]})
	}
	return out
}

func labelsIn(src string, syntax Syntax) []Candidate {
	p := newParser(src, syntax)
	lang.Run(p.run)
	names := make([]string, 0, len(p.marks))
	for _, m := range p.marks {
		names = append(names, m.name)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	out := make([]Candidate, len(names))
	for i, name := range names {
		out[i] = Candidate{Label: name, Doc: lang.Doc{Signature: ":" + name}}
	}
	return out
}
