package scope

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type CaptureKind uint8

const (
	AlreadyBound CaptureKind = iota
	CapturedRead
	ShadowedRead
)

type Capture struct {
	Kind CaptureKind
	Node syntax.Node
}

func Reserved(name string) bool {
	return name == CurPos || name == overrides || slices.Contains(evaluatorReads, name)
}

func (s *Scope) binding(name string) *Def {
	i := slices.IndexFunc(s.Defs, func(d *Def) bool { return d.Name == name })
	if i < 0 {
		return nil
	}
	return s.Defs[i]
}

func (t *Table) Capture(d *Def, name string) (Capture, bool) {
	if other := d.Scope.binding(name); other != nil {
		return Capture{Kind: AlreadyBound, Node: other.Binders[0]}, true
	}
	for _, u := range d.uses {
		if t.captured(u, d.Scope, name) {
			return Capture{Kind: CapturedRead, Node: u.Node}, true
		}
	}
	for _, u := range t.uses {
		if u.Name == name && t.seenFrom(u, d.Scope) {
			return Capture{Kind: ShadowedRead, Node: u.Node}, true
		}
	}
	return Capture{}, false
}

func (t *Table) captured(u Use, until *Scope, name string) bool {
	if binder, ok := t.binders[u.Node]; ok && binder.Scope.binding(name) != nil {
		return true
	}
	for s := t.ScopeAt(u.Node.Span().Start); s != nil && s != until; s = s.Parent {
		if s.binding(name) != nil {
			return true
		}
	}
	return false
}

func (t *Table) seenFrom(u Use, target *Scope) bool {
	for s := t.ScopeAt(u.Node.Span().Start); s != nil; s = s.Parent {
		if s == target {
			return true
		}
		if s.binding(u.Name) != nil {
			return false
		}
	}
	return false
}
