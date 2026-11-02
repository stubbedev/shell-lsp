package nix

import (
	"cmp"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func linkCalls(tree *syntax.File, ix *model.Index) {
	refs := map[model.Span]int{}
	for i, r := range ix.Refs {
		if r.Kind == model.KindFunction {
			refs[r.Span] = i
		}
	}
	var callees []model.Span
	visit := func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Apply:
			callees = appendCallee(callees, n.Fn)
		case *syntax.Binary:
			if n.Op == syntax.OpPipeInto {
				callees = appendCallee(callees, n.Right)
			}
			if n.Op == syntax.OpPipeFrom {
				callees = appendCallee(callees, n.Left)
			}
		}
		return true
	}
	syntax.Inspect(tree.Root, visit)
	for _, stray := range tree.Stray {
		syntax.Inspect(stray, visit)
	}
	slices.SortFunc(callees, func(a, b model.Span) int { return cmp.Compare(a.Start, b.Start) })
	functions := functionSymbols(ix)
	var open []int
	next := 0
	for _, callee := range callees {
		for ; next < len(functions) && ix.Symbols[functions[next]].Span.Start <= callee.Start; next++ {
			open = closed(open, ix, ix.Symbols[functions[next]].Span.Start)
			open = append(open, functions[next])
		}
		open = closed(open, ix, callee.Start)
		i, isRef := refs[callee]
		if isRef && len(open) > 0 {
			ix.Refs[i].Owner = open[len(open)-1]
		}
	}
}

func appendCallee(out []model.Span, fn syntax.Expr) []model.Span {
	switch fn := syntax.Unparenthesized(fn).(type) {
	case *syntax.Ident:
		return append(out, fn.Span())
	case *syntax.Select:
		if fn.Default == nil {
			return append(out, nameSpan(fn.Path[len(fn.Path)-1]))
		}
	}
	return out
}

func functionSymbols(ix *model.Index) []int {
	var out []int
	for i, s := range ix.Symbols {
		if s.Kind == model.KindFunction {
			out = append(out, i)
		}
	}
	slices.SortStableFunc(out, func(a, b int) int {
		x, y := ix.Symbols[a].Span, ix.Symbols[b].Span
		return cmp.Or(cmp.Compare(x.Start, y.Start), cmp.Compare(y.End, x.End))
	})
	return out
}

func closed(open []int, ix *model.Index, at int) []int {
	for len(open) > 0 && ix.Symbols[open[len(open)-1]].Span.End <= at {
		open = open[:len(open)-1]
	}
	return open
}
