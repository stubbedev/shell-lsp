package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const optionTypeAttr = "type"

type NewFile struct {
	Name string
	Text string
}

func (d *Document) withToLet(selection model.Span) (Refactoring, bool) {
	with, header, ok := d.withHeaderAt(selection)
	if !ok {
		return Refactoring{}, false
	}
	names, ok := d.namesThroughWith(with)
	if !ok || len(names) == 0 {
		return Refactoring{}, false
	}
	bound := "let inherit (" + d.text(syntax.Unparenthesized(with.Scope).Span()) + ") " + strings.Join(names, " ") + "; in"
	return Refactoring{Title: "Convert `with` to `let`/`inherit`", Kind: RefactorRewrite, Edits: []textedit.Edit{{Start: header.Start, End: header.End, NewText: bound}}, rebinds: true}, true
}

func (d *Document) withHeaderAt(selection model.Span) (*syntax.With, model.Span, bool) {
	for _, node := range slices.Backward(d.pathTo(selection)) {
		with, isWith := node.(*syntax.With)
		if !isWith {
			continue
		}
		tokens := d.tree.TokensIn(syntax.Span{Start: with.Scope.Span().End, End: with.Body.Span().Start})
		semi := slices.IndexFunc(tokens, func(t syntax.Token) bool { return t.Kind == syntax.TokSemicolon })
		if semi < 0 {
			return nil, model.Span{}, false
		}
		header := model.Span{Start: with.Span().Start, End: tokens[semi].Span().End}
		return with, header, selection.Start >= header.Start && selection.End <= header.End
	}
	return nil, model.Span{}, false
}

func (d *Document) namesThroughWith(with *syntax.With) ([]string, bool) {
	var names []string
	for _, use := range d.table.Uses() {
		if use.Ref.Kind != scope.Dynamic {
			continue
		}
		chain := d.table.Withs(use.Ref.With)
		if !slices.Contains(chain, with) {
			continue
		}
		if len(chain) != 1 {
			return nil, false
		}
		names = append(names, use.Name)
	}
	slices.Sort(names)
	return slices.Compact(names), true
}

func (d *Document) extractToFile(selection model.Span) (Refactoring, bool) {
	site, ok := d.bindingAt(selection)
	if !ok {
		return Refactoring{}, false
	}
	stmt := site.assign
	path, static := attrNames(stmt.Path)
	if !static || len(path) != 1 || !extractableValue(stmt.Value) {
		return Refactoring{}, false
	}
	formals, ok := d.formalsOfValue(stmt.Value, path[0])
	if !ok {
		return Refactoring{}, false
	}
	name := path[0] + nixExtension
	call := "import ./" + name
	head := ""
	if len(formals) > 0 {
		call += " { inherit " + strings.Join(formals, " ") + "; }"
		head = "{ " + strings.Join(formals, ", ") + " }:\n"
	}
	value := stmt.Value.Span()
	return Refactoring{
		Title:   "Extract to `" + name + "`",
		Kind:    RefactorExtract,
		Edits:   []textedit.Edit{{Start: value.Start, End: value.End, NewText: call}},
		Create:  &NewFile{Name: name, Text: head + d.text(value) + "\n"},
		rebinds: true,
	}, true
}

func extractableValue(e syntax.Expr) bool {
	switch syntax.Unparenthesized(e).(type) {
	case *syntax.AttrSet, *syntax.List, *syntax.Lambda, *syntax.Let, *syntax.With, *syntax.Apply:
		return true
	}
	return false
}

func (d *Document) formalsOfValue(value syntax.Expr, self string) ([]string, bool) {
	var formals []string
	for _, use := range d.freeUses(value.Span()) {
		switch use.Ref.Kind {
		case scope.Dynamic, scope.Undefined:
			return nil, false
		case scope.Global:
		case scope.Definition:
			if use.Name == self {
				return nil, false
			}
			formals = append(formals, use.Name)
		}
	}
	slices.Sort(formals)
	return slices.Compact(formals), true
}

func (d *Document) OptionTypeAt(offset int) (model.Span, bool) {
	site, ok := d.bindingAt(model.Span{Start: offset, End: offset})
	if !ok {
		return model.Span{}, false
	}
	call, isCall := syntax.Unparenthesized(site.assign.Value).(*syntax.Apply)
	if !isCall || callName(d.table, call.Fn) != mkOptionName {
		return model.Span{}, false
	}
	set, isSet := syntax.Unparenthesized(call.Args[len(call.Args)-1]).(*syntax.AttrSet)
	if !isSet {
		return model.Span{}, false
	}
	for _, binding := range set.Bindings {
		assign, isAssign := binding.(*syntax.Assign)
		if !isAssign {
			continue
		}
		if path, static := attrNames(assign.Path); static && slices.Equal(path, []string{optionTypeAttr}) {
			return assign.Value.Span(), true
		}
	}
	return model.Span{}, false
}
