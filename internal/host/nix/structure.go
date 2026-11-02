package nix

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func letBlock(n *syntax.Let) model.Span {
	span := n.Span()
	if count := len(n.Bindings); count > 0 {
		span.End = n.Bindings[count-1].Span().End
	}
	return span
}

func folds(tree *syntax.File) []model.Fold {
	var out []model.Fold
	visit := func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.AttrSet, *syntax.List, *syntax.String, *syntax.Lambda, *syntax.Paren, *syntax.Interpolation, *syntax.PatternParam, *syntax.Inherit, *syntax.LegacyLet:
			out = append(out, model.Fold{Span: n.Span()})
		case *syntax.Let:
			out = append(out, model.Fold{Span: letBlock(n)})
		}
		return true
	}
	syntax.Inspect(tree.Root, visit)
	for _, stray := range tree.Stray {
		syntax.Inspect(stray, visit)
	}
	return append(out, commentRuns(tree)...)
}

func commentRuns(tree *syntax.File) []model.Fold {
	var out []model.Fold
	for i := 0; i < len(tree.Comments); i++ {
		run := tree.Comments[i].Span
		for i+1 < len(tree.Comments) && adjacent(tree, tree.Comments[i], tree.Comments[i+1]) {
			i++
			run.End = tree.Comments[i].Span.End
		}
		out = append(out, model.Fold{Span: run, Comment: true})
	}
	return out
}

func adjacent(tree *syntax.File, a, b syntax.Comment) bool {
	if a.Kind != syntax.LineComment || b.Kind != syntax.LineComment {
		return false
	}
	gap := tree.Text[a.Span.End:b.Span.Start]
	return strings.TrimSpace(gap) == "" && strings.Count(gap, "\n") == 1
}

func (a Analysis) Enclosing(offset int) []model.Span {
	spans := []model.Span{{Start: 0, End: len(a.tree.Text)}}
	visit := func(n syntax.Node) bool {
		span := n.Span()
		if !span.Touches(offset) {
			return false
		}
		spans = append(spans, span)
		switch n := n.(type) {
		case *syntax.String:
			spans = append(spans, n.Content)
		case *syntax.Assign:
			spans = append(spans, model.Span{Start: n.Path[0].Span().Start, End: n.Path[len(n.Path)-1].Span().End})
		}
		return true
	}
	syntax.Inspect(a.tree.Root, visit)
	for _, stray := range a.tree.Stray {
		syntax.Inspect(stray, visit)
	}
	for _, c := range a.tree.Comments {
		if c.Span.Touches(offset) {
			spans = append(spans, c.Span)
		}
	}
	return model.Nested(offset, spans)
}
