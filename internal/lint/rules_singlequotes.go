package lint

import (
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var singleQuotesFamily = family{
	codes: map[Code]meta{
		2016: {severity: SeverityInfo},
	},
	rules: []rule{{on: nodes(&syntax.SglQuoted{}), node: checkSingleQuotedExpansion}},
}

var (
	expansionSyntax  = regexp.MustCompile("\\$[{(0-9A-Za-z_]|`[^`]+`")
	sedLastLine      = regexp.MustCompile(`\$[acdgGilpPsxy]([^A-Za-z]|$)|\$\{\s*(/|$)|\$\{\s+[acdgGilpPsxy]([^A-Za-z]|$)`)
	literalArguments = setOf(
		"trap", "sh", "bash", "ksh", "zsh", "ssh", "eval", "xprop", "alias", "sudo", "doas", "run0", "docker",
		"podman", "oc", "dpkg-query", "jq", "rename", "rg", "unset", "git filter-branch", "mumps -run %XCMD",
		"mumps -run LOOP%XCMD",
	)
	dialectLiteralArguments = map[dialect.Dialect]map[string]bool{
		dialect.Zsh: setOf(
			"zstyle", "zmv", "zcp", "zln", "bindkey", "sched", "emulate",
			"_arguments", "_alternative", "_values", "_call_program",
		),
	}
)

const evaluatingFlag = 'e'

func checkSingleQuotedExpansion(p *pass, n syntax.Node) {
	sq, ok := n.(*syntax.SglQuoted)
	if !ok || sq.Dollar || !expansionSyntax.MatchString(sq.Value) || p.quotesPromptOrVariableName() || p.evaluatedByExpansion() {
		return
	}
	name := p.quotingCommand()
	switch {
	case name == "sed":
		if sedLastLine.MatchString(sq.Value) {
			return
		}
	case literalArguments[name], dialectLiteralArguments[p.dialect][name], strings.HasSuffix(name, "awk"), strings.HasPrefix(name, "perl"):
		return
	}
	if span, ok := p.span(sq); ok {
		p.report(2016, span, "Single quotes keep $ and backticks literal, so nothing is expanded here; use double quotes if expansion is intended.")
	}
}

func (p *pass) evaluatedByExpansion() bool {
	for _, n := range p.stack {
		pe, ok := n.(*syntax.ParamExp)
		if !ok || pe.Flags == nil {
			continue
		}
		if slices.ContainsFunc(parser.ParameterFlags(pe.Flags), func(f parser.ZshFlag) bool { return f.Name == evaluatingFlag }) {
			return true
		}
	}
	return false
}

func (p *pass) quotesPromptOrVariableName() bool {
	for depth := range 2 {
		switch a := p.ancestor(depth).(type) {
		case *syntax.Assign:
			if a.Name != nil && p.parameters.Is(a.Name.Value, shelldocs.PromptParameter) {
				return true
			}
		case *syntax.UnaryTest:
			if a.Op == syntax.TsVarSet {
				return true
			}
		case *syntax.CallExpr:
			word, ok := p.ancestor(0).(*syntax.Word)
			return ok && p.namesTestedVariable(a, word)
		}
	}
	return false
}

func (p *pass) quotingCommand() string {
	call, ok := nearestCommand(p.stack).(*syntax.CallExpr)
	if !ok {
		return ""
	}
	word := p.effectiveCommandWord(call)
	name, ok := parser.Literal(word)
	if !ok {
		return ""
	}
	name = path.Base(name)
	rest := call.Args[slices.Index(call.Args, word)+1:]
	switch name {
	case "find":
		return execActionName(rest)
	case "git":
		if len(rest) > 0 && rest[0].Lit() == "filter-branch" {
			return "git filter-branch"
		}
	case "mumps":
		if len(rest) > 1 && rest[0].Lit() == "-run" {
			return "mumps -run " + rest[1].Lit()
		}
	}
	return name
}

func execActionName(args []*syntax.Word) string {
	for i, a := range args[:max(len(args)-1, 0)] {
		if !findTerminatedActions[a.Lit()] {
			continue
		}
		if name, ok := parser.Literal(args[i+1]); ok {
			return path.Base(name)
		}
		break
	}
	return "find"
}
