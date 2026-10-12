package lint

import (
	"path"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type shellMode uint8

const (
	runsStdin shellMode = iota
	runsCommandString
	runsScriptFile
)

type shellInvocation struct {
	mode    shellMode
	operand *syntax.Word
}

var (
	posixShellOptions = getopt.Grammar{Short: "o:", StopAtOperand: true, Plus: true}
	bashOptions       = getopt.Grammar{
		Short:         "o:O:",
		Long:          map[string]getopt.Arity{"rcfile": getopt.Valued, "init-file": getopt.Valued},
		StopAtOperand: true,
		Plus:          true,
	}
	kshOptions = getopt.Grammar{Short: "o:R:T:", StopAtOperand: true, Plus: true}
)

func invocationGrammar(d dialect.Dialect) getopt.Grammar {
	switch d {
	case dialect.Bash:
		return bashOptions
	case dialect.Ksh:
		return kshOptions
	}
	return posixShellOptions
}

func shellInvocationOf(name string, args []*syntax.Word) (shellInvocation, bool) {
	shell, ok := dialect.FromName(path.Base(name))
	if !ok {
		return shellInvocation{}, false
	}
	parsed := getopt.Parse(invocationGrammar(shell), args, parser.Literal)
	switch {
	case parsed.Has("-c"):
		if len(parsed.Operands) == 0 {
			return shellInvocation{}, false
		}
		return shellInvocation{mode: runsCommandString, operand: parsed.Operands[0]}, true
	case parsed.Has("-s") || len(parsed.Operands) == 0:
		return shellInvocation{mode: runsStdin}, true
	}
	return shellInvocation{mode: runsScriptFile, operand: parsed.Operands[0]}, true
}
