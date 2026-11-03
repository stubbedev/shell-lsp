package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type keyCandidate struct {
	start int
	path  []string
}

type MemberRequest struct {
	word     syntax.Span
	target   syntax.Span
	path     []string
	selected bool
	sets     []keyCandidate
}

func (c Cursor) MembersWanted() (MemberRequest, bool) {
	leaf, isAttr := c.ancestor(0).(*syntax.IdentAttr)
	if !isAttr {
		return MemberRequest{}, false
	}
	switch parent := c.ancestor(1).(type) {
	case *syntax.Select:
		return selectedMembers(c.cursor, parent.Target, parent.Path, leaf)
	case *syntax.HasAttr:
		return selectedMembers(c.cursor, parent.Target, parent.Path, leaf)
	case *syntax.Assign:
		sets := keyCandidates(c.cursor, parent, leaf)
		return MemberRequest{word: c.word, sets: sets}, len(sets) > 0
	}
	return MemberRequest{}, false
}

func selectedMembers(c *cursor, target syntax.Expr, path syntax.AttrPath, leaf syntax.Attr) (MemberRequest, bool) {
	i := slices.IndexFunc(path, func(a syntax.Attr) bool { return a == leaf })
	names, static := attrNames(path[:i])
	if !static {
		return MemberRequest{}, false
	}
	return MemberRequest{word: c.word, target: syntax.Unparenthesized(target).Span(), path: names, selected: true}, true
}

func (c Cursor) LibraryMembersWanted() ([]string, syntax.Span, bool) {
	leaf, isAttr := c.ancestor(0).(*syntax.IdentAttr)
	if !isAttr {
		return nil, syntax.Span{}, false
	}
	var target syntax.Expr
	var names []string
	switch parent := c.ancestor(1).(type) {
	case *syntax.Select:
		i := slices.IndexFunc(parent.Path, func(a syntax.Attr) bool { return a == syntax.Attr(leaf) })
		selected, static := attrNames(parent.Path[:i])
		if !static {
			return nil, syntax.Span{}, false
		}
		target, names = parent.Target, selected
	case *syntax.Inherit:
		if parent.From == nil {
			return nil, syntax.Span{}, false
		}
		target = parent.From.Inner
	default:
		return nil, syntax.Span{}, false
	}
	root, known := c.rootOf(c.chain, target, maxRootHops)
	if !known || !root.nixpkgs {
		return nil, syntax.Span{}, false
	}
	full := slices.Concat(root.path, names)
	if len(full) == 0 || full[0] != LibName {
		return nil, syntax.Span{}, false
	}
	return full[1:], c.word, true
}

func (c Cursor) LibraryWiths() ([][]string, syntax.Span) {
	if _, isName := c.ancestor(0).(*syntax.Ident); !isName {
		return nil, syntax.Span{}
	}
	var out [][]string
	for _, with := range c.table.NamesAt(c.word.Start + 1).Withs {
		if root, known := c.rootOf(c.chain, with.Scope, maxRootHops); known && root.nixpkgs && len(root.path) > 0 && root.path[0] == LibName {
			out = append(out, root.path[1:])
		}
	}
	return out, c.word
}

func LibraryItems(names []string, parent []string, word syntax.Span) []Item {
	out := make([]Item, len(names))
	for i, name := range names {
		out[i] = Item{Label: attrLabel(name), Kind: model.KindAttribute, Detail: LibName + "." + strings.Join(append(slices.Clip(parent), name), "."), Span: word}
	}
	return out
}

func keyCandidates(c *cursor, assign *syntax.Assign, leaf syntax.Attr) []keyCandidate {
	i := slices.IndexFunc(assign.Path, func(a syntax.Attr) bool { return a == leaf })
	path, static := attrNames(assign.Path[:i])
	if !static {
		return nil
	}
	return c.setsAround(nil, path, assign, 2)
}

func (c *cursor) formalSlotSets(lambda *syntax.Lambda) []keyCandidate {
	return c.setsAround([]keyCandidate{{start: lambda.Span().Start}}, nil, lambda, 4)
}

func (c *cursor) setsAround(out []keyCandidate, path []string, child syntax.Node, from int) []keyCandidate {
	for depth := from; ; depth++ {
		switch n := c.ancestor(depth).(type) {
		case *syntax.AttrSet:
			out = append(out, keyCandidate{start: n.Span().Start, path: slices.Clone(path)})
		case *syntax.Assign:
			if n.Value != child {
				return out
			}
			names, static := attrNames(n.Path)
			if !static {
				return out
			}
			path = slices.Concat(names, path)
		case *syntax.Paren:
		case *syntax.Lambda:
			if n.Body != child {
				return out
			}
		case *syntax.Let:
			if n.Body != child {
				return out
			}
		case *syntax.With:
			if n.Body != child {
				return out
			}
		case *syntax.Apply:
			if n.Args[len(n.Args)-1] != child || !slices.Contains(moduleWrappers, callName(c.table, n.Fn)) {
				return out
			}
		case *syntax.Binary:
			if n.Op != syntax.OpUpdate {
				return out
			}
		default:
			return out
		}
		child = c.ancestor(depth)
	}
}

func exprSpanned(tree *syntax.File, span syntax.Span) syntax.Expr {
	var out syntax.Expr
	for _, n := range chainTo(tree, span) {
		if e, isExpr := n.(syntax.Expr); isExpr && e.Span() == span {
			out = e
		}
	}
	return out
}

func setStarting(u *symbolTable, start int) *attrs {
	for _, n := range chainTo(u.f.tree, syntax.Span{Start: start, End: start + 1}) {
		if set, isSet := n.(*syntax.AttrSet); isSet && set.Span().Start == start {
			return u.sets[set]
		}
	}
	return nil
}

func (p *Project) Members(path string, req MemberRequest) []Item {
	p.begin()
	defer p.mu.Unlock()
	file, ok := p.byPath[path]
	if !ok {
		return nil
	}
	u := p.units[file]
	names := map[string]bool{}
	if req.selected {
		if target := exprSpanned(u.f.tree, req.target); target != nil {
			values := p.eval(u, target)
			for _, name := range req.path {
				values = p.selectAll(values, name)
			}
			for _, v := range values {
				p.memberNames(v, names)
			}
		}
	}
	for _, candidate := range req.sets {
		set := setStarting(u, candidate.start)
		if set == nil {
			continue
		}
		members := p.spacesOfSet(file, set)
		for _, s := range members {
			keys := candidate.path
			if len(keys) > 0 && set.kids[keys[0]] != nil && (keys[0] == configFormal || keys[0] == optionsFormal) && !slices.Contains(s.path(), keys[0]) {
				keys = keys[1:]
			}
			descended(s, keys, names)
		}
		if len(members) > 0 {
			break
		}
	}
	out := make([]Item, 0, len(names))
	for _, name := range sortedNames(names) {
		out = append(out, Item{Label: attrLabel(name), Kind: model.KindAttribute, Detail: "from this project", Span: req.word})
	}
	return out
}

func (p *Project) spacesOfSet(file int, set *attrs) []*space {
	sets := []*attrs{set}
	for _, name := range []string{configFormal, optionsFormal} {
		if kid := set.kids[name]; kid != nil {
			sets = append(sets, kid)
		}
	}
	var out []*space
	for _, at := range sets {
		for _, m := range p.memberships[model.Home{File: file, Scope: at.id}] {
			if !m.formals && !slices.Contains(out, m.space) {
				out = append(out, m.space)
			}
		}
	}
	return out
}

func descended(s *space, path []string, names map[string]bool) {
	for _, name := range path {
		kid := s.kids[name]
		if kid == nil {
			return
		}
		s = kid
	}
	spaceNames(s, names)
}

func spaceNames(s *space, names map[string]bool) {
	for name := range s.names {
		names[name] = true
	}
	for name, kid := range s.kids {
		if name != anyName && name != moduleArgsSet && kid.populated() {
			names[name] = true
		}
	}
}

func (s *space) populated() bool {
	if s.declared || len(s.bound) > 0 || len(s.members) > 0 {
		return true
	}
	for _, kid := range s.kids {
		if kid.populated() {
			return true
		}
	}
	return false
}

func (p *Project) memberNames(v flowValue, names map[string]bool) {
	if !p.enter(v) {
		return
	}
	defer delete(p.expanding, v)
	switch v := v.(type) {
	case flowSet:
		for _, name := range v.set.names.keys {
			names[name] = true
		}
	case flowSpace:
		spaceNames(v.space, names)
		p.boundNames(v.space, names)
	case flowPrimed:
		spaceNames(v.space, names)
	case flowSelf:
		for _, value := range p.selfValues() {
			p.memberNames(value, names)
		}
	case flowPkgs:
		for _, body := range p.overlayBodies() {
			p.memberNames(body, names)
		}
	case flowUpdate:
		for _, side := range []syntax.Expr{v.node.Left, v.node.Right} {
			for _, value := range p.eval(v.unit, side) {
				p.memberNames(value, names)
			}
		}
	case flowMapped:
		for _, value := range p.eval(v.unit, v.call.Args[1]) {
			p.memberNames(value, names)
		}
	}
}

func (p *Project) boundNames(s *space, names map[string]bool) {
	for _, b := range s.bound {
		for _, value := range p.evalSource(b.unit, b.source) {
			p.memberNames(value, names)
		}
	}
}

func (p *Project) ModuleSites(path string, start int) []OptionSite {
	p.begin()
	defer p.mu.Unlock()
	file, ok := p.byPath[path]
	if !ok {
		return nil
	}
	set := setStarting(p.units[file], start)
	if set == nil {
		return nil
	}
	var out []OptionSite
	for _, s := range p.spacesOfSet(file, set) {
		root := s
		for root.parent != nil {
			root = root.parent
		}
		for _, name := range sortedNames(p.classes) {
			if p.classes[name].root == root && optionClassNamed(name) != ClassUnknown {
				out = append(out, OptionSite{Class: optionClassNamed(name), Path: s.path()})
			}
		}
	}
	return out
}

func (r *reading) moduleSites(chain []syntax.Node, end int, known Evaluated) []OptionSite {
	for i := end - 1; i >= 0; i-- {
		set, isSet := chain[i].(*syntax.AttrSet)
		if !isSet {
			continue
		}
		bases := known.ModuleSites(set.Span().Start)
		if len(bases) == 0 {
			continue
		}
		var out []OptionSite
		for _, base := range bases {
			w := optionWalk{table: r.table, mode: walkModule, class: base.Class, path: base.Path, top: len(base.Path) == 0}
			for j := i; j < end; j++ {
				w.step(chain[j], chain[j+1])
			}
			if w.mode == walkModule {
				out = append(out, OptionSite{Class: w.class, Path: w.path})
			}
		}
		return out
	}
	return nil
}
