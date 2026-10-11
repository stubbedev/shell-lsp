package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var portableExpansionsFamily = family{
	codes: map[Code]meta{
		3002: {severity: SeverityWarning},
		3004: {severity: SeverityWarning},
		3007: {severity: SeverityWarning},
		3018: {severity: SeverityWarning},
		3019: {severity: SeverityWarning},
		3026: {severity: SeverityWarning},
		3028: {severity: SeverityWarning},
		3052: {severity: SeverityWarning},
		3053: {severity: SeverityWarning},
		3054: {severity: SeverityWarning},
		3055: {severity: SeverityWarning},
		3056: {severity: SeverityWarning},
		3057: {severity: SeverityWarning},
		3058: {severity: SeverityWarning},
		3059: {severity: SeverityWarning},
		3060: {severity: SeverityWarning},
	},
	rules: []rule{{on: nodes(&syntax.ExtGlob{}, &syntax.DblQuoted{}, &syntax.ArithmExp{}, &syntax.UnaryArithm{}, &syntax.BinaryArithm{}, &syntax.Word{}, &syntax.ParamExp{}), node: checkPortableExpansions}},
}

var (
	dynamicBashVariables = setOf("RANDOM", "SECONDS")
	staticBashVariables  = setOf(
		"OSTYPE", "MACHTYPE", "HOSTTYPE", "HOSTNAME", "DIRSTACK", "EUID", "UID", "SHLVL", "PIPESTATUS", "SHELLOPTS", "_",
		"BASHOPTS", "BASHPID", "BASH_ALIASES", "BASH_ARGC", "BASH_ARGV", "BASH_ARGV0", "BASH_CMDS", "BASH_COMMAND",
		"BASH_EXECUTION_STRING", "BASH_LINENO", "BASH_REMATCH", "BASH_SOURCE", "BASH_SUBSHELL", "BASH_VERSINFO",
		"EPOCHREALTIME", "EPOCHSECONDS", "FUNCNAME", "GROUPS", "MAPFILE",
	)
)

func checkPortableExpansions(p *pass, n syntax.Node) {
	if !strictShells.Has(p.dialect) {
		return
	}
	switch n := n.(type) {
	case *syntax.ExtGlob:
		p.nonPortableNode(portabilityIssue{code: 3002, shells: strictShells, feature: "The extended glob " + n.Op.String() + "...)"}, n)
	case *syntax.DblQuoted:
		if n.Dollar {
			p.nonPortableNode(portabilityIssue{code: 3004, shells: strictShells, feature: `The locale-translated string $"..."`, alternative: `a plain "..." string`}, n)
		}
	case *syntax.ArithmExp:
		if n.Bracket {
			p.nonPortableNode(portabilityIssue{code: 3007, shells: strictShells, feature: "The obsolete $[...] arithmetic", alternative: "$((...))"}, n)
		}
	case *syntax.UnaryArithm:
		if n.Op == syntax.Inc || n.Op == syntax.Dec {
			p.nonPortable(portabilityIssue{code: 3018, shells: strictShells, feature: "The " + n.Op.String() + " operator", alternative: "an assignment such as i=$((i + 1))"}, posSpan(n.OpPos, len(n.Op.String())))
		}
	case *syntax.BinaryArithm:
		if n.Op == syntax.Pow {
			p.nonPortable(portabilityIssue{code: 3019, shells: strictShells, feature: "Exponentiation with **"}, posSpan(n.OpPos, len("**")))
		}
	case *syntax.Word:
		if p.parsesGlobs(n) {
			checkCaretBracket(p, n)
		}
		if p.isArithmeticOperand(n) {
			checkArithmeticOperand(p, n)
		}
	case *syntax.ParamExp:
		checkBashParamExp(p, n)
	}
}

func checkCaretBracket(p *pass, w *syntax.Word) {
	for _, part := range w.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		start, end, found := caretBracket(lit.Value)
		if !found {
			continue
		}
		offset := parser.Offset(lit.ValuePos)
		p.nonPortable(portabilityIssue{code: 3026, shells: strictShells, feature: "Negating a bracket expression with ^", alternative: "[!...]"}, parser.Span{Start: offset + start, End: offset + end})
	}
}

func caretBracket(s string) (int, int, bool) {
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == '[' && i+2 < len(s) && s[i+1] == '^':
			if closing := strings.IndexByte(s[i+3:], ']'); closing >= 0 {
				return i, i + 3 + closing + 1, true
			}
		}
	}
	return 0, 0, false
}

func checkArithmeticOperand(p *pass, w *syntax.Word) {
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return
	}
	if len(w.Parts) == 1 && parser.IsName(lit.Value) {
		if span, ok := p.span(lit); ok {
			p.checkBashVariable(lit.Value, span)
		}
		return
	}
	digits := strings.TrimLeft(lit.Value, "0123456789")
	if digits != lit.Value && strings.HasPrefix(digits, "#") {
		p.nonPortableNode(portabilityIssue{code: 3052, shells: strictShells, feature: "The base prefix " + lit.Value[:len(lit.Value)-len(digits)+1], alternative: "plain decimal numbers or printf conversion"}, lit)
	}
}

func (p *pass) checkBashVariable(name string, span parser.Span) {
	switch {
	case dynamicBashVariables[name]:
	case !staticBashVariables[name], p.tree.Defines(name):
		return
	case name == "_" && p.dialect != dialect.Posix:
		return
	}
	p.nonPortable(portabilityIssue{code: 3028, shells: strictShells, feature: "The bash variable " + name}, span)
}

func checkBashParamExp(p *pass, pe *syntax.ParamExp) {
	if pe.Param == nil {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	text := p.text[span.Start:span.End]
	name := pe.Param.Value
	p.checkBashVariable(name, span)
	listOrName := name == "@" || name == "*" || isNameChars(name)
	switch {
	case pe.Excl:
		checkIndirectExpansion(p, pe, span, text)
	case pe.Length:
		if name == "@" || name == "*" {
			p.nonPortable(portabilityIssue{code: 3058, shells: posixOrDash, feature: "Taking the length of " + text + " (a positional parameter list)", alternative: "$#"}, span)
		}
	case pe.Slice != nil:
		if pe.Index == nil && listOrName && !isEmptyQuotes(pe.Slice.Offset) {
			p.nonPortable(portabilityIssue{code: 3057, shells: posixOrDash, feature: "The substring expansion " + text}, span)
		}
	case pe.Repl != nil:
		if listOrName {
			p.nonPortable(portabilityIssue{code: 3060, shells: posixOrDash, feature: "The pattern substitution " + text, alternative: "sed or a case loop"}, span)
		}
	case pe.Exp != nil:
		checkExpansionOperator(p, pe, span, text)
	case pe.Index != nil:
		p.nonPortable(portabilityIssue{code: 3054, shells: strictShells, feature: "The array subscript " + text}, span)
	}
}

func checkIndirectExpansion(p *pass, pe *syntax.ParamExp, span parser.Span, text string) {
	if !parser.IsNameChar(pe.Param.Value[0]) {
		return
	}
	p.nonPortable(portabilityIssue{code: 3053, shells: strictShells, feature: "The indirect expansion " + text, alternative: "eval"}, span)
	switch {
	case pe.Names != 0:
		p.nonPortable(portabilityIssue{code: 3056, shells: strictShells, feature: "Listing variable names by prefix with " + text}, span)
	case isAllSubscript(pe):
		p.nonPortable(portabilityIssue{code: 3055, shells: strictShells, feature: "Listing array keys with " + text}, span)
	}
}

func isEmptyQuotes(offset syntax.ArithmExpr) bool {
	w, ok := offset.(*syntax.Word)
	return ok && parser.LiteralChars(w) == "" && !slices.ContainsFunc(w.Parts, func(part syntax.WordPart) bool {
		switch part := part.(type) {
		case *syntax.SglQuoted:
			return false
		case *syntax.DblQuoted:
			return len(part.Parts) > 0
		}
		return true
	})
}

func isAllSubscript(pe *syntax.ParamExp) bool {
	w, ok := pe.Index.(*syntax.Word)
	if !ok || pe.Exp != nil || pe.Slice != nil || pe.Repl != nil {
		return false
	}
	lit := w.Lit()
	return lit == "@" || lit == "*"
}

func checkExpansionOperator(p *pass, pe *syntax.ParamExp, span parser.Span, text string) {
	switch pe.Exp.Op {
	case syntax.RemSmallSuffix, syntax.RemLargeSuffix, syntax.RemSmallPrefix, syntax.RemLargePrefix:
		if pe.Index == nil && (pe.Param.Value == "@" || pe.Param.Value == "*") {
			p.nonPortable(portabilityIssue{code: 3058, shells: posixOrDash, feature: "Trimming every positional parameter with " + text, alternative: "a loop over \"$@\""}, span)
		}
	case syntax.UpperFirst, syntax.UpperAll, syntax.LowerFirst, syntax.LowerAll:
		p.nonPortable(portabilityIssue{code: 3059, shells: strictShells, feature: "The case modification " + text, alternative: "tr"}, span)
	}
}

func isNameChars(s string) bool {
	for i := range len(s) {
		if !parser.IsNameChar(s[i]) {
			return false
		}
	}
	return true
}

func (p *pass) isArithmeticOperand(w *syntax.Word) bool { return isArithmeticOperandOf(p.parent(), w) }

func isArithmeticOperandOf(owner syntax.Node, w *syntax.Word) bool {
	if len(w.Parts) == 0 {
		return false
	}
	switch owner := owner.(type) {
	case *syntax.Assign:
		return owner.Index == syntax.ArithmExpr(w)
	case *syntax.ArrayElem:
		return owner.Index == syntax.ArithmExpr(w)
	case *syntax.ParamExp:
		return owner.Short && owner.Index == syntax.ArithmExpr(w)
	}
	return parser.IsArithmetic(owner)
}
