package lint

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var argumentsFamily = family{
	codes: map[Code]meta{
		2035: {severity: SeverityInfo, summary: "A file matched by a glob may start with a dash and be read as an option."},
		2101: {severity: SeverityWarning, summary: "A character class like [:digit:] is not inside a second pair of brackets."},
		2102: {severity: SeverityInfo, summary: "A bracket expression lists the same character twice, which suggests it was meant to match numbers."},
		2162: {severity: SeverityInfo, summary: "read without -r treats backslashes as escapes and drops them."},
		2336: {severity: SeverityWarning, summary: "cp -r is implementation-defined for special files."},
	},
	rules: []rule{
		{on: nodes(&syntax.CallExpr{}), node: checkLeadingGlobArguments},
		{on: nodes(&syntax.Word{}), node: checkBracketGlobs},
		{on: nodes(&syntax.CallExpr{}), node: checkReadWithoutRaw},
		{on: nodes(&syntax.CallExpr{}), node: checkRecursiveCopy},
	},
}

var optionTerminators = setOf("--", ":::", "::::")

var outputCommands = setOf("echo", "printf", "print")

func (p *pass) commandArguments(call *syntax.CallExpr) (string, []*syntax.Word, bool) {
	word := p.effectiveCommandWord(call)
	if word == nil {
		return "", nil, false
	}
	name, _ := parser.Literal(word)
	return name, call.Args[slices.Index(call.Args, word)+1:], true
}

func (p *pass) namesTestedVariable(call *syntax.CallExpr, w *syntax.Word) bool {
	previous, ok := previousArgument(call, w)
	if !ok || previous.Lit() != "-v" {
		return false
	}
	command := p.effectiveCommandWord(call)
	return command != nil && command.Lit() == "["
}

func previousArgument(call *syntax.CallExpr, w *syntax.Word) (*syntax.Word, bool) {
	i, found := slices.BinarySearchFunc(call.Args, w.Pos().Offset(), func(arg *syntax.Word, offset uint) int {
		return cmp.Compare(arg.Pos().Offset(), offset)
	})
	if !found || i == 0 || call.Args[i] != w {
		return nil, false
	}
	return call.Args[i-1], true
}

func checkLeadingGlobArguments(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	name, args, ok := p.commandArguments(call)
	if !ok || name == "[" || outputCommands[path.Base(name)] {
		return
	}
	for _, arg := range args {
		if lit, _ := parser.Literal(arg); optionTerminators[lit] {
			return
		}
		first, ok := arg.Parts[0].(*syntax.Lit)
		if !ok || (!strings.HasPrefix(first.Value, "*") && !strings.HasPrefix(first.Value, "?")) {
			continue
		}
		if span, ok := p.span(arg); ok {
			text := p.text[span.Start:span.End]
			p.report(2035, parser.Span{Start: span.Start, End: span.Start + 1}, "A file matched by "+text+" may start with a dash and be read as an option; use ./"+text+" or put -- before it.")
		}
	}
}

func checkBracketGlobs(p *pass, n syntax.Node) {
	w, ok := n.(*syntax.Word)
	if !ok || !p.expandsGlobs(w) {
		return
	}
	for _, part := range w.Parts[:parser.GlobQualifiers(w, true)] {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		start := parser.Offset(lit.Pos())
		for _, b := range bracketExpressions(lit.Value) {
			p.checkBracketExpression(lit.Value[b.start+1:b.end], parser.Span{Start: start + b.start, End: start + b.end + 1})
		}
	}
}

func (p *pass) checkBracketExpression(content string, span parser.Span) {
	inner := content
	if strings.HasPrefix(inner, "!") || strings.HasPrefix(inner, "^") {
		inner = inner[1:]
	}
	if len(inner) > 1 && strings.HasPrefix(inner, ":") && strings.HasSuffix(inner, ":") {
		p.report(2101, span, "A character class needs double brackets: [["+inner+"]] instead of ["+inner+"].")
		return
	}
	if strings.Contains(inner, "[") {
		return
	}
	chars := strings.ReplaceAll(unescapeGlob(inner), "-", "")
	for i := range len(chars) {
		if strings.IndexByte(chars[i+1:], chars[i]) >= 0 {
			p.report(2102, span, "A bracket expression matches single characters, so "+string(chars[i])+" is listed twice in ["+content+"]; ranges like [10-15] do not match numbers.")
			return
		}
	}
}

func unescapeGlob(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

type bracketRange struct{ start, end int }

func bracketExpressions(s string) []bracketRange {
	var out []bracketRange
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '[':
			if end := bracketEnd(s, i); end > 0 {
				out = append(out, bracketRange{start: i, end: end})
				i = end
			}
		}
	}
	return out
}

func bracketEnd(s string, open int) int {
	i := open + 1
	if i < len(s) && (s[i] == '!' || s[i] == '^') {
		i++
	}
	if i < len(s) && s[i] == ']' {
		i++
	}
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '[':
			if i+1 < len(s) && (s[i+1] == ':' || s[i+1] == '.' || s[i+1] == '=') {
				if close := strings.Index(s[i+2:], string(s[i+1])+"]"); close >= 0 {
					i += close + 3
				}
			}
		case ']':
			return i
		}
	}
	return -1
}

func (p *pass) expandsGlobs(w *syntax.Word) bool {
	switch owner := p.parent().(type) {
	case *syntax.CallExpr:
		name, _, ok := p.commandArguments(owner)
		if ok && (name == "tr" || name == "read") {
			return false
		}
		return !p.namesTestedVariable(owner, w)
	case *syntax.UnaryTest:
		return owner.Op != syntax.TsVarSet
	case *syntax.BinaryTest:
		return testOperators[owner.Op.String()].kind != arithmeticTest
	case *syntax.Redirect:
		return owner.Word == w
	case *syntax.ParamExp:
		return false
	}
	return true
}

func checkReadWithoutRaw(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	word := p.effectiveCommandWord(call)
	if word == nil || word.Lit() != "read" {
		return
	}
	args := call.Args[slices.Index(call.Args, word)+1:]
	flags := parseFlags(args, "adinNptu")
	if flags.has('r', "") || pollsInput(args) || p.readsWithoutEscapes(flags) {
		return
	}
	if span, ok := p.span(word); ok {
		p.report(2162, span, "read without -r treats backslashes as escapes and drops them; use read -r unless that is intended.",
			reviewFix("Add -r", textedit.Edit{Start: span.End, End: span.End, NewText: " -r"}))
	}
}

var dialectUnescapedReads = map[dialect.Dialect]string{dialect.Zsh: "qkzecl"}

func (p *pass) readsWithoutEscapes(flags flagSet) bool {
	for _, letter := range []byte(dialectUnescapedReads[p.dialect]) {
		if flags.has(letter, "") {
			return true
		}
	}
	return false
}

func pollsInput(args []*syntax.Word) bool {
	polls := false
	for i := 0; i < len(args); i++ {
		lit, ok := parser.Literal(args[i])
		if !ok || !strings.HasPrefix(lit, "-") || lit == "-" {
			continue
		}
		if lit == "--" {
			break
		}
		for j := 1; j < len(lit); j++ {
			switch {
			case strings.IndexByte("sre", lit[j]) >= 0:
				continue
			case strings.IndexByte("unNipat", lit[j]) < 0:
				return false
			}
			value := lit[j+1:]
			if value == "" {
				i++
				if i >= len(args) {
					return false
				}
				value, _ = parser.Literal(args[i])
			}
			if lit[j] == 't' {
				if value != "0" {
					return false
				}
				polls = true
			}
			break
		}
	}
	return polls
}

func checkRecursiveCopy(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	word := p.effectiveCommandWord(call)
	if word == nil || word.Lit() != "cp" {
		return
	}
	for _, arg := range call.Args[slices.Index(call.Args, word)+1:] {
		lit, ok := parser.Literal(arg)
		if !ok || lit == "--" {
			return
		}
		if !strings.HasPrefix(lit, "-") || strings.HasPrefix(lit, "--") || !strings.Contains(lit, "r") {
			continue
		}
		span, ok := p.span(arg)
		if !ok {
			return
		}
		at := span.Start + strings.IndexByte(p.text[span.Start:span.End], 'r')
		p.report(2336, span, "cp -r is implementation-defined for special files; use the portable -R.", safeFix("Replace -r with -R", textedit.Edit{Start: at, End: at + 1, NewText: "R"}))
		return
	}
}
