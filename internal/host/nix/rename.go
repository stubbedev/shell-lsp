package nix

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func ValidName(_ model.Kind, name string) bool {
	if scope.Reserved(name) || slices.ContainsFunc(builtins.Keywords(), func(k builtins.Keyword) bool { return k.Name == name }) {
		return false
	}
	f := syntax.Parse(name)
	id, ok := f.Root.(*syntax.Ident)
	return ok && len(f.Errors) == 0 && len(f.Stray) == 0 && id.Name == name
}

func removal(src string, stmt *syntax.Inherit, attr syntax.Attr) model.Span {
	span := attr.Span()
	if attr == stmt.Attrs[len(stmt.Attrs)-1] {
		span.Start = len(strings.TrimRight(src[:span.Start], " \t\r\n"))
		return span
	}
	span.End = len(src) - len(strings.TrimLeft(src[span.End:], " \t\r\n"))
	return span
}

func rewriteInherit(src string, stmt *syntax.Inherit, attr syntax.Attr, before, after string) *model.Rewrite {
	if len(stmt.Attrs) == 1 {
		return &model.Rewrite{Span: stmt.Span(), Before: before, After: after}
	}
	end := stmt.Span().End
	return &model.Rewrite{Span: model.Span{Start: end, End: end}, Before: " " + before, After: after, Remove: removal(src, stmt, attr)}
}

func source(src string, n syntax.Node) string {
	span := n.Span()
	return src[span.Start:span.End]
}

type rewriteForm uint8

const (
	noRewrite rewriteForm = iota
	reboundForm
	keptForm
	selectedForm
)

type pendingRewrite struct {
	at   int32
	form rewriteForm
	stmt *syntax.Inherit
	attr syntax.Attr
	name string
}

func (p pendingRewrite) rewrite(src string) *model.Rewrite {
	switch p.form {
	case reboundForm:
		return rebound(src, p.stmt, p.attr, p.name)
	case keptForm:
		return kept(src, p.stmt, p.attr)
	case selectedForm:
		return selected(src, p.stmt, p.attr)
	case noRewrite:
	}
	return nil
}

func rewriteAt(pending []pendingRewrite, at int, src string) *model.Rewrite {
	i, found := slices.BinarySearchFunc(pending, model.Narrow(at), func(p pendingRewrite, at int32) int { return cmp.Compare(p.at, at) })
	if !found {
		return nil
	}
	return pending[i].rewrite(src)
}

func (r renames) SymbolRewrite(symbol int) *model.Rewrite {
	return rewriteAt(r.unit.symbolRewrites, symbol, r.src)
}

func (r renames) RefRewrite(ref int) *model.Rewrite {
	return rewriteAt(r.unit.refRewrites, ref, r.src)
}

func rebound(src string, stmt *syntax.Inherit, attr syntax.Attr, name string) *model.Rewrite {
	read := name
	if stmt.From != nil {
		read = source(src, stmt.From) + "." + source(src, attr)
	}
	return rewriteInherit(src, stmt, attr, "", " = "+read+";")
}

func kept(src string, stmt *syntax.Inherit, attr syntax.Attr) *model.Rewrite {
	return rewriteInherit(src, stmt, attr, source(src, attr)+" = ", ";")
}

func selected(src string, stmt *syntax.Inherit, attr syntax.Attr) *model.Rewrite {
	return rewriteInherit(src, stmt, attr, source(src, attr)+" = "+source(src, stmt.From)+".", ";")
}

type renames struct {
	src    string
	table  *scope.Table
	ix     *model.Index
	owners []owner
	unit   *symbolTable
}

func (r renames) Refusal(symbol int) string {
	sym, o := r.ix.Symbols[symbol], r.owners[symbol]
	switch {
	case sym.Exported:
		return fmt.Sprintf("`%s` is an attribute in a Nix file that puts it in the environment of shell code under this name, and other Nix code may read it by its name", sym.Name)
	case o.def != nil && o.def.Implicit:
		return fmt.Sprintf("`%s` is read by the Nix evaluator itself under this name", sym.Name)
	case o.def == nil && !o.set.closed():
		if at, left := o.set.leftAt(); left {
			return fmt.Sprintf("attribute `%s` belongs to an attribute set that is used as a whole on line %d, so other Nix code may read it by its name", sym.Name, strings.Count(r.src[:at], "\n")+1)
		}
		return fmt.Sprintf("attribute `%s` belongs to an attribute set with no name of its own in this file, such as the value of the file or an argument, so other Nix code may read it by its name", sym.Name)
	}
	return ""
}

func (r renames) line(n syntax.Node) int {
	return strings.Count(r.src[:n.Span().Start], "\n") + 1
}

func (r renames) Conflict(symbol int, newName string) string {
	sym, o := r.ix.Symbols[symbol], r.owners[symbol]
	if o.set.names.has(newName) {
		return fmt.Sprintf("`%s` is already bound next to `%s`", newName, sym.Name)
	}
	if o.set.reads.has(newName) {
		return fmt.Sprintf("this file already reads an attribute `%s` of the set that holds `%s`, which would then find the renamed one", newName, sym.Name)
	}
	if o.set.rare != nil && slices.ContainsFunc(o.set.rare.callers, func(c *attrs) bool { return c.names.has(newName) }) {
		return fmt.Sprintf("a call in this file already passes `%s` next to `%s`", newName, sym.Name)
	}
	if o.def == nil {
		return ""
	}
	capture, found := r.table.Capture(o.def, newName)
	if !found {
		return ""
	}
	switch capture.Kind {
	case scope.AlreadyBound:
		return fmt.Sprintf("`%s` is already bound in the same scope on line %d", newName, r.line(capture.Node))
	case scope.CapturedRead:
		return fmt.Sprintf("the read of `%s` on line %d would be captured by a binding of `%s` around it", sym.Name, r.line(capture.Node), newName)
	default:
		return fmt.Sprintf("the read of `%s` on line %d would then find the renamed `%s` instead of what it reads now", newName, r.line(capture.Node), sym.Name)
	}
}
