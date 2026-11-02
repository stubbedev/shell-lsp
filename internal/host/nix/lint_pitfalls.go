package nix

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const (
	mkIfCall       = "mkIf"
	optionalCall   = "optional"
	optionalsCall  = "optionals"
	mainInput      = "nixpkgs"
	flakeSeparator = "/"
)

var (
	flakeTopLevel = []string{"description", inputsAttr, flakeOutput, "nixConfig"}
	impureReads   = []string{"currentSystem", "currentTime", "getEnv", "nixPath"}
	pinningKeys   = map[string][]string{
		"fetchTarball": {"sha256", "narHash"},
		"fetchurl":     {"sha256"},
		"fetchGit":     {"rev", "narHash"},
	}
)

func flakeShaped(root syntax.Expr) bool {
	set, ok := root.(*syntax.AttrSet)
	if !ok || set.Rec || flakeOutputs(root) == nil {
		return false
	}
	for _, b := range set.Bindings {
		a, ok := b.(*syntax.Assign)
		if !ok {
			return false
		}
		if name, static := syntax.AttrName(a.Path[0]); !static || !slices.Contains(flakeTopLevel, name) {
			return false
		}
	}
	return true
}

func (c *checker) impureBuiltin(name string, span syntax.Span) {
	if c.flake && slices.Contains(impureReads, name) {
		c.report(lint.CodeNixImpureInFlake, span, fmt.Sprintf("`%s` is not available when a flake is evaluated purely, so this fails or reads nothing without `--impure`", name), nil)
	}
}

func (c *checker) searchPath(p *syntax.Path) {
	if c.flake && p.Kind == syntax.SearchPath {
		c.report(lint.CodeNixImpureInFlake, p.Span(), "a `<...>` lookup fails when a flake is evaluated purely; use an input instead", nil)
	}
}

func (c *checker) application(a *syntax.Apply) {
	name, ok := c.builtinName(a.Fn)
	if !ok {
		return
	}
	keys, fetcher := pinningKeys[name]
	if !fetcher {
		return
	}
	arg := syntax.Unparenthesized(a.Args[0])
	if !unpinnedArgument(arg, keys) {
		return
	}
	c.report(lint.CodeNixUnpinnedFetch, a.Fn.Span(), fmt.Sprintf("`%s` without `%s` fetches whatever is there today, and fails in a pure evaluation", name, keys[0]), nil)
}

func unpinnedArgument(arg syntax.Expr, keys []string) bool {
	switch arg := arg.(type) {
	case *syntax.String, *syntax.URI:
		return true
	case *syntax.AttrSet:
		if arg.Rec {
			return false
		}
		for _, b := range arg.Bindings {
			a, ok := b.(*syntax.Assign)
			if !ok {
				return false
			}
			name, static := syntax.AttrName(a.Path[0])
			if !static || slices.Contains(keys, name) {
				return false
			}
		}
		return true
	}
	return false
}

func (c *checker) recOverride(b *syntax.Binary) {
	set, ok := syntax.Unparenthesized(b.Left).(*syntax.AttrSet)
	if !ok || !set.Rec {
		return
	}
	s, effective := c.table.ScopeOf(set)
	if !effective {
		return
	}
	for right := syntax.Unparenthesized(b.Right); right != nil; {
		overriding := right
		right = nil
		if chain, chained := overriding.(*syntax.Binary); chained && chain.Op == syntax.OpUpdate {
			overriding = syntax.Unparenthesized(chain.Left)
			right = syntax.Unparenthesized(chain.Right)
		}
		if literal, isSet := overriding.(*syntax.AttrSet); isSet {
			c.overrides(s, literal)
		}
	}
}

func (c *checker) overrides(s *scope.Scope, literal *syntax.AttrSet) {
	for _, b := range literal.Bindings {
		a, ok := b.(*syntax.Assign)
		if !ok || len(a.Path) != 1 {
			continue
		}
		name, static := syntax.AttrName(a.Path[0])
		if !static {
			continue
		}
		for _, d := range s.Defs {
			if d.Name == name && !d.Implicit && len(d.Uses()) > 0 {
				c.report(lint.CodeNixRecOverride, a.Path[0].Span(), fmt.Sprintf("the `rec` set on the left of `//` keeps reading its own `%s`, not this one", name), nil)
			}
		}
	}
}

func (c *checker) fileShape() {
	c.topLevelWith()
	if c.flake {
		c.unusedFlakeInputs()
	}
}

func moduleBody(lambda *syntax.Lambda) syntax.Expr {
	body := syntax.Unparenthesized(lambda.Body)
	for {
		l, isLet := body.(*syntax.Let)
		if !isLet {
			return body
		}
		body = syntax.Unparenthesized(l.Body)
	}
}

func (c *checker) moduleShape(lambda *syntax.Lambda, pattern *syntax.PatternParam) {
	config, ok := c.configFormalOf(pattern)
	if !ok {
		return
	}
	body := moduleBody(lambda)
	if e, isIf := body.(*syntax.If); isIf && pattern.Ellipsis && moduleBranches(e) && c.strictlyReads(e.Cond, config, 0) {
		c.report(lint.CodeNixModuleRecursion, e.Cond.Span(), "the module system must evaluate this module's shape before `config` exists, so a module that branches on `config` recurses forever; use `lib.mkIf`", nil)
		return
	}
	set, isSet := body.(*syntax.AttrSet)
	if !isSet {
		return
	}
	for _, b := range set.Bindings {
		a, isAssign := b.(*syntax.Assign)
		if !isAssign || len(a.Path) != 1 {
			continue
		}
		switch name, _ := syntax.AttrName(a.Path[0]); name {
		case configFormal:
			c.configBranch(a.Value, config)
		case importsAttr:
			c.importsBranch(a.Value, config)
		}
	}
}

func moduleBranches(e *syntax.If) bool {
	return moduleKeyed(e.Then) || moduleKeyed(e.Else)
}

func moduleKeyed(e syntax.Expr) bool {
	set, ok := syntax.Unparenthesized(e).(*syntax.AttrSet)
	if !ok {
		return false
	}
	return slices.ContainsFunc(set.Bindings, func(b syntax.Binding) bool {
		a, isAssign := b.(*syntax.Assign)
		if !isAssign {
			return false
		}
		name, static := syntax.AttrName(a.Path[0])
		return static && slices.Contains(moduleKeys, name)
	})
}

func (c *checker) configFormalOf(pattern *syntax.PatternParam) (*scope.Def, bool) {
	for _, f := range pattern.Formals {
		if f.Name.Name == configFormal {
			return c.table.Binder(f.Name)
		}
	}
	return nil, false
}

func (c *checker) configBranch(value syntax.Expr, config *scope.Def) {
	e, ok := syntax.Unparenthesized(value).(*syntax.If)
	if !ok || !c.strictlyReads(e.Cond, config, 0) {
		return
	}
	c.report(lint.CodeNixModuleRecursion, e.Cond.Span(), "`config = if` on a value from `config` recurses forever: the module system needs this set's attributes to compute `config`", c.mkIfFix(e))
}

func (c *checker) mkIfFix(e *syntax.If) []lint.Fix {
	otherwise, ok := syntax.Unparenthesized(e.Else).(*syntax.AttrSet)
	if !ok || len(otherwise.Bindings) != 0 {
		return nil
	}
	if d, bound := c.bindingAt(libName, e.Span().Start); !bound || d == nil {
		return nil
	}
	replacement := libName + "." + mkIfCall + " " + c.argument(e.Cond) + " " + c.argument(e.Then)
	return c.offer("Use `lib.mkIf`", lint.Review, textedit.Edit{Start: e.Span().Start, End: e.Span().End, NewText: replacement})
}

func (c *checker) argument(e syntax.Expr) string {
	if atomic(e) {
		return c.textOf(e.Span())
	}
	return "(" + c.textOf(e.Span()) + ")"
}

func (c *checker) importsBranch(value syntax.Expr, config *scope.Def) {
	switch e := syntax.Unparenthesized(value).(type) {
	case *syntax.If:
		if c.strictlyReads(e.Cond, config, 0) {
			c.importsRecursion(e.Cond)
		}
	case *syntax.Binary:
		if e.Op == syntax.OpConcat {
			c.importsBranch(e.Left, config)
			c.importsBranch(e.Right, config)
		}
	case *syntax.Apply:
		if c.optionalListCall(e) && c.strictlyReads(e.Args[0], config, 0) {
			c.importsRecursion(e.Args[0])
		}
	}
}

func (c *checker) importsRecursion(cond syntax.Expr) {
	c.report(lint.CodeNixModuleRecursion, cond.Span(), "`imports` is read before `config` exists, so choosing imports from `config` recurses forever", nil)
}

func (c *checker) optionalListCall(a *syntax.Apply) bool {
	fn, ok := a.Fn.(*syntax.Select)
	if !ok || len(a.Args) != 2 || len(fn.Path) == 0 {
		return false
	}
	name, static := syntax.AttrName(fn.Path[len(fn.Path)-1])
	return static && (name == optionalCall || name == optionalsCall)
}

func (c *checker) strictlyReads(e syntax.Expr, config *scope.Def, depth int) bool {
	if depth > len(c.table.Defs()) {
		return false
	}
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Ident:
		return c.identReads(e, config, depth)
	case *syntax.Select:
		return e.Default == nil && c.strictlyReads(e.Target, config, depth)
	case *syntax.Unary:
		return c.strictlyReads(e.Operand, config, depth)
	case *syntax.Binary:
		if e.Op == syntax.OpEq || e.Op == syntax.OpNe {
			return c.strictlyReads(e.Left, config, depth) || c.strictlyReads(e.Right, config, depth)
		}
		if e.Op == syntax.OpAnd || e.Op == syntax.OpOr {
			return c.strictlyReads(e.Left, config, depth)
		}
	}
	return false
}

func (c *checker) identReads(id *syntax.Ident, config *scope.Def, depth int) bool {
	ref, resolved := c.table.Of(id)
	if !resolved || ref.Def == nil {
		return false
	}
	if ref.Def == config {
		return true
	}
	if len(ref.Def.Bindings) != 1 {
		return false
	}
	a, ok := ref.Def.Bindings[0].(*syntax.Assign)
	if !ok || len(a.Path) != 1 {
		return false
	}
	return c.strictlyReads(a.Value, config, depth+1)
}

func (c *checker) bindingAt(name string, at int) (*scope.Def, bool) {
	for s := c.table.ScopeAt(at); s != nil; s = s.Parent {
		for _, d := range s.Defs {
			if d.Name == name {
				return d, true
			}
		}
	}
	return nil, false
}

func (c *checker) topLevelWith() {
	if !c.optional {
		return
	}
	body := syntax.Unparenthesized(c.tree.Root)
	if lambda, ok := body.(*syntax.Lambda); ok {
		body = syntax.Unparenthesized(lambda.Body)
	}
	for {
		l, isLet := body.(*syntax.Let)
		if !isLet {
			break
		}
		body = syntax.Unparenthesized(l.Body)
	}
	w, ok := body.(*syntax.With)
	if !ok {
		return
	}
	start := w.Span().Start
	c.report(lint.CodeNixTopLevelWith, syntax.Span{Start: start, End: start + len(withKeyword)}, "a `with` around a whole file hides where every name comes from, and hides typos from the undefined-name check", nil)
}

func (c *checker) storeCopy(i *syntax.Interpolation) {
	if !c.optional {
		return
	}
	p, ok := syntax.Unparenthesized(i.Value).(*syntax.Path)
	if !ok || p.Kind == syntax.SearchPath {
		return
	}
	c.report(lint.CodeNixStoreCopy, i.Span(), "interpolating a path copies all of it into the Nix store", nil)
}

func (c *checker) pkgsLib(n *syntax.Select) {
	if !c.optional || len(n.Path) == 0 {
		return
	}
	target, ok := n.Target.(*syntax.Ident)
	if !ok || target.Name != pkgsName {
		return
	}
	first, static := syntax.AttrName(n.Path[0])
	if !static || first != libName {
		return
	}
	at := n.Span().Start
	d, bound := c.bindingAt(libName, at)
	if !bound || slices.ContainsFunc(d.Bindings, func(b syntax.Binding) bool {
		span := b.Span()
		return span.Start <= at && at < span.End
	}) {
		return
	}
	end := n.Path[0].Span().End
	c.report(lint.CodeNixPkgsLib, syntax.Span{Start: at, End: end}, "`lib` is in scope here, so `pkgs.lib` is the long way to it", c.offer("Use `lib`", lint.Review, textedit.Edit{Start: at, End: end, NewText: libName}))
}

type flakeInput struct {
	name string
	attr syntax.Attr
}

func eachLeaf(bindings []syntax.Binding, prefix syntax.AttrPath, visit func(syntax.AttrPath, syntax.Expr)) {
	for _, b := range bindings {
		a, ok := b.(*syntax.Assign)
		if !ok {
			continue
		}
		path := append(slices.Clone(prefix), a.Path...)
		if set, nested := syntax.Unparenthesized(a.Value).(*syntax.AttrSet); nested && !set.Rec {
			eachLeaf(set.Bindings, path, visit)
			continue
		}
		visit(path, a.Value)
	}
}

func eachInputLeaf(root syntax.Expr, visit func(syntax.AttrPath, syntax.Expr)) {
	set, ok := root.(*syntax.AttrSet)
	if !ok {
		return
	}
	eachLeaf(set.Bindings, nil, func(path syntax.AttrPath, value syntax.Expr) {
		if len(path) < 2 {
			return
		}
		if name, static := syntax.AttrName(path[0]); static && name == inputsAttr {
			visit(path[1:], value)
		}
	})
}

func flakeInputsOf(root syntax.Expr) (declared []flakeInput, follows []string) {
	seen := map[string]bool{}
	eachInputLeaf(root, func(path syntax.AttrPath, value syntax.Expr) {
		name, static := syntax.AttrName(path[0])
		if !static {
			return
		}
		if !seen[name] {
			seen[name] = true
			declared = append(declared, flakeInput{name: name, attr: path[0]})
		}
		if last, static := syntax.AttrName(path[len(path)-1]); !static || last != followsAttr {
			return
		}
		if s, ok := syntax.Unparenthesized(value).(*syntax.String); ok {
			if target, plain := syntax.StringValue(s); plain {
				follows = append(follows, target)
			}
		}
	})
	return declared, follows
}

func (c *checker) unusedFlakeInputs() {
	declared, follows := flakeInputsOf(c.tree.Root)
	if len(declared) == 0 {
		return
	}
	used, decidable := c.inputsRead()
	if !decidable {
		return
	}
	for _, target := range follows {
		head, _, _ := strings.Cut(target, flakeSeparator)
		used[head] = true
	}
	for _, input := range declared {
		if !used[input.name] {
			c.report(lint.CodeNixUnusedFlakeInput, input.attr.Span(), fmt.Sprintf("the flake declares the input `%s`, but neither `outputs` nor a `follows` reads it", input.name), nil)
		}
	}
}

func (c *checker) inputsRead() (map[string]bool, bool) {
	if c.outputs == nil {
		return nil, false
	}
	used := map[string]bool{}
	var defs []*scope.Def
	switch p := c.outputs.Param.(type) {
	case *syntax.IdentParam:
		d, bound := c.table.Binder(p.Name)
		if !bound {
			return nil, false
		}
		defs = append(defs, d)
	case *syntax.PatternParam:
		for _, f := range p.Formals {
			d, bound := c.table.Binder(f.Name)
			if !bound {
				return nil, false
			}
			if f.Name.Name == selfFormal {
				if !c.selfKeepsInputs(d) {
					return nil, false
				}
				continue
			}
			if d.Used() {
				used[f.Name.Name] = true
			}
		}
		if p.Alias == nil {
			return used, true
		}
		d, bound := c.table.Binder(p.Alias)
		if !bound {
			return nil, false
		}
		defs = append(defs, d)
	}
	for _, d := range defs {
		if !c.selectedInputs(d, used) {
			return nil, false
		}
	}
	return used, true
}

func (c *checker) selfKeepsInputs(self *scope.Def) bool {
	parents := c.selectsOf(self)
	for _, u := range self.Uses() {
		s, selected := parents[u.Node]
		if !selected {
			return false
		}
		if name, static := syntax.AttrName(s.Path[0]); !static || name == inputsAttr {
			return false
		}
	}
	return true
}

func (c *checker) selectedInputs(d *scope.Def, used map[string]bool) bool {
	parents := c.selectsOf(d)
	for _, u := range d.Uses() {
		s, selected := parents[u.Node]
		if !selected {
			return false
		}
		name, static := syntax.AttrName(s.Path[0])
		if !static {
			return false
		}
		used[name] = true
	}
	return true
}

func (c *checker) selectsOf(d *scope.Def) map[syntax.Node]*syntax.Select {
	out := map[syntax.Node]*syntax.Select{}
	syntax.Inspect(c.tree.Root, func(n syntax.Node) bool {
		if s, ok := n.(*syntax.Select); ok && len(s.Path) > 0 {
			if id, isIdent := s.Target.(*syntax.Ident); isIdent {
				if ref, resolved := c.table.Of(id); resolved && ref.Def == d {
					out[id] = s
				}
			}
		}
		return true
	})
	return out
}
