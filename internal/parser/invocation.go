package parser

import (
	"iter"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type Invocation struct {
	Args          []*syntax.Word
	Placeholder   string
	InheritsStdin bool
}

const (
	findCommand        = "find"
	defaultPlaceholder = "{}"
	findBatchEnd       = "+"
	findCommandEnd     = ";"
)

var findExecActions = []string{"-exec", "-execdir", "-ok", "-okdir"}

func (w Wrappers) Runs(name string) bool {
	return w.Has(name) || (w.Programs && name == findCommand)
}

func (w Wrappers) Invocations(args []*syntax.Word) iter.Seq[Invocation] {
	return func(yield func(Invocation) bool) {
		w.invocations(args, Invocation{InheritsStdin: true}, yield)
	}
}

func (w Wrappers) invocations(args []*syntax.Word, run Invocation, yield func(Invocation) bool) bool {
	for i := 0; i < len(args); {
		if w.Programs && LiteralIs(args[i], findCommand) {
			return w.findInvocations(args[i+1:], run, yield)
		}
		spec, next, wrapped := w.step(args, i)
		if !wrapped {
			run.Args = args[i:]
			return yield(run)
		}
		placeholder, known := spec.placeholder(args[i+1 : next])
		if !known || !run.substitutes(placeholder) {
			return true
		}
		run.InheritsStdin = run.InheritsStdin && !spec.closesStdin
		i = next
	}
	return true
}

func (run *Invocation) substitutes(placeholder string) bool {
	switch {
	case placeholder == "" || placeholder == run.Placeholder:
		return true
	case run.Placeholder == "":
		run.Placeholder = placeholder
		return true
	}
	return false
}

func (w Wrappers) findInvocations(args []*syntax.Word, run Invocation, yield func(Invocation) bool) bool {
	if !run.substitutes(defaultPlaceholder) {
		return true
	}
	for i := 0; i < len(args); i++ {
		action, ok := Literal(args[i])
		if !ok || !slices.Contains(findExecActions, action) {
			continue
		}
		end, ok := findActionEnd(args, i+1)
		if !ok {
			return true
		}
		if !w.invocations(args[i+1:end], run, yield) {
			return false
		}
		i = end
	}
	return true
}

func findActionEnd(args []*syntax.Word, from int) (int, bool) {
	for i := from; i < len(args); i++ {
		if LiteralIs(args[i], findCommandEnd) || (LiteralIs(args[i], findBatchEnd) && i > from && LiteralIs(args[i-1], defaultPlaceholder)) {
			return i, i > from
		}
	}
	return 0, false
}

func (s wrapper) placeholder(options []*syntax.Word) (string, bool) {
	placeholder := ""
	for i := 0; i < len(options); i++ {
		option, ok := Literal(options[i])
		if !ok || option == getopt.EndOfOptions {
			continue
		}
		for _, name := range s.replace {
			value, attached := optionValue(option, name)
			switch {
			case attached:
				placeholder = value
			case option != name:
				continue
			case !slices.Contains(s.valued, name):
				placeholder = defaultPlaceholder
			case i+1 < len(options):
				i++
				if placeholder, ok = Literal(options[i]); !ok {
					return "", false
				}
			}
		}
	}
	return placeholder, true
}

func optionValue(option, name string) (string, bool) {
	if strings.HasPrefix(name, getopt.EndOfOptions) {
		return strings.CutPrefix(option, name+"=")
	}
	value, ok := strings.CutPrefix(option, name)
	return value, ok && value != ""
}
