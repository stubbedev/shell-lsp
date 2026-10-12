package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var portableExpansionsFamily = family{
	codes: map[Code]meta{
		3002: {shells: strictShells, summary: "Extended globs are undefined in POSIX sh."},
		3004: {shells: strictShells, summary: "Locale-translated $\"...\" strings are undefined in POSIX sh."},
		3007: {shells: strictShells, summary: "$[...] arithmetic is undefined in POSIX sh."},
		3018: {shells: strictShells, summary: "The ++ and -- operators are undefined in POSIX sh."},
		3019: {shells: strictShells, summary: "Exponentiation with ** is undefined in POSIX sh."},
		3026: {shells: strictShells, summary: "Negating a bracket expression with ^ is undefined in POSIX sh."},
		3028: {shells: strictShells, summary: "A bash-specific variable is undefined in POSIX sh."},
		3052: {shells: strictShells, summary: "Arithmetic base prefixes like 16# are undefined in POSIX sh."},
		3053: {shells: strictShells, summary: "Indirect expansion with ${!name} is undefined in POSIX sh."},
		3054: {shells: strictShells, summary: "Array subscripts are undefined in POSIX sh."},
		3055: {shells: strictShells, summary: "Listing array keys with ${!array[@]} is undefined in POSIX sh."},
		3056: {shells: strictShells, summary: "Listing variable names by prefix with ${!prefix*} is undefined in POSIX sh."},
		3057: {shells: posixOrDash, summary: "Substring expansion ${var:offset} is undefined in POSIX sh and dash."},
		3058: {shells: posixOrDash, summary: "Applying a length or trim operator to $@ or $* is undefined in POSIX sh and dash."},
		3059: {shells: strictShells, summary: "Case modification in parameter expansion is undefined in POSIX sh."},
		3060: {shells: posixOrDash, summary: "Pattern substitution ${var/pattern/replacement} is undefined in POSIX sh and dash."},
	},
	nodes: []nodeRule{onNodes(checkPortableExpansions, &syntax.ExtGlob{}, &syntax.DblQuoted{}, &syntax.ArithmExp{}, &syntax.UnaryArithm{}, &syntax.BinaryArithm{}, &syntax.Word{}, &syntax.ParamExp{})},
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
		p.nonPortableNode(portabilityIssue{code: 3002, feature: "The extended glob " + n.Op.String() + "...)"}, n)
	case *syntax.DblQuoted:
		if n.Dollar {
			p.nonPortableNode(portabilityIssue{code: 3004, feature: `The locale-translated string $"..."`, alternative: `a plain "..." string`}, n)
		}
	case *syntax.ArithmExp:
		if n.Bracket {
			p.nonPortableNode(portabilityIssue{code: 3007, feature: "The obsolete $[...] arithmetic", alternative: "$((...))"}, n)
		}
	case *syntax.UnaryArithm:
		if n.Op == syntax.Inc || n.Op == syntax.Dec {
			p.nonPortable(portabilityIssue{code: 3018, feature: "The " + n.Op.String() + " operator", alternative: "an assignment such as i=$((i + 1))"}, posSpan(n.OpPos, len(n.Op.String())))
		}
	case *syntax.BinaryArithm:
		if n.Op == syntax.Pow {
			p.nonPortable(portabilityIssue{code: 3019, feature: "Exponentiation with **"}, posSpan(n.OpPos, len("**")))
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
		caret := offset + start + 1
		var fixes []Fix
		if caret < len(p.text) && p.text[caret] == '^' {
			fixes = append(fixes, safeFix("Replace [^ with [!", textedit.Edit{Start: caret, End: caret + 1, NewText: "!"}))
		}
		p.nonPortable(portabilityIssue{code: 3026, feature: "Negating a bracket expression with ^", alternative: "[!...]"}, parser.Span{Start: offset + start, End: offset + end}, fixes...)
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
		p.nonPortableNode(portabilityIssue{code: 3052, feature: "The base prefix " + lit.Value[:len(lit.Value)-len(digits)+1], alternative: "plain decimal numbers or printf conversion"}, lit)
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
	p.nonPortable(portabilityIssue{code: 3028, feature: "The bash variable " + name}, span)
}

func checkBashParamExp(p *pass, pe *syntax.ParamExp) {
	if pe.Param == nil {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	text := p.spanText(span)
	name := pe.Param.Value
	p.checkBashVariable(name, span)
	kind, _ := parser.ParamKindOf(name)
	listOrName := kind == parser.ParamAll || isNameChars(name)
	switch {
	case pe.Excl:
		checkIndirectExpansion(p, pe, span, text)
	case pe.Length:
		if kind == parser.ParamAll {
			p.nonPortable(portabilityIssue{code: 3058, feature: "Taking the length of " + text + " (a positional parameter list)", alternative: "$#"}, span)
		}
	case pe.Slice != nil:
		if pe.Index == nil && listOrName && !isEmptyQuotes(pe.Slice.Offset) {
			p.nonPortable(portabilityIssue{code: 3057, feature: "The substring expansion " + text}, span)
		}
	case pe.Repl != nil:
		if listOrName {
			p.nonPortable(portabilityIssue{code: 3060, feature: "The pattern substitution " + text, alternative: "sed or a case loop"}, span)
		}
	case pe.Exp != nil:
		checkExpansionOperator(p, pe, span, text)
	case pe.Index != nil:
		p.nonPortable(portabilityIssue{code: 3054, feature: "The array subscript " + text}, span)
	}
}

func checkIndirectExpansion(p *pass, pe *syntax.ParamExp, span parser.Span, text string) {
	if !parser.IsNameChar(pe.Param.Value[0]) {
		return
	}
	p.nonPortable(portabilityIssue{code: 3053, feature: "The indirect expansion " + text, alternative: "eval"}, span)
	switch {
	case pe.Names != 0:
		p.nonPortable(portabilityIssue{code: 3056, feature: "Listing variable names by prefix with " + text}, span)
	case isAllSubscript(pe):
		p.nonPortable(portabilityIssue{code: 3055, feature: "Listing array keys with " + text}, span)
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
		if kind, _ := parser.ParamKindOf(pe.Param.Value); pe.Index == nil && kind == parser.ParamAll {
			p.nonPortable(portabilityIssue{code: 3058, feature: "Trimming every positional parameter with " + text, alternative: "a loop over \"$@\""}, span)
		}
	case syntax.UpperFirst, syntax.UpperAll, syntax.LowerFirst, syntax.LowerAll:
		p.nonPortable(portabilityIssue{code: 3059, feature: "The case modification " + text, alternative: "tr"}, span)
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
