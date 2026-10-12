package parser

import (
	"iter"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type wrapper struct {
	program     bool
	valued      []string
	operands    int
	assignments bool
	replace     []string
	closesStdin bool
	queries     string
}

var commandWrappers = map[string]wrapper{
	"command": {queries: "vV"},
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
	"setsid":  {program: true},
	"timeout": {program: true, operands: 1, valued: []string{"-k", "-s", "--kill-after", "--signal"}},
	"watch":   {program: true, valued: []string{"-n", "-q", "--interval", "--equexit"}},
	"xargs": {
		program: true, closesStdin: true, replace: []string{"-I", "-J", "-i", "--replace"},
		valued: []string{
			"-a", "-d", "-E", "-I", "-J", "-L", "-n", "-P", "-R", "-S", "-s",
			"--arg-file", "--delimiter", "--max-args", "--max-procs", "--max-chars", "--process-slot-var",
		},
	},
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

type TextLayer struct {
	Name    string
	Args    []string
	Wrap    Wrap
	Wrapped bool
}

func (l TextLayer) Own() []string {
	if !l.Wrapped {
		return l.Args
	}
	return l.Args[:l.Wrap.Command]
}

func (w Wrappers) TextLayers(name string, args []string) iter.Seq[TextLayer] {
	return func(yield func(TextLayer) bool) {
		for {
			wrap, wrapped := w.Unwrap(name, args)
			if !yield(TextLayer{Name: name, Args: args, Wrap: wrap, Wrapped: wrapped}) || !wrapped || wrap.Command == len(args) {
				return
			}
			name, args = args[wrap.Command], args[wrap.Command+1:]
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
	rest := args[i+1:]
	wrap := spec.skip(len(rest), func(j int) (string, bool) { return Literal(rest[j]) })
	if wrap.Query {
		return wrapper{}, i, false
	}
	return spec, i + 1 + wrap.Command, true
}

func (s wrapper) skip(n int, word func(int) (string, bool)) Wrap {
	operands, options, query := s.operands, true, false
	for i := 0; i < n; {
		lit, literal := word(i)
		switch {
		case literal && options && lit == getopt.EndOfOptions:
			options = false
			i++
		case literal && options && getopt.IsOption(lit):
			query = query || s.queriesWith(lit)
			words, _ := getopt.Words(lit, s.arity)
			if i += words; i > n {
				return Wrap{Command: n, Pending: true, Query: query}
			}
		case operands > 0:
			operands--
			options = false
			i++
		case literal && s.assignments && isAssignment(lit):
			options = false
			i++
		default:
			return Wrap{Command: i, Query: query}
		}
	}
	return Wrap{Command: n, Pending: operands > 0, Query: query}
}

func (s wrapper) queriesWith(option string) bool {
	return s.queries != "" && !strings.HasPrefix(option, "--") && strings.ContainsAny(option[1:], s.queries)
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
