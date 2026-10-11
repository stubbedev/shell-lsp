package actions

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func functionHeaderAt(s *site) (*syntax.FuncDecl, bool) {
	for _, node := range slices.Backward(s.pathAt()) {
		fn, ok := node.(*syntax.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name == nil || fn.Body == nil || s.sel.Start >= parser.Offset(fn.Body.Pos()) {
			return nil, false
		}
		return fn, true
	}
	return nil, false
}

func toggleFunctionKeyword(s *site) (Change, bool) {
	d := s.Tree.Dialect
	fn, ok := functionHeaderAt(s)
	if !ok || d == dialect.Ksh {
		return Change{}, false
	}
	text := s.Tree.Text()
	name := litSpan(fn.Name)
	if fn.RsrvWord {
		edits := []textedit.Edit{{Start: parser.Offset(fn.Position), End: name.Start}}
		if !fn.Parens {
			edits = append(edits, textedit.Edit{Start: name.End, End: name.End, NewText: "()"})
		}
		return Change{Title: "Declare " + fn.Name.Value + "() without the function keyword", Edits: edits}, true
	}
	keyword, known := shelldocs.Keyword("function")
	if _, block := fn.Body.Cmd.(*syntax.Block); !block || !known || !keyword.Dialects.Has(d) {
		return Change{}, false
	}
	parens, ok := emptyParens(text, name.End)
	if !ok {
		return Change{}, false
	}
	removal := textedit.Edit{Start: name.End, End: parens}
	if parens < len(text) && !strings.ContainsRune(" \t\n", rune(text[parens])) {
		removal.NewText = " "
	}
	return Change{Title: "Declare " + fn.Name.Value + " with the function keyword", Edits: []textedit.Edit{
		{Start: name.Start, End: name.Start, NewText: "function "},
		removal,
	}}, true
}

func emptyParens(text string, at int) (int, bool) {
	at += len(leadingSpace(text, at))
	if at >= len(text) || text[at] != '(' {
		return 0, false
	}
	at++
	at += len(leadingSpace(text, at))
	if at >= len(text) || text[at] != ')' {
		return 0, false
	}
	return at + 1, true
}
