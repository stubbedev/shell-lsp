package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type shellOptions struct {
	errexit        bool
	pipefail       bool
	inheritErrexit bool
	lastpipe       bool
	execfail       bool
	globSubst      bool
	shWordSplit    bool
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
	shebang := shebangOptionChanges(tree.Text(), tree.Dialect)
	m := StrictMode{Errexit: turnsOn(shebang, "errexit"), Nounset: turnsOn(shebang, "nounset")}
	for _, c := range tree.Commands {
		if m.Errexit && m.Nounset && m.Pipefail {
			break
		}
		set := StrictModeSetBy(c, tree.Dialect)
		m = StrictMode{Errexit: m.Errexit || set.Errexit, Nounset: m.Nounset || set.Nounset, Pipefail: m.Pipefail || set.Pipefail}
	}
	return m
}

func StrictModeSetBy(c parser.Command, d dialect.Dialect) StrictMode {
	changes := optionChanges(c.Name, c.Args, d)
	return StrictMode{Errexit: turnsOn(changes, "errexit"), Nounset: turnsOn(changes, "nounset"), Pipefail: turnsOn(changes, "pipefail")}
}

type optionChange struct {
	name string
	on   bool
}

func turnsOn(changes []optionChange, name string) bool {
	return slices.Contains(changes, optionChange{name: name, on: true})
}

func optionChanges(command string, args []*syntax.Word, d dialect.Dialect) []optionChange {
	switch command {
	case "set":
		return setOptionChanges(args, d)
	case "shopt":
		return shoptOptionChanges(args)
	case "setopt", "unsetopt":
		var changes []optionChange
		for _, a := range args {
			if lit, ok := parser.Literal(a); ok && !strings.HasPrefix(lit, "-") {
				changes = append(changes, optionChange{name: zshOption(lit, command == "unsetopt"), on: true})
			}
		}
		return changes
	}
	return nil
}

func setOptionChanges(args []*syntax.Word, d dialect.Dialect) []optionChange {
	var changes []optionChange
	var owed []bool
	for _, a := range args {
		lit, ok := parser.Literal(a)
		if !ok {
			return changes
		}
		if len(owed) > 0 {
			changes = append(changes, optionChange{name: optionName(lit), on: owed[0]})
			owed = owed[1:]
			continue
		}
		if len(lit) < 2 || (lit[0] != '-' && lit[0] != '+') || lit == "--" {
			return changes
		}
		on := lit[0] == '-'
		for i := 1; i < len(lit); i++ {
			if lit[i] == 'o' {
				owed = append(owed, on)
			} else if name, ok := shelldocs.SetLetter(lit[i], d); ok {
				changes = append(changes, optionChange{name: name, on: on})
			}
		}
	}
	return changes
}

func shoptOptionChanges(args []*syntax.Word) []optionChange {
	var names []string
	on, changing := false, false
	for _, a := range args {
		lit, ok := parser.Literal(a)
		if !ok {
			return nil
		}
		if len(lit) > 1 && lit[0] == '-' {
			if strings.ContainsRune(lit, 's') {
				on, changing = true, true
			}
			if strings.ContainsRune(lit, 'u') {
				on, changing = false, true
			}
			continue
		}
		names = append(names, optionName(lit))
	}
	if !changing {
		return nil
	}
	changes := make([]optionChange, len(names))
	for i, name := range names {
		changes[i] = optionChange{name: name, on: on}
	}
	return changes
}

func optionName(word string) string {
	return strings.ToLower(strings.ReplaceAll(word, "_", ""))
}

func zshOption(word string, unset bool) string {
	name := optionName(word)
	if !unset {
		return name
	}
	if rest, ok := strings.CutPrefix(name, "no"); ok {
		return rest
	}
	return "no" + name
}

func scanShellOptions(tree *parser.Tree) shellOptions {
	opts := shellOptions{errexit: turnsOn(shebangOptionChanges(tree.Text(), tree.Dialect), "errexit")}
	for _, c := range tree.Commands {
		for _, change := range optionChanges(c.Name, c.Args, tree.Dialect) {
			if !change.on {
				continue
			}
			switch change.name {
			case "errexit":
				opts.errexit = true
			case "pipefail":
				opts.pipefail = true
			case "inheriterrexit", "posix":
				opts.inheritErrexit = true
			case "lastpipe":
				opts.lastpipe = true
			case "execfail":
				opts.execfail = true
			case "globsubst":
				opts.globSubst = true
			case "shwordsplit":
				opts.shWordSplit = true
			}
		}
		if c.Name == "emulate" {
			emulated := slices.ContainsFunc(c.Args, func(a *syntax.Word) bool {
				lit, ok := parser.Literal(a)
				return ok && (lit == "sh" || lit == "ksh")
			})
			opts.globSubst = opts.globSubst || emulated
			opts.shWordSplit = opts.shWordSplit || emulated
		}
	}
	return opts
}

func shebangOptionChanges(text string, d dialect.Dialect) []optionChange {
	first, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(first, "#!") {
		return nil
	}
	var changes []optionChange
	for _, field := range strings.Fields(first)[1:] {
		if !strings.HasPrefix(field, "-") || strings.HasPrefix(field, "--") {
			continue
		}
		for i := 1; i < len(field); i++ {
			if name, ok := shelldocs.SetLetter(field[i], d); ok {
				changes = append(changes, optionChange{name: name, on: true})
			}
		}
	}
	return changes
}

func (p *pass) implicitlySplit(pe *syntax.ParamExp) bool {
	return p.dialect.Features().SplitsUnquoted || pe.Split == syntax.OptUnset && p.options().shWordSplit
}

func (p *pass) implicitlyGlobbed(pe *syntax.ParamExp) bool {
	return p.dialect.Features().GlobsUnquoted || pe.GlobSubst == syntax.OptUnset && p.options().globSubst
}

func (p *pass) substitutionGlobbed() bool {
	return p.dialect.Features().GlobsUnquoted || p.options().globSubst
}
