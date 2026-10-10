package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

type shellOptions struct {
	errexit        bool
	pipefail       bool
	inheritErrexit bool
	lastpipe       bool
	execfail       bool
}

func (p *pass) options() shellOptions {
	if p.shellOptions == nil {
		opts := scanShellOptions(p.tree)
		p.shellOptions = &opts
	}
	return *p.shellOptions
}

func scanShellOptions(tree *parser.Tree) shellOptions {
	opts := shellOptions{errexit: shebangErrexit(tree.Text())}
	for _, c := range tree.Commands {
		if c.Name == "set" || c.Name == "shopt" {
			opts.lastpipe = opts.lastpipe || mentions(c.Args, "lastpipe")
			opts.execfail = opts.execfail || mentions(c.Args, "execfail")
		}
		switch c.Name {
		case "set":
			opts.errexit = opts.errexit || setsErrexit(c.Args)
			if setsOptionFlag(c.Args) || mentions(c.Args, "pipefail") {
				opts.pipefail = true
			}
			opts.inheritErrexit = opts.inheritErrexit || setsOptionFlag(c.Args)
		case "shopt":
			opts.pipefail = opts.pipefail || mentions(c.Args, "pipefail")
			opts.inheritErrexit = opts.inheritErrexit || mentions(c.Args, "inherit_errexit")
		}
	}
	return opts
}

func shebangErrexit(text string) bool {
	first, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(first, "#!") {
		return false
	}
	for _, field := range strings.Fields(first)[1:] {
		if strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "--") && strings.Contains(field, "e") {
			return true
		}
	}
	return false
}

func setsErrexit(args []*syntax.Word) bool {
	return mentions(args, "errexit") || hasShortFlag(args, 'e')
}

func setsOptionFlag(args []*syntax.Word) bool { return hasShortFlag(args, 'o') }

func hasShortFlag(args []*syntax.Word, flag byte) bool {
	for _, a := range args {
		lit, ok := parser.Literal(a)
		if ok && strings.HasPrefix(lit, "-") && !strings.HasPrefix(lit, "--") && strings.IndexByte(lit, flag) > 0 {
			return true
		}
	}
	return false
}

func mentions(args []*syntax.Word, word string) bool {
	for _, a := range args {
		if lit, ok := parser.Literal(a); ok && lit == word {
			return true
		}
	}
	return false
}
