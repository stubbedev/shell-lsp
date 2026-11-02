package nix

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

type ownedRegion struct {
	span  syntax.Span
	owner int
}

func underscored(name string) bool {
	return name != scope.CurPos && strings.HasPrefix(name, unusedPrefix)
}

func unusedCode(name string, named lint.Code) lint.Code {
	if underscored(name) {
		return lint.CodeNixUnusedUnderscore
	}
	return named
}

func deadAmong(defs []*scope.Def, body int, regions func() []ownedRegion) []bool {
	allLive := true
	for _, d := range defs {
		if !d.Implicit && !usedAtOrAfter(d, body) {
			allLive = false
			break
		}
	}
	if allLive {
		return nil
	}
	owned := regions()
	slices.SortStableFunc(owned, func(a, b ownedRegion) int { return cmp.Compare(a.span.Start, b.span.Start) })
	live := make([]bool, len(defs))
	var edges [][2]int
	for i, d := range defs {
		live[i] = d.Implicit
		for _, u := range d.Uses() {
			at := u.Node.Span().Start
			owners := regionsAt(owned, at)
			if len(owners) == 0 {
				live[i] = true
			}
			for _, r := range owners {
				edges = append(edges, [2]int{r.owner, i})
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, e := range edges {
			if live[e[0]] && !live[e[1]] {
				live[e[1]] = true
				changed = true
			}
		}
	}
	dead := make([]bool, len(defs))
	for i := range dead {
		dead[i] = !live[i]
	}
	return dead
}

func usedAtOrAfter(d *scope.Def, at int) bool {
	uses := d.Uses()
	return len(uses) > 0 && uses[len(uses)-1].Node.Span().Start >= at
}

func regionsAt(owned []ownedRegion, at int) []ownedRegion {
	end, _ := slices.BinarySearchFunc(owned, at, func(r ownedRegion, at int) int {
		if r.span.Start <= at {
			return -1
		}
		return 1
	})
	start := end
	for start > 0 && owned[start-1].span.End > at {
		start--
	}
	return owned[start:end]
}

func (c *checker) unusedLets() {
	for _, d := range c.table.Defs() {
		if d.Scope.Kind == scope.LetScope && d == d.Scope.Defs[0] {
			c.unusedInLet(d.Scope)
		}
	}
}

func (c *checker) unusedInLet(s *scope.Scope) {
	body := 0
	for _, d := range s.Defs {
		for _, b := range d.Bindings {
			body = max(body, b.Span().End)
		}
	}
	dead := deadAmong(s.Defs, body, func() []ownedRegion { return letRegions(s.Defs) })
	for i, d := range s.Defs {
		c.usedUnderscore(d)
		if len(dead) == 0 || !dead[i] {
			continue
		}
		var fixes []lint.Fix
		if !readByOthers(d) {
			fixes = c.removal(d)
		}
		c.report(unusedCode(d.Name, lint.CodeNixUnusedLet), d.Binders[0].Span(), fmt.Sprintf("`%s` is bound but nothing that is read reads it", d.Name), fixes)
	}
}

func readByOthers(d *scope.Def) bool {
	for _, u := range d.Uses() {
		at := u.Node.Span().Start
		if !slices.ContainsFunc(d.Bindings, func(b syntax.Binding) bool { return removedWith(b, at) }) {
			return true
		}
	}
	return false
}

func removedWith(b syntax.Binding, at int) bool {
	if i, inherits := b.(*syntax.Inherit); inherits && len(i.Attrs) > 1 {
		return false
	}
	span := b.Span()
	return span.Start <= at && at < span.End
}

func letRegions(defs []*scope.Def) []ownedRegion {
	var out []ownedRegion
	for i, d := range defs {
		for _, b := range d.Bindings {
			switch b := b.(type) {
			case *syntax.Assign:
				out = append(out, ownedRegion{span: b.Span(), owner: i})
			case *syntax.Inherit:
				if b.From != nil {
					out = append(out, ownedRegion{span: b.From.Span(), owner: i})
				}
			}
		}
	}
	return out
}

func (c *checker) usedUnderscore(d *scope.Def) {
	if c.optional && underscored(d.Name) && d.Used() && !d.Implicit {
		c.report(lint.CodeNixUsedUnderscore, d.Binders[0].Span(), fmt.Sprintf("`%s` starts with `_`, which says it goes unread, but it is read", d.Name), nil)
	}
}

func (c *checker) deadInPattern(p *syntax.PatternParam, defs []*scope.Def) []bool {
	return deadAmong(defs, p.Span().End, func() []ownedRegion {
		var out []ownedRegion
		for _, f := range p.Formals {
			if f.Default == nil {
				continue
			}
			if d, bound := c.table.Binder(f.Name); bound {
				if i := slices.Index(defs, d); i >= 0 {
					out = append(out, ownedRegion{span: f.Default.Span(), owner: i})
				}
			}
		}
		return out
	})
}
