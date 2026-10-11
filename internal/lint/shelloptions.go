package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
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

type StrictMode struct {
	Errexit  bool
	Nounset  bool
	Pipefail bool
}

func StrictModeOf(tree *parser.Tree) StrictMode {
	text := tree.Text()
	m := StrictMode{Errexit: shebangFlag(text, 'e'), Nounset: shebangFlag(text, 'u')}
	for _, c := range tree.Commands {
		if m.Errexit && m.Nounset && m.Pipefail {
			break
		}
		set := StrictModeSetBy(c)
		m = StrictMode{Errexit: m.Errexit || set.Errexit, Nounset: m.Nounset || set.Nounset, Pipefail: m.Pipefail || set.Pipefail}
	}
	return m
}

func StrictModeSetBy(c parser.Command) StrictMode {
	switch c.Name {
	case "set":
		return StrictMode{Errexit: setsErrexit(c.Args), Nounset: mentions(c.Args, "nounset") || hasShortFlag(c.Args, 'u'), Pipefail: mentions(c.Args, "pipefail")}
	case "setopt", "unsetopt":
		var m StrictMode
		for _, a := range c.Args {
			lit, ok := parser.Literal(a)
			if !ok {
				continue
			}
			switch zshOption(lit, c.Name == "unsetopt") {
			case "errexit":
				m.Errexit = true
			case "nounset":
				m.Nounset = true
			case "pipefail":
				m.Pipefail = true
			}
		}
		return m
	}
	return StrictMode{}
}

func zshOption(word string, unset bool) string {
	name := strings.ToLower(strings.ReplaceAll(word, "_", ""))
	if !unset {
		return name
	}
	if rest, ok := strings.CutPrefix(name, "no"); ok {
		return rest
	}
	return "no" + name
}

func scanShellOptions(tree *parser.Tree) shellOptions {
	opts := shellOptions{errexit: shebangFlag(tree.Text(), 'e')}
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

func shebangFlag(text string, flag byte) bool {
	first, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(first, "#!") {
		return false
	}
	for _, field := range strings.Fields(first)[1:] {
		if strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "--") && strings.IndexByte(field, flag) > 0 {
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
