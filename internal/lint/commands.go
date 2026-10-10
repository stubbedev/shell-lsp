package lint

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

type command struct {
	call    *syntax.CallExpr
	name    string
	word    *syntax.Word
	args    []*syntax.Word
	builtin bool
}

func commandOf(call *syntax.CallExpr) (command, bool) {
	if len(call.Args) == 0 {
		return command{}, false
	}
	name, ok := parser.Literal(call.Args[0])
	if !ok {
		return command{}, false
	}
	c := command{call: call, name: name, word: call.Args[0], args: call.Args[1:]}
	if name == "builtin" && len(call.Args) > 1 {
		if inner, ok := parser.Literal(call.Args[1]); ok {
			c.name, c.word, c.args, c.builtin = inner, call.Args[1], call.Args[2:], true
		}
	}
	return c, true
}

func (c command) is(names ...string) bool {
	return !strings.Contains(c.name, "/") && slices.Contains(names, c.name)
}

func (c command) isBase(names ...string) bool {
	return !c.builtin && slices.Contains(names, path.Base(c.name))
}

var commandWrappers = map[string]string{
	"command": "",
	"builtin": "",
	"busybox": "",
	"exec":    "a",
	"time":    "fo",
}

func (p *pass) effectiveCommandWord(call *syntax.CallExpr) *syntax.Word {
	for i := 0; i < len(call.Args); {
		lit, ok := parser.Literal(call.Args[i])
		withArgument, wrapper := commandWrappers[lit]
		if lit == "run" && p.bats {
			wrapper = true
		}
		if !ok || !wrapper {
			return call.Args[i]
		}
		i++
		for i < len(call.Args) {
			opt, ok := parser.Literal(call.Args[i])
			if !ok || !strings.HasPrefix(opt, "-") || opt == "-" {
				break
			}
			i++
			if opt == "--" {
				break
			}
			if strings.ContainsAny(opt[1:], withArgument) {
				i++
			}
		}
	}
	return nil
}
