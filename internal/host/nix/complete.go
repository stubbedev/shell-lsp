package nix

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const cursorName = "shellLspCursor"

var (
	fullKeywords = []string{"assert", "if", "let", "rec", "with"}
	atomKeywords = []string{"rec"}
	setKeywords  = []string{"inherit"}
	letKeywords  = []string{"in", "inherit"}
)

type Item struct {
	Label  string
	Kind   model.Kind
	Detail string
	Doc    string
	Span   syntax.Span

	packagePath []string
}

func (it Item) HasDoc() bool { return it.Doc != "" || it.packagePath != nil }

func Complete(text string, offset int, files Files, known Evaluated) []Item {
	c, ok := locate(text, offset, files)
	if !ok {
		return nil
	}
	c.known = known
	return c.items()
}

func ItemDoc(text string, offset int, label string, files Files, known Evaluated) (string, bool) {
	items := Complete(text, offset, files, known)
	i := slices.IndexFunc(items, func(it Item) bool { return it.Label == label })
	switch {
	case i < 0:
		return "", false
	case items[i].packagePath != nil:
		return packageDoc(known, items[i].packagePath)
	}
	return items[i].Doc, items[i].Doc != ""
}

func (a Analysis) Expect(offset int) (model.Expect, bool) {
	c, ok := locate(a.tree.Text, offset, NoFiles)
	if !ok {
		return model.Expect{}, false
	}
	items := c.items()
	if len(items) == 0 {
		return model.Expect{}, false
	}
	kinds := make([]model.Kind, len(items))
	for i, it := range items {
		kinds[i] = it.Kind
	}
	slices.Sort(kinds)
	return model.Expect{Kinds: slices.Compact(kinds), Span: c.word, Scope: model.Global}, true
}

type cursor struct {
	*reading
	chain  []syntax.Node
	word   syntax.Span
	spelt  string
	prefix string
	known  Evaluated
}

func locate(text string, offset int, files Files) (*cursor, bool) {
	offset = min(offset, len(text))
	word, ok := wordAt(text, offset)
	if !ok {
		return nil, false
	}
	tree := syntax.Parse(text[:word.Start] + cursorName + text[word.End:])
	chain := cursorChain(tree, word.Start)
	if chain == nil {
		return nil, false
	}
	return &cursor{
		reading: newReading(&Document{tree: tree, table: scope.Resolve(tree)}, chain, files),
		chain:   chain,
		word:    word,
		spelt:   text[word.Start:word.End],
		prefix:  text[word.Start:offset],
		known:   NoEvaluation,
	}, true
}

func wordAt(text string, offset int) (syntax.Span, bool) {
	start := offset
	for start > 0 && syntax.IsNameByte(text[start-1]) {
		start--
	}
	for start < offset && !syntax.IsNameStart(text[start]) {
		start++
	}
	if start == offset && offset > 0 && syntax.IsDigit(text[offset-1]) {
		return syntax.Span{}, false
	}
	end := offset
	for end < len(text) && syntax.IsNameByte(text[end]) {
		end++
	}
	return syntax.Span{Start: start, End: end}, true
}

func cursorChain(tree *syntax.File, at int) []syntax.Node {
	span := syntax.Span{Start: at, End: at + len(cursorName)}
	chain := chainTo(tree, span)
	if len(chain) == 0 {
		return nil
	}
	switch leaf := chain[len(chain)-1].(type) {
	case *syntax.Ident:
		if leaf.Span() == span {
			return chain
		}
	case *syntax.IdentAttr:
		if leaf.Span() == span {
			return chain
		}
	}
	return nil
}

func chainTo(tree *syntax.File, span syntax.Span) []syntax.Node {
	for _, root := range slices.Concat([]syntax.Expr{tree.Root}, tree.Stray) {
		var chain []syntax.Node
		syntax.Inspect(root, func(n syntax.Node) bool {
			s := n.Span()
			if s.Start > span.Start || s.End < span.End {
				return false
			}
			chain = append(chain, n)
			return true
		})
		if len(chain) > 0 {
			return chain
		}
	}
	return nil
}

func (c *cursor) ancestor(n int) syntax.Node {
	i := len(c.chain) - 1 - n
	if i < 0 {
		return nil
	}
	return c.chain[i]
}

func (c *cursor) clean(s string) string { return strings.ReplaceAll(s, cursorName, c.spelt) }

func (c *cursor) items() []Item {
	var candidates []Item
	switch leaf := c.ancestor(0).(type) {
	case *syntax.Ident:
		candidates = c.expression(leaf)
	case *syntax.IdentAttr:
		candidates = c.attribute(leaf)
	default:
		panic("nix: the completion cursor is not a name")
	}
	seen := make(map[string]bool, len(candidates))
	out := make([]Item, 0, len(candidates))
	for _, it := range candidates {
		if seen[it.Label] || it.Label == cursorName {
			continue
		}
		seen[it.Label] = true
		it.Span = c.word
		it.Detail, it.Doc = c.clean(it.Detail), c.clean(it.Doc)
		out = append(out, it)
	}
	slices.SortStableFunc(out, func(a, b Item) int { return cmp.Compare(a.Label, b.Label) })
	return out
}

func (c *cursor) expression(leaf *syntax.Ident) []Item {
	switch parent := c.ancestor(1).(type) {
	case *syntax.IdentParam, *syntax.PatternParam:
		return nil
	case *syntax.Formal:
		if parent.Name == leaf {
			return c.formalSlot()
		}
	}
	return slices.Concat(c.names(), keywordItems(c.expressionKeywords()), keywordItems(c.trailingKeywords()))
}

func (c *cursor) formalSlot() []Item {
	pattern, patterned := c.ancestor(2).(*syntax.PatternParam)
	lambda, ok := c.ancestor(3).(*syntax.Lambda)
	if !patterned || !ok || len(pattern.Formals) != 1 || pattern.Ellipsis || pattern.Alias != nil {
		return nil
	}
	if _, bad := lambda.Body.(*syntax.Bad); !bad {
		return nil
	}
	return slices.Concat(keywordItems(setKeywords), c.formalsAt(4, nil), c.optionKeysAt(len(c.chain)-1-3))
}

func (c *cursor) names() []Item {
	names := c.table.NamesAt(c.word.Start + 1)
	out := make([]Item, 0, len(names.Visible))
	for _, n := range names.Visible {
		switch {
		case n.Ref.Kind == scope.Definition:
			out = append(out, c.defItem(n.Ref.Def))
		case !c.hiddenAlias(n.Name):
			out = append(out, builtinItem(*n.Ref.Builtin))
		}
	}
	for _, with := range names.Withs {
		if v := c.eval(with.Scope); closedValues(v) {
			out = append(out, memberItems(v)...)
			continue
		}
		out = append(out, c.evaluatedMembers(with.Scope, nil)...)
	}
	return out
}

func (c *cursor) hiddenAlias(name string) bool {
	return strings.HasPrefix(name, builtins.HiddenAliasPrefix) && !strings.HasPrefix(c.prefix, builtins.HiddenAliasPrefix)
}

func (c *cursor) expressionKeywords() []string {
	for i := len(c.chain) - 2; i >= 0; i-- {
		child := c.chain[i+1]
		switch parent := c.chain[i].(type) {
		case *syntax.Apply:
			if parent.Fn != child {
				return atomKeywords
			}
		case *syntax.Binary:
			if parent.Left != child {
				return atomKeywords
			}
		case *syntax.Unary, *syntax.List:
			return atomKeywords
		case *syntax.Select:
			if parent.Target == child {
				return nil
			}
			return atomKeywords
		case *syntax.HasAttr:
			return nil
		default:
			return fullKeywords
		}
	}
	return fullKeywords
}

func (c *cursor) trailingKeywords() []string {
	apply, ok := c.ancestor(1).(*syntax.Apply)
	if !ok || apply.Args[len(apply.Args)-1] != c.ancestor(0) {
		return nil
	}
	var out []string
	if selection, ok := apply.Fn.(*syntax.Select); ok && len(apply.Args) == 1 && selection.Default == nil {
		out = append(out, "or")
	}
	var edge syntax.Node = apply
	i := 2
	for binary, ok := c.ancestor(i).(*syntax.Binary); ok && binary.Right == edge; binary, ok = c.ancestor(i).(*syntax.Binary) {
		edge = binary
		i++
	}
	branch, ok := c.ancestor(i).(*syntax.If)
	if !ok {
		return out
	}
	switch edge {
	case branch.Cond:
		if isBad(branch.Then) {
			out = append(out, "then")
		}
	case branch.Then:
		if isBad(branch.Else) {
			out = append(out, "else")
		}
	}
	return out
}

func isBad(e syntax.Expr) bool {
	_, bad := e.(*syntax.Bad)
	return bad
}

func (c *cursor) attribute(leaf *syntax.IdentAttr) []Item {
	switch parent := c.ancestor(1).(type) {
	case *syntax.Select:
		return c.pathMembers(parent.Target, parent.Path, leaf)
	case *syntax.HasAttr:
		return c.pathMembers(parent.Target, parent.Path, leaf)
	case *syntax.Inherit:
		if parent.From != nil {
			if v := c.eval(parent.From.Inner); closedValues(v) {
				return memberItems(v)
			}
			return c.evaluatedMembers(parent.From.Inner, nil)
		}
		return c.names()
	case *syntax.Assign:
		if parent.Path[0] == syntax.Attr(leaf) {
			return slices.Concat(c.keys(), c.optionKeys())
		}
		return c.optionKeys()
	}
	return nil
}

func (c *cursor) pathMembers(target syntax.Expr, path syntax.AttrPath, leaf *syntax.IdentAttr) []Item {
	i := slices.IndexFunc(path, func(a syntax.Attr) bool { return a == syntax.Attr(leaf) })
	p, u := c.linked()
	if v := p.evalPath(u, target, path[:i]); closedValues(v) {
		return memberItems(v)
	}
	names, static := attrNames(path[:i])
	if !static {
		return nil
	}
	return c.evaluatedMembers(target, names)
}

func (c *cursor) keys() []Item {
	switch owner := c.ancestor(2).(type) {
	case *syntax.Let:
		if isBad(owner.Body) {
			return keywordItems(letKeywords)
		}
		return keywordItems(setKeywords)
	case *syntax.AttrSet:
		return slices.Concat(keywordItems(setKeywords), c.passedKeys(owner))
	}
	return nil
}

func (c *cursor) passedKeys(set *syntax.AttrSet) []Item {
	return c.formalsAt(3, boundKeys(set))
}

func (c *cursor) formalsAt(from int, bound map[string]bool) []Item {
	site, ok := c.argumentOf(from)
	if !ok {
		return nil
	}
	p, u := c.linked()
	var out []Item
	for _, v := range p.calleeAt(u, site.apply, site.arg) {
		callee, isLambda := v.(flowLambda)
		if !isLambda {
			continue
		}
		pattern, ok := callee.fn.Param.(*syntax.PatternParam)
		if !ok {
			continue
		}
		tree := callee.unit.f.tree
		for _, f := range pattern.Formals {
			if !bound[f.Name.Name] {
				out = append(out, Item{Label: f.Name.Name, Kind: model.KindAttribute, Detail: formalDetail(tree.Text, f), Doc: commentAbove(tree, f.Span().Start)})
			}
		}
	}
	return out
}

func (c *cursor) argumentOf(from int) (callSite, bool) {
	child := c.ancestor(from - 1)
	for i := from; ; i++ {
		switch parent := c.ancestor(i).(type) {
		case *syntax.Paren:
			child = parent
		case *syntax.Apply:
			arg := slices.IndexFunc(parent.Args, func(e syntax.Expr) bool { return e == child })
			return callSite{apply: parent, arg: arg, depth: len(c.chain) - 1 - i}, arg >= 0
		case *syntax.Binary:
			call, piped := parent.Piped()
			if !piped || call.Args[len(call.Args)-1] != child {
				return callSite{}, false
			}
			return callSite{apply: call, arg: len(call.Args) - 1, depth: len(c.chain) - 1 - i}, true
		default:
			return callSite{}, false
		}
	}
}

func boundKeys(set *syntax.AttrSet) map[string]bool {
	bound := map[string]bool{}
	mark := func(a syntax.Attr) {
		if name, ok := syntax.AttrName(a); ok {
			bound[name] = true
		}
	}
	for _, b := range set.Bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			mark(b.Path[0])
		case *syntax.Inherit:
			for _, a := range b.Attrs {
				mark(a)
			}
		}
	}
	return bound
}

func memberItems(values []flowValue) []Item {
	var out []Item
	for _, v := range values {
		switch v := v.(type) {
		case flowBuiltins:
			for _, b := range builtins.All() {
				if !strings.HasPrefix(b.Name, builtins.HiddenAliasPrefix) {
					out = append(out, builtinItem(b))
				}
			}
		case flowSet:
			out = append(out, setItems(v)...)
		}
	}
	return out
}

func setItems(v flowSet) []Item {
	var out []Item
	for _, s := range v.unit.ix.Symbols {
		if !v.set.names.has(s.Name) || s.Scope != v.set.id || s.Hidden || s.Kind == model.KindModule {
			continue
		}
		kind := model.KindAttribute
		if s.Kind == model.KindFunction {
			kind = model.KindFunction
		}
		out = append(out, Item{Label: attrLabel(s.Name), Kind: kind, Detail: s.Detail, Doc: commentAbove(v.unit.f.tree, s.Span.Start)})
	}
	return out
}

func builtinItem(b builtins.Builtin) Item {
	kind := model.KindVariable
	if len(b.Params) > 0 {
		kind = model.KindFunction
	}
	return Item{Label: b.Name, Kind: kind, Detail: builtinSignature(b), Doc: b.Doc}
}

func builtinSignature(b builtins.Builtin) string {
	return strings.Join(append([]string{b.Name}, b.Params...), " ")
}

func keywordItems(names []string) []Item {
	out := make([]Item, 0, len(names))
	for _, name := range names {
		k, ok := keywordNamed(name)
		if !ok {
			panic("nix: `" + name + "` is not a keyword")
		}
		out = append(out, Item{Label: k.Name, Kind: model.KindKeyword, Doc: k.Doc})
	}
	return out
}

func keywordNamed(name string) (builtins.Keyword, bool) {
	all := builtins.Keywords()
	i := slices.IndexFunc(all, func(k builtins.Keyword) bool { return k.Name == name })
	if i < 0 {
		return builtins.Keyword{}, false
	}
	return all[i], true
}
