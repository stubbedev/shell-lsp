package lint

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/embedded/sed"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var singleQuotesFamily = family{
	codes: map[Code]meta{
		2016: {severity: SeverityInfo, summary: "Single quotes keep $ and backticks literal, so nothing in them is expanded."},
	},
	nodes: []nodeRule{onNode(checkSingleQuotedExpansion)},
}

var (
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

const (
	evaluatingFlag          = 'e'
	sedExpansionPlaceholder = "1"
)

func checkSingleQuotedExpansion(p *pass, sq *syntax.SglQuoted) {
	if sq.Dollar || !strings.ContainsAny(sq.Value, "$`") || p.quotesPromptOrVariableName() || p.evaluatedByExpansion() {
		return
	}
	name := p.quotingCommand()
	if literalArguments[name] || dialectLiteralArguments[p.dialect][name] || strings.HasSuffix(name, "awk") || strings.HasPrefix(name, "perl") {
		return
	}
	readByCommand := func(syntax.Node) bool { return false }
	if name == "sed" {
		readByCommand = p.sedLastLineAddress(sq)
	}
	if !p.expandsWhenDoubleQuoted(sq.Value, readByCommand) {
		return
	}
	if span, ok := p.span(sq); ok {
		p.report(2016, span, "Single quotes keep $ and backticks literal, so nothing is expanded here; use double quotes if expansion is intended.")
	}
}

func (p *pass) sedLastLineAddress(sq *syntax.SglQuoted) func(syntax.Node) bool {
	word, ok := p.parent().(*syntax.Word)
	if !ok {
		panic(fmt.Sprintf("lint: the single-quoted text at offset %d sits in a %T, not a word", parser.Offset(sq.Pos()), p.parent()))
	}
	preceding := word.Parts[:slices.Index(word.Parts, syntax.WordPart(sq))]
	before, _ := parser.Text(&syntax.Word{Parts: preceding}, sedExpansionPlaceholder)
	addresses := sed.LastLineAddresses(before + sq.Value)
	return func(n syntax.Node) bool {
		pe, ok := n.(*syntax.ParamExp)
		return ok && pe.Short && slices.Contains(addresses, len(before)+parser.Offset(pe.Dollar))
	}
}

func (p *pass) expandsWhenDoubleQuoted(text string, readByCommand func(syntax.Node) bool) bool {
	word, err := parser.ParseDocument(text, p.tree.Lang())
	if err != nil {
		return false
	}
	found := false
	parser.Walk(word, func(n syntax.Node) bool {
		if found {
			return false
		}
		expands, closed := doubleQuotedExpansion(n)
		found = expands && closed && !readByCommand(n)
		return closed
	})
	return found
}

func doubleQuotedExpansion(n syntax.Node) (expands, closed bool) {
	switch n := n.(type) {
	case *syntax.ParamExp:
		if n.Short {
			return namesParameterDirectly(n), true
		}
		return true, parser.Closed(n.Rbrace)
	case *syntax.CmdSubst:
		return !n.Backquotes || len(n.Stmts) > 0, parser.Closed(n.Right)
	case *syntax.ArithmExp:
		return !n.Bracket, parser.Closed(n.Right)
	}
	return false, true
}

func namesParameterDirectly(pe *syntax.ParamExp) bool {
	kind, _ := parser.ParamKindOf(pe.Param.Value)
	named := kind == parser.ParamName || kind == parser.ParamPositional || kind == parser.ParamZero
	return named && parser.Offset(pe.Param.Pos()) == parser.Offset(pe.Dollar)+len("$")
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
	command, ok := p.resolved(call)
	if !ok || !command.literal {
		return ""
	}
	name, rest := path.Base(command.name), command.args
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
