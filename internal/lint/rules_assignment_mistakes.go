package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var assignmentMistakesFamily = family{
	codes: map[Code]meta{
		2037: {severity: SeverityError, summary: "A command's output is meant to be assigned, but the value is a prefix assignment for the next word."},
		2099: {severity: SeverityWarning, summary: "Arithmetic written as separate words in an assignment is not evaluated."},
		2100: {severity: SeverityWarning, summary: "An arithmetic-looking value is assigned as a literal string."},
		2124: {severity: SeverityWarning, summary: "Assigning an array to a string joins its elements."},
		2125: {severity: SeverityWarning, summary: "Globs and brace expansions are not expanded in assignments."},
		2145: {severity: SeverityError, summary: "Joining text with an array attaches the text only to its first and last elements."},
		2209: {severity: SeverityWarning, summary: "A command name is assigned as a string instead of its output."},
	},
	nodes: []nodeRule{
		onNode(checkAssignmentCommand),
		onNode(checkAssignedValue),
		onNode(checkMixedArrayArgument),
	},
}

var arithmeticWords = setOf("+", "-", "*", "/")

func checkAssignmentCommand(p *pass, call *syntax.CallExpr) {
	if len(call.Assigns) != 1 {
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
		if lit, ok := assign.Value.Parts[0].(*syntax.Lit); ok && namesUtility(lit.Value) {
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
	for end < len(lit.Value) && parser.IsNameChar(lit.Value[end]) {
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

func checkAssignedValue(p *pass, assign *syntax.Assign) {
	if assign.Naked || assign.Value == nil || assign.Array != nil {
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

func checkMixedArrayArgument(p *pass, w *syntax.Word) {
	if !p.isArgumentWord(w) {
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
