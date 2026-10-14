package shelloptions

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type Options struct {
	Errexit        bool
	Pipefail       bool
	InheritErrexit bool
	Lastpipe       bool
	Execfail       bool
	GlobSubst      bool
	ShWordSplit    bool
	ExtendedGlob   bool
	KshGlob        bool
	NoBareGlobQual bool
	ExtGlob        bool
	GlobStar       bool
	NoCaseMatch    bool
	NoCaseGlob     bool
	NullGlob       bool
	FailGlob       bool
	NoPatsubAmp    bool
	RematchPCRE    bool
	Dynamic        bool
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
		set := StrictModeSetBy(c.Name, c.Args, tree.Dialect)
		m = StrictMode{Errexit: m.Errexit || set.Errexit, Nounset: m.Nounset || set.Nounset, Pipefail: m.Pipefail || set.Pipefail}
	}
	return m
}

func StrictModeSetBy(command string, args []*syntax.Word, d dialect.Dialect) StrictMode {
	changes := optionChanges(command, args, d)
	return StrictMode{Errexit: turnsOn(changes, "errexit"), Nounset: turnsOn(changes, "nounset"), Pipefail: turnsOn(changes, "pipefail")}
}

type optionChange struct {
	name string
	on   bool
}

func turnsOn(changes []optionChange, name string) bool {
	return slices.Contains(changes, optionChange{name: name, on: true})
}

func unwrap(command string, args []*syntax.Word) (string, []*syntax.Word) {
	for (command == "builtin" || command == "command") && len(args) > 0 {
		inner, ok := parser.Literal(args[0])
		if !ok {
			return "", nil
		}
		command, args = inner, args[1:]
	}
	return command, args
}

var optionCommands = map[string]bool{"set": true, "shopt": true, "setopt": true, "unsetopt": true, "emulate": true}

func setsUnknownOptions(command string, args []*syntax.Word) bool {
	command, args = unwrap(command, args)
	return optionCommands[command] && slices.ContainsFunc(args, func(w *syntax.Word) bool {
		_, ok := parser.Literal(w)
		return !ok
	})
}

func optionChanges(command string, args []*syntax.Word, d dialect.Dialect) []optionChange {
	command, args = unwrap(command, args)
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
	case "emulate":
		return emulateOptionChanges(args)
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

var shEmulation = []string{"globsubst", "shwordsplit", "nobareglobqual"}

var emulations = map[string][]string{
	"sh":   shEmulation,
	"bash": shEmulation,
	"ksh":  append(slices.Clip(shEmulation), "kshglob"),
}

func emulateOptionChanges(args []*syntax.Word) []optionChange {
	var changes []optionChange
	for i := 0; i < len(args); i++ {
		lit, ok := parser.Literal(args[i])
		if !ok {
			return changes
		}
		switch {
		case lit == "-o" || lit == "+o":
			if i+1 < len(args) {
				if name, ok := parser.Literal(args[i+1]); ok {
					changes = append(changes, optionChange{name: zshOption(name, lit == "+o"), on: true})
				}
				i++
			}
		case strings.HasPrefix(lit, "-"):
		default:
			for _, name := range emulations[lit] {
				changes = append(changes, optionChange{name: name, on: true})
			}
		}
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

func Scan(tree *parser.Tree) Options {
	opts := Options{
		Errexit:      turnsOn(shebangOptionChanges(tree.Text(), tree.Dialect), "errexit"),
		ExtendedGlob: tree.Dialect == dialect.Zsh && dialect.CompletionFile(tree.Text()),
	}
	fields := map[string]*bool{
		"errexit": &opts.Errexit, "pipefail": &opts.Pipefail, "inheriterrexit": &opts.InheritErrexit, "posix": &opts.InheritErrexit,
		"lastpipe": &opts.Lastpipe, "execfail": &opts.Execfail, "globsubst": &opts.GlobSubst, "shwordsplit": &opts.ShWordSplit,
		"extendedglob": &opts.ExtendedGlob, "kshglob": &opts.KshGlob, "nobareglobqual": &opts.NoBareGlobQual,
		"extglob": &opts.ExtGlob, "globstar": &opts.GlobStar, "nocasematch": &opts.NoCaseMatch, "nocaseglob": &opts.NoCaseGlob,
		"nullglob": &opts.NullGlob, "failglob": &opts.FailGlob, "rematchpcre": &opts.RematchPCRE,
	}
	cleared := map[string]*bool{"patsubreplacement": &opts.NoPatsubAmp}
	for _, c := range tree.Commands {
		opts.Dynamic = opts.Dynamic || setsUnknownOptions(c.Name, c.Args)
		for _, change := range optionChanges(c.Name, c.Args, tree.Dialect) {
			changed := fields
			if !change.on {
				changed = cleared
			}
			if field, ok := changed[change.name]; ok {
				*field = true
			}
		}
	}
	return opts
}

func EnabledBy(tree *parser.Tree, name string) []parser.Command {
	var commands []parser.Command
	for _, c := range tree.Commands {
		if turnsOn(optionChanges(c.Name, c.Args, tree.Dialect), name) {
			commands = append(commands, c)
		}
	}
	return commands
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
