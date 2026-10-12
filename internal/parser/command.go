package parser

import (
	"iter"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type wrapper struct {
	grammar     getopt.Grammar
	program     bool
	operands    int
	assignments bool
	replace     []string
	closesStdin bool
	queries     []string
}

var commandWrappers = map[string]wrapper{
	"command": {queries: []string{"-v", "-V"}},
	"builtin": {},
	"busybox": {},
	"exec":    {},
	"time":    {},
	"sudo":    {program: true},
	"doas":    {program: true},
	"env":     {program: true, assignments: true},
	"nice":    {program: true},
	"nohup":   {program: true},
	"setsid":  {program: true},
	"timeout": {program: true, operands: 1},
	"watch":   {program: true},
	"xargs":   {program: true, closesStdin: true, replace: []string{"-I", "-J", "-i", "--replace"}},
}

const batsRun = "run"

type Wrappers struct {
	Bats     bool
	Programs bool
}

type Wrap struct {
	Command int
	Pending bool
	Query   bool
}

func (w Wrappers) Has(name string) bool {
	_, ok := w.lookup(name)
	return ok
}

var batsRunGrammar = getopt.Grammar{
	Short:         "0123456789",
	Long:          map[string]getopt.Arity{"keep-empty-lines": getopt.Flag, "separate-stderr": getopt.Flag},
	StopAtOperand: true,
}

func (w Wrappers) lookup(name string) (wrapper, bool) {
	if w.Bats && name == batsRun {
		return wrapper{grammar: batsRunGrammar}, true
	}
	spec, ok := commandWrappers[name]
	spec.grammar = getopt.Commands[name]
	return spec, ok && (w.Programs || !spec.program)
}

func (w Wrappers) Unwrap(name string, args []string) (Wrap, bool) {
	spec, ok := w.lookup(name)
	if !ok {
		return Wrap{}, false
	}
	return skip(spec, args, verbatim), true
}

func verbatim(word string) (string, bool) { return word, true }

type Layer struct {
	Name   string
	Args   []string
	Offset int
	Wrap   Wrap
	Wraps  bool
}

func (l Layer) Own() []string {
	if !l.Wraps {
		return l.Args
	}
	return l.Args[:l.Wrap.Command]
}

func (w Wrappers) Layers(name string, args []string) iter.Seq[Layer] {
	return func(yield func(Layer) bool) {
		offset := 0
		for {
			wrap, wraps := w.Unwrap(name, args)
			if !yield(Layer{Name: name, Args: args, Offset: offset, Wrap: wrap, Wraps: wraps}) || !wraps || wrap.Command == len(args) {
				return
			}
			name, args, offset = args[wrap.Command], args[wrap.Command+1:], offset+wrap.Command+1
		}
	}
}

func (w Wrappers) Start(args []*syntax.Word) int {
	for i := 0; i < len(args); {
		_, next, ok := w.step(args, i)
		if !ok {
			return i
		}
		i = next
	}
	return -1
}

func (w Wrappers) step(args []*syntax.Word, i int) (wrapper, int, bool) {
	name, ok := Literal(args[i])
	if !ok {
		return wrapper{}, i, false
	}
	spec, ok := w.lookup(name)
	if !ok {
		return wrapper{}, i, false
	}
	wrap := skip(spec, args[i+1:], Literal)
	if wrap.Query {
		return wrapper{}, i, false
	}
	return spec, i + 1 + wrap.Command, true
}

func skip[W any](s wrapper, words []W, literal func(W) (string, bool)) Wrap {
	parsed := getopt.Parse(s.grammar, words, literal)
	query := parsed.Has(s.queries...)
	if parsed.Missing {
		return Wrap{Command: len(words), Pending: true, Query: query}
	}
	operands := s.operands
	for i := len(words) - len(parsed.Operands); i < len(words); i++ {
		lit, ok := literal(words[i])
		switch {
		case operands > 0:
			operands--
		case ok && s.assignments && isAssignment(lit):
		default:
			return Wrap{Command: i, Query: query}
		}
	}
	return Wrap{Command: len(words), Pending: operands > 0, Query: query}
}

func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	return ok && IsName(name)
}
