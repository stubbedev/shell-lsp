package parser

import (
	"iter"
	"slices"
	"sort"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func (ix *Index) ScopeAt(offset int) ScopeID {
	last := sort.Search(len(ix.Scopes), func(i int) bool { return ix.Scopes[i].Span.Start > offset }) - 1
	if last <= int(FileScope) {
		return FileScope
	}
	s := ScopeID(last)
	for s != FileScope && !ix.Scopes[s].Span.Contains(offset) {
		s = ix.Scopes[s].Parent
	}
	return s
}

func (ix *Index) Binding(name string, scope ScopeID) (ScopeID, bool) {
	locals := ix.localScopes[name]
	for s := scope; ; s = ix.Scopes[s].Parent {
		if locals[s] {
			return s, true
		}
		if s == FileScope {
			return FileScope, false
		}
	}
}

type Definitions struct {
	all       []Definition
	positions []int
}

func (d Definitions) Len() int { return len(d.positions) }

func (d Definitions) At(i int) *Definition { return &d.all[d.positions[i]] }

func (d Definitions) All() iter.Seq[*Definition] {
	return func(yield func(*Definition) bool) {
		for _, p := range d.positions {
			if !yield(&d.all[p]) {
				return
			}
		}
	}
}

func (d Definitions) Backward() iter.Seq[*Definition] {
	return func(yield func(*Definition) bool) {
		for _, p := range slices.Backward(d.positions) {
			if !yield(&d.all[p]) {
				return
			}
		}
	}
}

func (ix *Index) visibleBinding(name string, scope ScopeID) binding {
	bound, local := ix.Binding(name, scope)
	key := binding{name: name}
	if local {
		key.scope, key.local = bound, true
	}
	return key
}

func (ix *Index) Visible(name string, scope ScopeID) Definitions {
	return Definitions{all: ix.Definitions, positions: ix.definitionsByBinding[ix.visibleBinding(name, scope)]}
}

func (ix *Index) VisibleAttrs(name string, scope ScopeID) Attr {
	return ix.attrsByBinding[ix.visibleBinding(name, scope)]
}

func (ix *Index) Defines(name string) bool {
	_, ok := ix.attrsByName[name]
	return ok
}

func (ix *Index) AttrsAnywhere(name string) Attr {
	return ix.attrsByName[name]
}

func (ix *Index) DefinitionsIn(scope ScopeID) Definitions {
	return Definitions{all: ix.Definitions, positions: ix.definitionsByScope[scope]}
}

func (ix *Index) FunctionNamed(name string) (Function, bool) {
	i, ok := ix.functionsByName[name]
	if !ok {
		return Function{}, false
	}
	return ix.Functions[i], true
}

func (ix *Index) EnclosingFunction(scope ScopeID) (Function, bool) {
	i := slices.IndexFunc(ix.Functions, func(f Function) bool { return f.Body == scope })
	if i < 0 {
		return Function{}, false
	}
	return ix.Functions[i], true
}

func (t *Tree) PathAt(offset int) []syntax.Node {
	var path []syntax.Node
	Walk(t.File, func(node syntax.Node) bool {
		if node == nil {
			return false
		}
		span, ok := t.NodeSpan(node)
		if !ok || !span.Contains(offset) {
			_, isFile := node.(*syntax.File)
			return isFile
		}
		path = append(path, node)
		return true
	})
	return path
}

func (t *Tree) Text() string { return t.text }

func (t *Tree) Line(offset int) int {
	return sort.SearchInts(t.lineStarts, offset+1)
}

func (t *Tree) NodeSpan(node syntax.Node) (Span, bool) {
	if w, ok := node.(*syntax.Word); ok && len(w.Parts) == 0 {
		return Span{}, false
	}
	if stmt, ok := node.(*syntax.Stmt); ok && stmt.Cmd == nil && len(stmt.Redirs) == 0 {
		return Span{}, false
	}
	start := node.Pos()
	if !start.IsValid() {
		return Span{}, false
	}
	end := node.End()
	if !end.IsValid() {
		return Span{Start: offset(start), End: len(t.text)}, true
	}
	return Span{Start: offset(start), End: max(offset(start), offset(end))}, true
}

func (t *Tree) LiteralKeys(name string) []string {
	pairs := t.Dialect.Features().AssociativePairs && t.associative(name)
	var keys []string
	add := func(index syntax.ArithmExpr) {
		w, ok := index.(*syntax.Word)
		if !ok {
			return
		}
		if key, ok := Literal(w); ok && key != "" && key != "@" && key != "*" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	Walk(t.File, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Assign:
			if n.Name == nil || n.Name.Value != name {
				return true
			}
			if n.Index != nil {
				add(n.Index)
			}
			if n.Array == nil {
				return true
			}
			for i, elem := range n.Array.Elems {
				switch {
				case elem.Index != nil:
					add(elem.Index)
				case pairs && i%2 == 0 && elem.Value != nil:
					add(elem.Value)
				}
			}
		case *syntax.ParamExp:
			if n.Param != nil && n.Param.Value == name && n.Index != nil {
				add(n.Index)
			}
		}
		return true
	})
	return keys
}

func (t *Tree) associative(name string) bool {
	return slices.ContainsFunc(t.Definitions, func(d Definition) bool {
		return d.Name == name && d.Attrs.Has(AttrAssoc)
	})
}
