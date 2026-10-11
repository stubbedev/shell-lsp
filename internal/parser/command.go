package parser

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/getopt"
)

type wrapper struct {
	program     bool
	valued      []string
	operands    int
	assignments bool
}

var commandWrappers = map[string]wrapper{
	"command": {},
	"builtin": {},
	"busybox": {},
	"exec":    {valued: []string{"-a"}},
	"time":    {valued: []string{"-f", "-o", "--format", "--output"}},
	"sudo": {program: true, valued: []string{
		"-C", "-D", "-g", "-p", "-R", "-r", "-T", "-t", "-U", "-u",
		"--close-from", "--chdir", "--group", "--host", "--prompt", "--chroot", "--role", "--command-timeout", "--type", "--other-user", "--user",
	}},
	"doas":    {program: true, valued: []string{"-a", "-C", "-u"}},
	"env":     {program: true, assignments: true, valued: []string{"-a", "-C", "-P", "-S", "-u", "--argv0", "--chdir", "--split-string", "--unset"}},
	"nice":    {program: true, valued: []string{"-n", "--adjustment"}},
	"nohup":   {program: true},
	"timeout": {program: true, operands: 1, valued: []string{"-k", "-s", "--kill-after", "--signal"}},
	"watch":   {program: true, valued: []string{"-n", "-q", "--interval", "--equexit"}},
	"xargs": {program: true, valued: []string{
		"-a", "-d", "-E", "-I", "-J", "-L", "-n", "-P", "-R", "-S", "-s",
		"--arg-file", "--delimiter", "--max-args", "--max-procs", "--max-chars", "--process-slot-var",
	}},
}

const batsRun = "run"

type Wrappers struct {
	Bats     bool
	Programs bool
}

type Wrap struct {
	Command int
	Pending bool
}

func (w Wrappers) Has(name string) bool {
	_, ok := w.lookup(name)
	return ok
}

func (w Wrappers) lookup(name string) (wrapper, bool) {
	if w.Bats && name == batsRun {
		return wrapper{}, true
	}
	spec, ok := commandWrappers[name]
	return spec, ok && (w.Programs || !spec.program)
}

func (w Wrappers) Unwrap(name string, args []string) (Wrap, bool) {
	spec, ok := w.lookup(name)
	if !ok {
		return Wrap{}, false
	}
	return spec.skip(len(args), func(i int) (string, bool) { return args[i], true }), true
}

func (w Wrappers) Start(args []*syntax.Word) int {
	for i := 0; i < len(args); {
		name, ok := Literal(args[i])
		if !ok {
			return i
		}
		spec, ok := w.lookup(name)
		if !ok {
			return i
		}
		rest := args[i+1:]
		i += 1 + spec.skip(len(rest), func(j int) (string, bool) { return Literal(rest[j]) }).Command
	}
	return -1
}

func (s wrapper) skip(n int, word func(int) (string, bool)) Wrap {
	operands, options := s.operands, true
	for i := 0; i < n; {
		lit, literal := word(i)
		switch {
		case literal && options && lit == getopt.EndOfOptions:
			options = false
			i++
		case literal && options && getopt.IsOption(lit):
			words, _ := getopt.Words(lit, s.arity)
			if i += words; i > n {
				return Wrap{Command: n, Pending: true}
			}
		case operands > 0:
			operands--
			options = false
			i++
		case literal && s.assignments && isAssignment(lit):
			options = false
			i++
		default:
			return Wrap{Command: i}
		}
	}
	return Wrap{Command: n, Pending: operands > 0}
}

func (s wrapper) arity(name string) getopt.Arity {
	if slices.Contains(s.valued, name) {
		return getopt.Valued
	}
	return getopt.Flag
}

func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	return ok && IsName(name)
}
