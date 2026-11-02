package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const functionLabel = "function"

type Signature struct {
	Label  string
	Doc    string
	Params []syntax.Span
	Active int
}

type callSite struct {
	apply *syntax.Apply
	arg   int
	depth int
}

func SignatureAt(text string, offset int) (Signature, bool) {
	c, ok := locate(text, offset, NoFiles)
	if !ok {
		return Signature{}, false
	}
	site, ok := c.callSite()
	if !ok {
		return Signature{}, false
	}
	head := c.calleeLabel(site.apply.Fn)
	for _, callee := range c.eval(site.apply.Fn) {
		switch callee := callee.(type) {
		case flowBuiltin:
			b, known := builtins.Lookup(callee.name)
			if !known {
				panic("nix: `" + callee.name + "` is not a builtin")
			}
			return builtinCall(head, b, site.arg)
		case flowLambda:
			return c.lambdaCall(head, callee, site)
		}
	}
	return Signature{}, false
}

func (a Analysis) Invocation(offset int) (model.Invocation, bool) {
	c, ok := locate(a.tree.Text, offset, NoFiles)
	if !ok {
		return model.Invocation{}, false
	}
	site, ok := c.callSite()
	if !ok {
		return model.Invocation{}, false
	}
	switch fn := syntax.Unparenthesized(site.apply.Fn).(type) {
	case *syntax.Ident:
		kind := model.KindFunction
		if ref, known := c.table.Of(fn); known && ref.Kind == scope.Definition {
			kind = c.defKind(ref.Def)
		}
		return model.Invocation{Name: fn.Name, Kind: kind, Active: site.arg}, true
	case *syntax.Select:
		return qualifiedInvocation(fn, site.arg)
	}
	return model.Invocation{}, false
}

func qualifiedInvocation(fn *syntax.Select, active int) (model.Invocation, bool) {
	target, ok := fn.Target.(*syntax.Ident)
	if !ok {
		return model.Invocation{}, false
	}
	names := []string{target.Name}
	for _, a := range fn.Path {
		name, static := syntax.AttrName(a)
		if !static {
			return model.Invocation{}, false
		}
		names = append(names, name)
	}
	last := len(names) - 1
	return model.Invocation{Name: names[last], Qualifier: names[:last], Kind: model.KindFunction, Active: active}, true
}

func (c *cursor) callSite() (callSite, bool) {
	for i := len(c.chain) - 2; i >= 0; i-- {
		apply, ok := c.chain[i].(*syntax.Apply)
		if !ok {
			continue
		}
		child := c.chain[i+1]
		if arg := slices.IndexFunc(apply.Args, func(e syntax.Expr) bool { return e == child }); arg >= 0 {
			return callSite{apply: apply, arg: arg, depth: i}, true
		}
	}
	return callSite{}, false
}

func (c *cursor) calleeLabel(fn syntax.Expr) string {
	span := fn.Span()
	text := c.clean(c.tree.Text[span.Start:span.End])
	switch syntax.Unparenthesized(fn).(type) {
	case *syntax.Ident, *syntax.Select:
		if !strings.Contains(text, "\n") {
			return text
		}
	}
	return functionLabel
}

type labelBuilder struct {
	text   strings.Builder
	params []syntax.Span
}

func (b *labelBuilder) word(s string) { b.text.WriteString(s) }

func (b *labelBuilder) param(s string) {
	start := b.text.Len()
	b.text.WriteString(s)
	b.params = append(b.params, syntax.Span{Start: start, End: b.text.Len()})
}

func builtinCall(head string, b builtins.Builtin, arg int) (Signature, bool) {
	if arg >= len(b.Params) {
		return Signature{}, false
	}
	var label labelBuilder
	label.word(head)
	for _, p := range b.Params {
		label.word(" ")
		label.param(p)
	}
	return Signature{Label: label.text.String(), Doc: b.Doc, Params: label.params, Active: arg}, true
}

type paramGroup struct {
	first   int
	formals []*syntax.Formal
}

func (c *cursor) lambdaCall(head string, callee flowLambda, site callSite) (Signature, bool) {
	var label labelBuilder
	label.word(head)
	var groups []paramGroup
	for lambda := callee.fn; lambda != nil; {
		label.word(" ")
		groups = append(groups, c.writeParam(&label, callee.unit.f.src, lambda.Param))
		lambda, _ = syntax.Unparenthesized(lambda.Body).(*syntax.Lambda)
	}
	if site.arg >= len(groups) {
		return Signature{}, false
	}
	return Signature{Label: label.text.String(), Doc: c.clean(c.calleeDoc(callee, site.apply.Fn)), Params: label.params, Active: c.activeParam(groups[site.arg], site)}, true
}

func (c *cursor) calleeDoc(callee flowLambda, fn syntax.Expr) string {
	if doc := heldDoc(callee); doc != "" {
		return doc
	}
	id, plain := syntax.Unparenthesized(fn).(*syntax.Ident)
	if !plain {
		return ""
	}
	ref, ok := c.table.Of(id)
	if !ok || ref.Kind != scope.Definition {
		return ""
	}
	origins := c.originsOf(ref.Def)
	if len(origins) == 0 {
		return ""
	}
	return c.originDoc(origins[0])
}

func heldDoc(callee flowLambda) string {
	tree := callee.unit.f.tree
	doc := ""
	visit := func(n syntax.Node) bool {
		a, assigned := n.(*syntax.Assign)
		if !assigned {
			return doc == ""
		}
		for l, ok := syntax.Unparenthesized(a.Value).(*syntax.Lambda); ok; l, ok = syntax.Unparenthesized(l.Body).(*syntax.Lambda) {
			if l == callee.fn {
				doc = commentAbove(tree, a.Span().Start)
				return false
			}
		}
		return doc == ""
	}
	for _, root := range slices.Concat([]syntax.Expr{tree.Root}, tree.Stray) {
		syntax.Inspect(root, visit)
	}
	return doc
}

func (c *cursor) writeParam(label *labelBuilder, text string, param syntax.Param) paramGroup {
	group := paramGroup{first: len(label.params)}
	switch param := param.(type) {
	case *syntax.IdentParam:
		label.param(c.clean(param.Name.Name))
	case *syntax.PatternParam:
		group.formals = param.Formals
		c.writePattern(label, text, param)
	default:
		panic("nix: a parsed lambda has an unknown parameter")
	}
	return group
}

func (c *cursor) writePattern(label *labelBuilder, text string, pattern *syntax.PatternParam) {
	if len(pattern.Formals) == 0 {
		label.param(emptyPattern(pattern))
		return
	}
	label.word("{ ")
	for i, f := range pattern.Formals {
		if i > 0 {
			label.word(", ")
		}
		label.param(c.clean(formalLabel(text, f)))
	}
	if pattern.Ellipsis {
		label.word(", ...")
	}
	label.word(" }")
}

func emptyPattern(p *syntax.PatternParam) string {
	if p.Ellipsis {
		return "{ ... }"
	}
	return "{ }"
}

func formalLabel(text string, f *syntax.Formal) string {
	if f.Default == nil {
		return f.Name.Name
	}
	span := f.Default.Span()
	value := text[span.Start:span.End]
	if strings.Contains(value, "\n") {
		value = "…"
	}
	return f.Name.Name + " ? " + value
}

func (c *cursor) activeParam(group paramGroup, site callSite) int {
	if len(group.formals) == 0 {
		return group.first
	}
	set, ok := syntax.Unparenthesized(site.apply.Args[site.arg]).(*syntax.AttrSet)
	if !ok {
		return group.first
	}
	if field, ok := c.fieldInSet(set, site, group.formals); ok {
		return group.first + field
	}
	bound := boundKeys(set)
	first := -1
	for i, f := range group.formals {
		if bound[f.Name.Name] {
			continue
		}
		if c.prefix != "" && strings.HasPrefix(f.Name.Name, c.prefix) {
			return group.first + i
		}
		if first < 0 {
			first = i
		}
	}
	return group.first + max(first, 0)
}

func (c *cursor) fieldInSet(set *syntax.AttrSet, site callSite, formals []*syntax.Formal) (int, bool) {
	for i := site.depth + 1; i+1 < len(c.chain); i++ {
		if c.chain[i] != syntax.Node(set) {
			continue
		}
		assign, ok := c.chain[i+1].(*syntax.Assign)
		if !ok {
			return 0, false
		}
		name, static := syntax.AttrName(assign.Path[0])
		if !static {
			return 0, false
		}
		field := slices.IndexFunc(formals, func(f *syntax.Formal) bool { return f.Name.Name == name })
		return field, field >= 0
	}
	return 0, false
}
