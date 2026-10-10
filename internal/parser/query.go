package parser

import (
	"slices"

	"mvdan.cc/sh/v3/syntax"
)

func (ix *Index) ScopeAt(offset int) ScopeID {
	best := FileScope
	for i, scope := range ix.Scopes {
		if i == int(FileScope) || !scope.Span.Contains(offset) {
			continue
		}
		if scope.Span.End-scope.Span.Start <= ix.Scopes[best].Span.End-ix.Scopes[best].Span.Start {
			best = ScopeID(i)
		}
	}
	return best
}

func (ix *Index) Binding(name string, scope ScopeID) (ScopeID, bool) {
	for s := scope; ; s = ix.Scopes[s].Parent {
		if slices.ContainsFunc(ix.Definitions, func(d Definition) bool { return d.Name == name && d.Local && d.Within == s }) {
			return s, true
		}
		if s == FileScope {
			return FileScope, false
		}
	}
}

func (ix *Index) Visible(name string, scope ScopeID) []Definition {
	binding, local := ix.Binding(name, scope)
	var defs []Definition
	for _, def := range ix.Definitions {
		if def.Name != name {
			continue
		}
		if local && def.Local && def.Within == binding || !local && !def.Local {
			defs = append(defs, def)
		}
	}
	return defs
}

func (ix *Index) FunctionNamed(name string) (Function, bool) {
	i := slices.IndexFunc(ix.Functions, func(f Function) bool { return f.Name == name })
	if i < 0 {
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
	syntax.Walk(t.File, func(node syntax.Node) bool {
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
