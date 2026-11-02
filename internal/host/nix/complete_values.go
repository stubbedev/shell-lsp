package nix

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

const (
	builtinsName = "builtins"
	evalBudget   = 4096
)

type Files func(path string) (string, bool)

func NoFiles(string) (string, bool) { return "", false }

type valueKind uint8

const (
	unknownValue valueKind = iota
	setValue
	lambdaValue
	builtinValue
	builtinsValue
)

type value struct {
	kind    valueKind
	members []member
	open    bool
	builtin builtins.Builtin
	lambda  *syntax.Lambda
	tree    *syntax.File
	doc     string
}

type member struct {
	name    string
	origins []origin
}

type origin struct {
	assign  *syntax.Assign
	depth   int
	inherit *syntax.Inherit
	attr    syntax.Attr
	formal  *syntax.Formal
}

type evaluator struct {
	tree     *syntax.File
	table    *scope.Table
	files    Files
	formals  map[*syntax.Ident]*syntax.Formal
	patterns map[*syntax.Ident]*syntax.PatternParam
	steps    int
}

func newEvaluator(tree *syntax.File, table *scope.Table, chain []syntax.Node, files Files) *evaluator {
	e := &evaluator{tree: tree, table: table, files: files, formals: map[*syntax.Ident]*syntax.Formal{}, patterns: map[*syntax.Ident]*syntax.PatternParam{}}
	for _, n := range chain {
		lambda, ok := n.(*syntax.Lambda)
		if !ok {
			continue
		}
		pattern, ok := lambda.Param.(*syntax.PatternParam)
		if !ok {
			continue
		}
		if pattern.Alias != nil {
			e.patterns[pattern.Alias] = pattern
		}
		for _, f := range pattern.Formals {
			e.formals[f.Name] = f
		}
	}
	return e
}

func (ev *evaluator) eval(e syntax.Expr) value {
	if ev.steps++; ev.steps > evalBudget {
		return value{}
	}
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Ident:
		if ref, ok := ev.table.Of(e); ok {
			return ev.evalRef(ref, e.Name)
		}
	case *syntax.AttrSet:
		return setOf(e.Bindings)
	case *syntax.Lambda:
		return value{kind: lambdaValue, lambda: e, tree: ev.tree}
	case *syntax.Select:
		return ev.selected(ev.eval(e.Target), e.Path)
	case *syntax.Apply:
		return ev.evalApplied(e.Fn, e.Args)
	case *syntax.Let:
		return ev.eval(e.Body)
	case *syntax.With:
		return ev.eval(e.Body)
	case *syntax.Assert:
		return ev.eval(e.Body)
	}
	return value{}
}

func (ev *evaluator) evalRef(ref scope.Ref, name string) value {
	switch ref.Kind {
	case scope.Definition:
		return ev.evalDef(ref.Def)
	case scope.Global:
		return builtinNamed(ref.Builtin)
	case scope.Dynamic:
		return ev.throughWith(ref.With, name)
	case scope.Undefined:
	}
	return value{}
}

func builtinNamed(b builtins.Builtin) value {
	if b.Name == builtinsName {
		return value{kind: builtinsValue}
	}
	return value{kind: builtinValue, builtin: b}
}

func (ev *evaluator) throughWith(innermost *syntax.With, name string) value {
	for _, with := range ev.table.Withs(innermost) {
		set := ev.eval(with.Scope)
		switch set.kind {
		case builtinsValue:
			if found := ev.memberNamed(set, name); found.kind != unknownValue {
				return found
			}
			continue
		case setValue:
			if i := memberIndex(set.members, name); i >= 0 {
				return ev.evalOrigins(set.members[i].origins)
			}
			if !set.open {
				continue
			}
		case unknownValue, lambdaValue, builtinValue:
		}
		return value{}
	}
	return value{}
}

func (ev *evaluator) evalDef(d *scope.Def) value {
	switch d.Kind {
	case scope.AliasDef:
		pattern := ev.patterns[d.Binders[0].(*syntax.Ident)]
		if pattern == nil {
			return value{}
		}
		out := value{kind: setValue, open: pattern.Ellipsis}
		for _, f := range pattern.Formals {
			out.members = addMember(out.members, f.Name.Name, origin{formal: f})
		}
		return out
	case scope.ArgDef, scope.FormalDef:
		return value{}
	case scope.LetDef, scope.RecDef, scope.InheritDef:
	}
	return ev.evalOrigins(ev.originsOf(d))
}

func (ev *evaluator) originsOf(d *scope.Def) []origin {
	out := make([]origin, 0, len(d.Bindings))
	for _, b := range d.Bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			out = append(out, origin{assign: b, depth: 1})
		case *syntax.Inherit:
			for _, attr := range b.Attrs {
				if owner, bound := ev.table.Binder(attr); bound && owner == d {
					out = append(out, origin{inherit: b, attr: attr})
				}
			}
		}
	}
	return out
}

func (ev *evaluator) evalOrigins(origins []origin) value {
	var merged, first value
	for i, o := range origins {
		v := ev.evalOrigin(o)
		if i == 0 {
			first = v
		}
		if v.kind != setValue {
			continue
		}
		merged.kind = setValue
		merged.open = merged.open || v.open
		for _, m := range v.members {
			merged.members = addMember(merged.members, m.name, m.origins...)
		}
	}
	if merged.kind == setValue {
		return merged
	}
	return first
}

func (ev *evaluator) evalOrigin(o origin) value {
	switch {
	case o.assign != nil:
		return ev.evalAssign(o)
	case o.inherit != nil:
		name, ok := syntax.AttrName(o.attr)
		if !ok {
			return value{}
		}
		if o.inherit.From != nil {
			return ev.memberNamed(ev.eval(o.inherit.From.Inner), name)
		}
		if ref, ok := ev.table.Inherited(o.attr); ok {
			return ev.evalRef(ref, name)
		}
	}
	return value{}
}

func (ev *evaluator) evalAssign(o origin) value {
	if o.depth < len(o.assign.Path) {
		out := value{kind: setValue}
		name, ok := syntax.AttrName(o.assign.Path[o.depth])
		if !ok {
			out.open = true
			return out
		}
		out.members = addMember(out.members, name, origin{assign: o.assign, depth: o.depth + 1})
		return out
	}
	v := ev.eval(o.assign.Value)
	if v.kind == lambdaValue && v.doc == "" {
		v.doc = commentAbove(ev.tree, o.assign.Span().Start)
	}
	return v
}

func setOf(bindings []syntax.Binding) value {
	out := value{kind: setValue}
	index := make(map[string]int, len(bindings))
	add := func(a syntax.Attr, o origin) {
		name, ok := syntax.AttrName(a)
		if !ok {
			out.open = true
			return
		}
		if i, seen := index[name]; seen {
			out.members[i].origins = append(out.members[i].origins, o)
			return
		}
		index[name] = len(out.members)
		out.members = append(out.members, member{name: name, origins: []origin{o}})
	}
	for _, b := range bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			add(b.Path[0], origin{assign: b, depth: 1})
		case *syntax.Inherit:
			for _, attr := range b.Attrs {
				add(attr, origin{inherit: b, attr: attr})
			}
		}
	}
	return out
}

func addMember(members []member, name string, origins ...origin) []member {
	if i := memberIndex(members, name); i >= 0 {
		members[i].origins = append(members[i].origins, origins...)
		return members
	}
	return append(members, member{name: name, origins: origins})
}

func memberIndex(members []member, name string) int {
	for i, m := range members {
		if m.name == name {
			return i
		}
	}
	return -1
}

func (ev *evaluator) memberNamed(v value, name string) value {
	switch v.kind {
	case builtinsValue:
		if strings.HasPrefix(name, builtins.HiddenAliasPrefix) {
			return value{}
		}
		if b, ok := builtins.Lookup(name); ok {
			return builtinNamed(b)
		}
	case setValue:
		if i := memberIndex(v.members, name); i >= 0 {
			return ev.evalOrigins(v.members[i].origins)
		}
	case unknownValue, lambdaValue, builtinValue:
	}
	return value{}
}

func (ev *evaluator) selected(v value, path syntax.AttrPath) value {
	for _, a := range path {
		name, ok := syntax.AttrName(a)
		if !ok {
			return value{}
		}
		v = ev.memberNamed(v, name)
	}
	return v
}

func (ev *evaluator) evalApplied(fn syntax.Expr, args []syntax.Expr) value {
	if len(args) > 0 {
		if loaded, ok := ev.loaded(fn, args[0]); ok {
			return ev.peel(loaded, len(args)-1)
		}
	}
	return ev.peel(ev.eval(fn), len(args))
}

func (ev *evaluator) peel(v value, n int) value {
	for range n {
		if v.kind != lambdaValue {
			return value{}
		}
		if next, ok := syntax.Unparenthesized(v.lambda.Body).(*syntax.Lambda); ok {
			v.lambda = next
			continue
		}
		if v.tree != ev.tree {
			return value{}
		}
		v = ev.eval(v.lambda.Body)
	}
	return v
}

func (ev *evaluator) loaded(fn, arg syntax.Expr) (value, bool) {
	if !ev.loads(fn) {
		return value{}, false
	}
	path, ok := syntax.Unparenthesized(arg).(*syntax.Path)
	if !ok || path.Kind != syntax.RelativePath {
		return value{}, true
	}
	for _, part := range path.Parts {
		if _, literal := part.(*syntax.Literal); !literal {
			return value{}, true
		}
	}
	span := path.Span()
	text, ok := ev.files(ev.tree.Text[span.Start:span.End])
	if !ok {
		return value{}, true
	}
	tree := syntax.Parse(text)
	lambda, ok := syntax.Unparenthesized(tree.Root).(*syntax.Lambda)
	if !ok {
		return value{}, true
	}
	return value{kind: lambdaValue, lambda: lambda, tree: tree, doc: commentAbove(tree, lambda.Span().Start)}, true
}

func (ev *evaluator) loads(fn syntax.Expr) bool {
	if calleeName(fn) == callPackageName {
		return true
	}
	id, ok := fn.(*syntax.Ident)
	if !ok || id.Name != importName {
		return false
	}
	ref, ok := ev.table.Of(id)
	return ok && ref.Kind == scope.Global
}

func (ev *evaluator) defItem(d *scope.Def) Item {
	it := Item{Label: d.Name, Kind: ev.defKind(d), Detail: d.Name}
	if binder, ok := d.Binders[0].(*syntax.Ident); ok {
		if f := ev.formals[binder]; f != nil {
			it.Detail = formalDetail(ev.tree.Text, f)
			it.Doc = commentAbove(ev.tree, f.Span().Start)
		}
		return it
	}
	origins := ev.originsOf(d)
	if len(origins) > 0 {
		it.Detail, it.Doc = ev.originDetail(origins[0]), ev.originDoc(origins[0])
	}
	return it
}

func (ev *evaluator) defKind(d *scope.Def) model.Kind {
	switch d.Kind {
	case scope.ArgDef, scope.FormalDef, scope.AliasDef:
		return model.KindParameter
	case scope.LetDef, scope.InheritDef:
		if bindsFunction(ev.originsOf(d)) {
			return model.KindFunction
		}
		return model.KindVariable
	case scope.RecDef:
	}
	if bindsFunction(ev.originsOf(d)) {
		return model.KindFunction
	}
	return model.KindAttribute
}

func bindsFunction(origins []origin) bool {
	if len(origins) == 0 || origins[0].assign == nil || origins[0].depth < len(origins[0].assign.Path) {
		return false
	}
	_, ok := syntax.Unparenthesized(origins[0].assign.Value).(*syntax.Lambda)
	return ok
}

func (ev *evaluator) memberItem(m member) Item {
	label := m.name
	if !ValidName(model.KindAttribute, m.name) {
		label = `"` + m.name + `"`
	}
	kind := model.KindAttribute
	if bindsFunction(m.origins) {
		kind = model.KindFunction
	}
	return Item{Label: label, Kind: kind, Detail: ev.originDetail(m.origins[0]), Doc: ev.originDoc(m.origins[0])}
}

func (ev *evaluator) originDetail(o origin) string {
	f := file{src: ev.tree.Text}
	switch {
	case o.assign != nil:
		return f.detail(binding{name: nameSpan(o.assign.Path[o.depth-1]), span: o.assign.Span()})
	case o.inherit != nil:
		return f.detail(binding{name: nameSpan(o.attr), span: o.inherit.Span(), inherit: true})
	}
	return formalDetail(ev.tree.Text, o.formal)
}

func (ev *evaluator) originDoc(o origin) string {
	switch {
	case o.assign != nil:
		return commentAbove(ev.tree, o.assign.Span().Start)
	case o.inherit != nil:
		return commentAbove(ev.tree, o.inherit.Span().Start)
	}
	return commentAbove(ev.tree, o.formal.Span().Start)
}

func formalDetail(text string, f *syntax.Formal) string {
	span := f.Span()
	detail := text[span.Start:span.End]
	if strings.Contains(detail, "\n") {
		return f.Name.Name
	}
	return detail
}

func commentAbove(tree *syntax.File, offset int) string {
	text := tree.Text
	var lines []string
	for at := offset; ; {
		c, ok := tree.CommentBefore(at)
		if !ok || strings.TrimSpace(text[textline.Start(text, c.Span.Start):c.Span.Start]) != "" || strings.Count(text[c.Span.End:at], "\n") > 1 {
			break
		}
		at = c.Span.Start
		if c.Kind == syntax.LineComment {
			if _, isDirective := parsedDirective(tree, c); isDirective {
				continue
			}
			lines = append(lines, strings.TrimSpace(text[c.Body.Start:c.Body.End]))
			continue
		}
		lines = append(lines, strings.TrimSpace(text[c.Body.Start:c.Body.End]))
		break
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n")
}
