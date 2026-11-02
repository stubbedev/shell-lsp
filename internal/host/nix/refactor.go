package nix

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

const extractedName = "extracted"

type RefactorKind uint8

const (
	RefactorExtract RefactorKind = iota
	RefactorInline
	RefactorRewrite
)

type Refactoring struct {
	Title string
	Kind  RefactorKind
	Edits []textedit.Edit
	fresh string
	gone  *scope.Def
}

func (d *Document) Refactorings(selection model.Span) []Refactoring {
	if len(d.tree.Errors) > 0 {
		return nil
	}
	selection = trimmed(d.tree.Text, selection)
	var out []Refactoring
	for _, compute := range []func(model.Span) (Refactoring, bool){d.extract, d.inline, d.toInherit, d.nest, d.flatten} {
		if r, ok := compute(selection); ok && d.keepsMeaning(r) {
			out = append(out, r)
		}
	}
	return out
}

func trimmed(text string, span model.Span) model.Span {
	inner := text[span.Start:span.End]
	start := span.Start + len(inner) - len(strings.TrimLeft(inner, " \t\r\n"))
	return model.Span{Start: start, End: start + len(strings.TrimSpace(inner))}
}

func (d *Document) keepsMeaning(r Refactoring) bool {
	after := syntax.Parse(textedit.Apply(d.tree.Text, r.Edits))
	if len(after.Errors) > 0 || len(after.Stray) > 0 {
		return false
	}
	table := scope.Resolve(after)
	return len(table.Duplicates()) == len(d.table.Duplicates()) &&
		slices.Equal(meanings(after, table, nil, r.fresh), meanings(d.tree, d.table, r.gone, ""))
}

func meanings(tree *syntax.File, t *scope.Table, gone *scope.Def, fresh string) []string {
	ordinals := map[*scope.Def]int{}
	next := map[string]int{}
	for _, def := range t.Defs() {
		if def != gone && def.Name != fresh {
			ordinals[def] = next[def.Name]
			next[def.Name]++
		}
	}
	out := make([]string, 0, len(t.Uses()))
	for _, u := range t.Uses() {
		if u.Name == fresh || gone != nil && u.Ref.Def == gone {
			continue
		}
		out = append(out, u.Name+" "+meaning(tree, t, u.Ref, ordinals))
	}
	slices.Sort(out)
	return out
}

func meaning(tree *syntax.File, t *scope.Table, ref scope.Ref, ordinals map[*scope.Def]int) string {
	switch ref.Kind {
	case scope.Definition:
		return "binding " + strconv.Itoa(ordinals[ref.Def])
	case scope.Global:
		return "global"
	case scope.Dynamic:
		span := ref.With.Scope.Span()
		return "with " + strconv.Itoa(len(t.Withs(ref.With))) + " " + tree.Text[span.Start:span.End]
	case scope.Undefined:
	}
	return "undefined"
}

func (d *Document) checker() *checker { return &checker{tree: d.tree, table: d.table} }

func (d *Document) guarded(title string, kind RefactorKind, edits ...textedit.Edit) (Refactoring, bool) {
	fixes := d.checker().offer(title, lint.Review, edits...)
	if len(fixes) == 0 {
		return Refactoring{}, false
	}
	return Refactoring{Title: title, Kind: kind, Edits: fixes[0].Edits}, true
}

func (d *Document) pathTo(span model.Span) []syntax.Node {
	var path []syntax.Node
	d.inspect(func(n syntax.Node) bool {
		s := n.Span()
		if s.Start > span.Start || span.End > s.End {
			return false
		}
		path = append(path, n)
		return true
	})
	return path
}

func (d *Document) scopeAbove(path []syntax.Node, at int) *scope.Scope {
	for i := at - 1; i >= 0; i-- {
		if w, ok := path[i].(*syntax.With); ok && syntax.Node(w.Scope) == path[i+1] {
			continue
		}
		if s, ok := d.table.ScopeOf(path[i]); ok {
			return s
		}
	}
	return nil
}

func (d *Document) freeUses(span model.Span) []scope.Use {
	var out []scope.Use
	for _, u := range d.table.Uses() {
		at := u.Node.Span()
		if at.Start < span.Start || span.End < at.End {
			continue
		}
		if u.Ref.Def != nil {
			binder := u.Ref.Def.Binders[0].Span()
			if span.Start <= binder.Start && binder.End <= span.End {
				continue
			}
		}
		out = append(out, u)
	}
	return out
}

func positionDependent(uses []scope.Use) bool {
	return slices.ContainsFunc(uses, func(u scope.Use) bool { return u.Name == scope.CurPos })
}

func readsSame(uses []scope.Use, at *scope.Scope) bool {
	return !slices.ContainsFunc(uses, func(u scope.Use) bool { return !readSame(u, at) })
}

func readSame(u scope.Use, at *scope.Scope) bool {
	var with *syntax.With
	for s := at; s != nil; s = s.Parent {
		if with == nil {
			with = s.With
		}
		if i := slices.IndexFunc(s.Defs, func(def *scope.Def) bool { return def.Name == u.Name }); i >= 0 {
			return s.Defs[i] == u.Ref.Def
		}
	}
	switch u.Ref.Kind {
	case scope.Dynamic:
		return with == u.Ref.With
	case scope.Undefined:
		return with == nil
	case scope.Global:
		return true
	case scope.Definition:
	}
	return false
}

func (d *Document) extract(selection model.Span) (Refactoring, bool) {
	if selection.Start == selection.End {
		return Refactoring{}, false
	}
	path := d.pathTo(selection)
	self := slices.IndexFunc(path, func(n syntax.Node) bool {
		_, isExpr := n.(syntax.Expr)
		return isExpr && n.Span() == selection
	})
	if self < 0 || !extractable(path, self) {
		return Refactoring{}, false
	}
	uses := d.freeUses(selection)
	if positionDependent(uses) {
		return Refactoring{}, false
	}
	name := d.freshName()
	value := d.text(selection)
	replace := textedit.Edit{Start: selection.Start, End: selection.End, NewText: name}
	for i := self - 1; i >= 0; i-- {
		let, ok := path[i].(*syntax.Let)
		if !ok || len(let.Bindings) == 0 {
			continue
		}
		s, owned := d.table.ScopeOf(let)
		if !owned || !readsSame(uses, s) {
			continue
		}
		last := let.Bindings[len(let.Bindings)-1].Span()
		insert := textedit.Edit{Start: last.End, End: last.End, NewText: d.separatorAfter(last) + name + " = " + value + ";"}
		return d.extracted(name, insert, replace)
	}
	anchor := self
	for anchor > 0 && !standsAlone(path[anchor-1], path[anchor]) {
		anchor--
	}
	if !readsSame(uses, d.scopeAbove(path, anchor)) {
		return Refactoring{}, false
	}
	wrapper := "let " + name + " = " + value + "; in "
	if path[anchor].Span() == selection {
		replace.NewText = wrapper + name
		return d.extracted(name, replace)
	}
	start := path[anchor].Span().Start
	return d.extracted(name, textedit.Edit{Start: start, End: start, NewText: wrapper}, replace)
}

func (d *Document) extracted(name string, edits ...textedit.Edit) (Refactoring, bool) {
	r, ok := d.guarded("Extract to a `let` binding", RefactorExtract, edits...)
	r.fresh = name
	return r, ok
}

func extractable(path []syntax.Node, self int) bool {
	switch path[self].(type) {
	case *syntax.Ident, *syntax.Bad:
		return false
	}
	if self == 0 {
		return true
	}
	switch parent := path[self-1].(type) {
	case *syntax.StringAttr:
		return false
	case *syntax.Inherit:
		return syntax.Node(parent.From) != path[self]
	}
	return true
}

func standsAlone(parent, child syntax.Node) bool {
	is := func(e syntax.Expr) bool { return e != nil && syntax.Node(e) == child }
	switch p := parent.(type) {
	case *syntax.Assign:
		return is(p.Value)
	case *syntax.Lambda:
		return is(p.Body)
	case *syntax.Let:
		return is(p.Body)
	case *syntax.Paren:
		return is(p.Inner)
	case *syntax.Interpolation:
		return is(p.Value)
	case *syntax.If:
		return is(p.Cond) || is(p.Then) || is(p.Else)
	case *syntax.With:
		return is(p.Body)
	case *syntax.Assert:
		return is(p.Cond) || is(p.Body)
	case *syntax.Formal:
		return is(p.Default)
	case *syntax.DynamicAttr:
		return is(p.Value)
	}
	return false
}

func (d *Document) freshName() string {
	name := extractedName
	for n := 2; strings.Contains(d.tree.Text, name); n++ {
		name = extractedName + strconv.Itoa(n)
	}
	return name
}

func (d *Document) separatorAfter(stmt model.Span) string {
	text := d.tree.Text
	indent := text[textline.Start(text, stmt.Start):stmt.Start]
	if strings.TrimSpace(indent) == "" {
		return "\n" + indent
	}
	return " "
}

func (d *Document) inline(selection model.Span) (Refactoring, bool) {
	def, ok := d.inlineTarget(selection)
	if !ok {
		return Refactoring{}, false
	}
	stmt, ok := def.Bindings[0].(*syntax.Assign)
	if !ok || len(stmt.Path) != 1 {
		return Refactoring{}, false
	}
	id, ok := def.Uses()[0].Node.(*syntax.Ident)
	if !ok {
		return Refactoring{}, false
	}
	at := id.Span()
	if stmt.Span().Start <= at.Start && at.End <= stmt.Span().End {
		return Refactoring{}, false
	}
	value := stmt.Value
	uses := d.freeUses(value.Span())
	path := d.pathTo(at)
	if positionDependent(uses) || !readsSame(uses, d.scopeAbove(path, len(path)-1)) {
		return Refactoring{}, false
	}
	let := d.letOwning(def.Scope)
	if let == nil {
		return Refactoring{}, false
	}
	replacement := textedit.Edit{Start: at.Start, End: at.End, NewText: placed(d.text(value.Span()), value, path)}
	removal := textedit.Edit{Start: let.Span().Start, End: let.Body.Span().Start}
	if len(let.Bindings) > 1 {
		removal = d.checker().statementRemoval(stmt.Span())
	}
	r, ok := d.guarded("Inline `"+def.Name+"`", RefactorInline, removal, replacement)
	r.gone = def
	return r, ok
}

func (d *Document) inlineTarget(selection model.Span) (*scope.Def, bool) {
	inside := func(n syntax.Node) bool {
		s := n.Span()
		return s.Start <= selection.Start && selection.End <= s.End
	}
	for _, def := range d.table.Defs() {
		if def.Kind != scope.LetDef || def.Implicit || len(def.Uses()) != 1 || len(def.Bindings) != 1 {
			continue
		}
		if inside(def.Binders[0]) || inside(def.Uses()[0].Node) {
			return def, true
		}
	}
	return nil, false
}

func (d *Document) letOwning(s *scope.Scope) *syntax.Let {
	var found *syntax.Let
	d.inspect(func(n syntax.Node) bool {
		if let, ok := n.(*syntax.Let); ok {
			if owned, known := d.table.ScopeOf(let); known && owned == s {
				found = let
			}
		}
		return found == nil
	})
	return found
}

func placed(text string, value syntax.Expr, path []syntax.Node) string {
	if len(path) < 2 {
		return text
	}
	parent, child := path[len(path)-2], path[len(path)-1]
	if standsAlone(parent, child) || bareAt(value, parent, child) {
		return text
	}
	return "(" + text + ")"
}

func bareAt(value syntax.Expr, parent, child syntax.Node) bool {
	selected := false
	switch p := parent.(type) {
	case *syntax.Select:
		selected = syntax.Node(p.Target) == child
	case *syntax.HasAttr:
		selected = syntax.Node(p.Target) == child
	}
	if !selected {
		return atomic(value)
	}
	switch v := value.(type) {
	case *syntax.Ident:
		return v.Name != orKeyword
	case *syntax.Paren, *syntax.List, *syntax.AttrSet, *syntax.String:
		return true
	}
	return false
}

type bindingSite struct {
	assign   *syntax.Assign
	siblings []syntax.Binding
}

func (d *Document) bindingAt(selection model.Span) (bindingSite, bool) {
	var found bindingSite
	scan := func(bindings []syntax.Binding) {
		for _, b := range bindings {
			a, ok := b.(*syntax.Assign)
			if !ok {
				continue
			}
			names := pathSpan(a.Path)
			if names.Start <= selection.Start && selection.End <= names.End || a.Span() == selection {
				found = bindingSite{assign: a, siblings: bindings}
			}
		}
	}
	d.inspect(func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.AttrSet:
			scan(n.Bindings)
		case *syntax.Let:
			scan(n.Bindings)
		}
		return true
	})
	return found, found.assign != nil
}

func pathSpan(path syntax.AttrPath) model.Span {
	return model.Span{Start: path[0].Span().Start, End: path[len(path)-1].Span().End}
}

func (s bindingSite) headIsUnique() bool {
	head, static := syntax.AttrName(s.assign.Path[0])
	if !static {
		return false
	}
	other := func(a syntax.Attr) bool {
		name, ok := syntax.AttrName(a)
		return !ok || name == head
	}
	for _, b := range s.siblings {
		if b == syntax.Binding(s.assign) {
			continue
		}
		switch b := b.(type) {
		case *syntax.Assign:
			if other(b.Path[0]) {
				return false
			}
		case *syntax.Inherit:
			if slices.ContainsFunc(b.Attrs, other) {
				return false
			}
		}
	}
	return true
}

func (d *Document) toInherit(selection model.Span) (Refactoring, bool) {
	site, ok := d.bindingAt(selection)
	if !ok || len(site.assign.Path) != 1 {
		return Refactoring{}, false
	}
	attr, ok := site.assign.Path[0].(*syntax.IdentAttr)
	if !ok || attr.Name == orKeyword || attr.Name == scope.CurPos {
		return Refactoring{}, false
	}
	text := "inherit " + attr.Name + ";"
	switch v := site.assign.Value.(type) {
	case *syntax.Ident:
		if v.Name != attr.Name {
			return Refactoring{}, false
		}
	case *syntax.Select:
		from, ok := d.selectedFrom(v, attr.Name)
		if !ok {
			return Refactoring{}, false
		}
		text = "inherit (" + from + ") " + attr.Name + ";"
	default:
		return Refactoring{}, false
	}
	span := site.assign.Span()
	return d.guarded("Convert to `"+text+"`", RefactorRewrite, textedit.Edit{Start: span.Start, End: span.End, NewText: text})
}

func (d *Document) selectedFrom(v *syntax.Select, name string) (string, bool) {
	id, ok := v.Path[len(v.Path)-1].(*syntax.IdentAttr)
	if !ok || v.Default != nil || id.Name != name {
		return "", false
	}
	if len(v.Path) == 1 {
		return d.text(syntax.Unparenthesized(v.Target).Span()), true
	}
	return d.text(model.Span{Start: v.Span().Start, End: v.Path[len(v.Path)-2].Span().End}), true
}

func (d *Document) nest(selection model.Span) (Refactoring, bool) {
	site, ok := d.bindingAt(selection)
	if !ok || len(site.assign.Path) < 2 || !site.headIsUnique() {
		return Refactoring{}, false
	}
	a := site.assign
	head := d.text(a.Path[0].Span())
	rest := d.text(pathSpan(a.Path[1:]))
	text := head + " = { " + rest + " = " + d.text(a.Value.Span()) + "; };"
	return d.guarded("Nest as `"+head+" = { "+rest+" = …; };`", RefactorRewrite, textedit.Edit{Start: a.Span().Start, End: a.Span().End, NewText: text})
}

func (d *Document) flatten(selection model.Span) (Refactoring, bool) {
	site, ok := d.bindingAt(selection)
	if !ok || !site.headIsUnique() {
		return Refactoring{}, false
	}
	a := site.assign
	set, ok := a.Value.(*syntax.AttrSet)
	if !ok || set.Rec || len(set.Bindings) == 0 {
		return Refactoring{}, false
	}
	head := d.text(pathSpan(a.Path))
	lines := make([]string, 0, len(set.Bindings))
	for _, b := range set.Bindings {
		inner, isAssign := b.(*syntax.Assign)
		if !isAssign {
			return Refactoring{}, false
		}
		if _, static := syntax.AttrName(inner.Path[0]); !static {
			return Refactoring{}, false
		}
		lines = append(lines, head+"."+d.text(inner.Span()))
	}
	text := strings.Join(lines, d.separatorAfter(a.Span()))
	return d.guarded("Flatten to `"+head+".…`", RefactorRewrite, textedit.Edit{Start: a.Span().Start, End: a.Span().End, NewText: text})
}
