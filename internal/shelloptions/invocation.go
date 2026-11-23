package shelloptions

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type Flags struct {
	words []string
}

func ParseFlags(arguments []string) Flags {
	var f Flags
	for i := 0; i < len(arguments); i++ {
		word := arguments[i]
		if word == "--" || !strings.HasPrefix(word, "-") {
			break
		}
		if strings.HasPrefix(word, "--") {
			continue
		}
		letters, owes := strings.CutSuffix(strings.ReplaceAll(word[1:], "c", ""), "o")
		if letters != "" {
			f.words = append(f.words, "-"+letters)
		}
		if owes && i+1 < len(arguments) {
			f.words = append(f.words, "-o", arguments[i+1])
			i++
		}
	}
	return f
}

func (f Flags) SetCommand() string {
	if len(f.words) == 0 {
		return ""
	}
	return "set " + strings.Join(f.words, " ") + "\n"
}

func (f Flags) Pipefail() bool {
	for i := 0; i+1 < len(f.words); i++ {
		if f.words[i] == "-o" && optionName(f.words[i+1]) == "pipefail" {
			return true
		}
	}
	return false
}

func (f Flags) changes(s dialect.Shell) []optionChange {
	return setWordChanges(f.words, s)
}

type Invocation struct {
	Interpreter string
	Flags       Flags
}

func Invoke(argv []string) (Invocation, bool) {
	return invocationOf(dialect.Invoke(argv))
}

func InvokeShebang(text string) (Invocation, bool) {
	return invocationOf(dialect.ShebangInvocation(text))
}

func invocationOf(found dialect.Invocation, ok bool) (Invocation, bool) {
	if !ok {
		return Invocation{}, false
	}
	return Invocation{Interpreter: found.Interpreter, Flags: ParseFlags(found.Arguments)}, true
}

func (i Invocation) Dialect() (dialect.Dialect, bool) {
	return dialect.FromInterpreter(i.Interpreter)
}
