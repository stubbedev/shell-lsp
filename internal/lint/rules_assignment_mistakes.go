package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var assignmentMistakesFamily = family{
	codes: map[Code]meta{
		2037: {severity: SeverityError},
		2099: {severity: SeverityWarning},
		2100: {severity: SeverityWarning},
		2124: {severity: SeverityWarning},
		2125: {severity: SeverityWarning},
		2145: {severity: SeverityError},
		2209: {severity: SeverityWarning},
	},
	rules: []rule{
		{node: checkAssignmentCommand},
		{node: checkAssignedValue},
		{node: checkMixedArrayArgument},
	},
}

var arithmeticWords = setOf("+", "-", "*", "/")

func checkAssignmentCommand(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || len(call.Assigns) != 1 {
		return
	}
	span, ok := p.span(call)
	if !ok {
		return
	}
	assign := call.Assigns[0]
	if len(call.Args) > 0 {
		first := call.Args[0]
		if op, ok := parser.Literal(first); ok && arithmeticWords[op] {
			p.reportNode(2099, first, "Arithmetic needs $(( )): write "+assign.Name.Value+"=$(( ... "+op+" ... )) instead of separate words.")
		}
		if isGlob(first) || startsWithUnquotedDash(first) {
			p.report(2037, span, "To assign a command's output, use "+assign.Name.Value+"=$(command ...); as written the value is a prefix assignment for the next word.")
			return
		}
	}
	if assign.Value != nil && len(assign.Value.Parts) == 1 {
		if lit, ok := assign.Value.Parts[0].(*syntax.Lit); ok && posixUtilities[lit.Value] {
			p.report(2209, span, "To assign the output of "+lit.Value+", use "+assign.Name.Value+"=$("+lit.Value+"); quote it if the string itself is intended.")
		}
	}
	if len(call.Args) == 0 {
		p.checkStringArithmetic(assign)
	}
}

func startsWithUnquotedDash(w *syntax.Word) bool {
	lit, ok := w.Parts[0].(*syntax.Lit)
	return ok && strings.HasPrefix(lit.Value, "-")
}

func (p *pass) checkStringArithmetic(assign *syntax.Assign) {
	if assign.Value == nil || len(assign.Value.Parts) != 1 {
		return
	}
	lit, ok := assign.Value.Parts[0].(*syntax.Lit)
	if !ok {
		return
	}
	end := 0
	for end < len(lit.Value) && isNameChar(lit.Value[end]) {
		end++
	}
	name := lit.Value[:end]
	if !parser.IsName(name) || end+1 >= len(lit.Value) || !strings.ContainsRune("+-*", rune(lit.Value[end])) {
		return
	}
	span, ok := p.span(assign.Value)
	if !ok || p.tree.Visible(name, p.tree.ScopeAt(span.Start)).Len() == 0 {
		return
	}
	p.report(2100, span, "This assigns the literal string "+lit.Value+"; use $(( "+lit.Value+" )) to compute it.")
}

func checkAssignedValue(p *pass, n syntax.Node) {
	assign, ok := n.(*syntax.Assign)
	if !ok || assign.Naked || assign.Value == nil || assign.Array != nil {
		return
	}
	span, ok := p.span(assign.Value)
	if !ok {
		return
	}
	for part := range unquotedParts(assign.Value) {
		if pe, ok := part.(*syntax.ParamExp); ok && !pe.Length && kindOf(pe) == expandsElements {
			p.report(2124, span, "Assigning an array to a string joins its elements; assign it as an array with ( ), or use * to join deliberately.")
			return
		}
	}
	if isPattern(assign.Value) {
		p.report(2125, span, "Globs and brace expansions are not expanded in assignments; use an array ( ) to collect the matches, or quote the value.")
	}
}

func checkMixedArrayArgument(p *pass, n syntax.Node) {
	w, ok := n.(*syntax.Word)
	if !ok || !p.isArgumentWord(w) {
		return
	}
	parts := 0
	for range unquotedParts(w) {
		parts++
	}
	if parts < 2 {
		return
	}
	for part := range unquotedParts(w) {
		if pe, ok := part.(*syntax.ParamExp); ok && isArrayExpansion(pe) {
			if span, ok := p.span(pe); ok {
				p.report(2145, span, "This argument joins text with an array, which attaches the text to the first and last element only; use $* to join, or pass the array as separate arguments.")
			}
			return
		}
	}
}

func (p *pass) isArgumentWord(w *syntax.Word) bool {
	switch parent := p.parent().(type) {
	case *syntax.CallExpr:
		return true
	case *syntax.Redirect:
		return parent.Word == w
	}
	return false
}
