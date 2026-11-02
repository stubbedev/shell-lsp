package nix

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

func (c *checker) offer(title string, safety lint.Safety, edits ...textedit.Edit) []lint.Fix {
	for _, e := range edits {
		if c.commented(e.Start, e.End) {
			return nil
		}
	}
	return []lint.Fix{{Title: title, Safety: safety, Edits: edits}}
}

func (c *checker) commented(start, end int) bool {
	comments := c.tree.Comments
	i, _ := slices.BinarySearchFunc(comments, start, func(comment syntax.Comment, at int) int {
		return cmp.Compare(comment.Span.End, at+1)
	})
	return i < len(comments) && comments[i].Span.Start < end
}

func (c *checker) textOf(span syntax.Span) string { return c.tree.Text[span.Start:span.End] }

func (c *checker) skipBlank(at int) int {
	return skipBlank(c.tree.Text, at)
}

func skipBlank(text string, at int) int {
	return len(text) - len(strings.TrimLeft(text[at:], " \t\r\n"))
}

func (c *checker) separated(span syntax.Span) bool {
	text := c.tree.Text
	before := span.Start == 0 || strings.IndexByte(" \t\r\n([{=;", text[span.Start-1]) >= 0
	after := span.End == len(text) || strings.IndexByte(" \t\r\n)]},;", text[span.End]) >= 0
	return before && after
}

func (c *checker) withoutParens(p *syntax.Paren) []textedit.Edit {
	span, inner := p.Span(), p.Inner.Span()
	openEnd, closeStart := span.Start+1, span.End-1
	if strings.TrimSpace(c.textOf(syntax.Span{Start: openEnd, End: inner.Start})) == "" {
		openEnd = inner.Start
	}
	if strings.TrimSpace(c.textOf(syntax.Span{Start: inner.End, End: closeStart})) == "" {
		closeStart = inner.End
	}
	return []textedit.Edit{{Start: span.Start, End: openEnd}, {Start: closeStart, End: span.End}}
}

func (c *checker) removal(d *scope.Def) []lint.Fix {
	edits := make([]textedit.Edit, 0, len(d.Bindings))
	for _, b := range d.Bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			edits = append(edits, c.statementRemoval(b.Span()))
		case *syntax.Inherit:
			edits = append(edits, c.inheritedRemoval(b, d))
		}
	}
	return c.offer("Remove `"+d.Name+"`", lint.Safe, edits...)
}

func (c *checker) statementRemoval(span syntax.Span) textedit.Edit {
	text := c.tree.Text
	lineStart := textline.Start(text, span.Start)
	lineEnd := textline.End(text, span.End)
	if strings.TrimSpace(text[lineStart:span.Start]) == "" && strings.TrimSpace(text[span.End:lineEnd]) == "" {
		return textedit.Edit{Start: lineStart, End: textline.Next(text, span.End)}
	}
	return textedit.Edit{Start: span.Start, End: span.End + len(text[span.End:]) - len(strings.TrimLeft(text[span.End:], " \t"))}
}

func (c *checker) inheritedRemoval(stmt *syntax.Inherit, d *scope.Def) textedit.Edit {
	if len(stmt.Attrs) == 1 {
		return c.statementRemoval(stmt.Span())
	}
	i := slices.IndexFunc(stmt.Attrs, func(a syntax.Attr) bool {
		owner, bound := c.table.Binder(a)
		return bound && owner == d
	})
	if i == len(stmt.Attrs)-1 {
		return textedit.Edit{Start: stmt.Attrs[i-1].Span().End, End: stmt.Attrs[i].Span().End}
	}
	return textedit.Edit{Start: stmt.Attrs[i].Span().Start, End: stmt.Attrs[i+1].Span().Start}
}

func (c *checker) formalRemoval(p *syntax.PatternParam, i int) textedit.Edit {
	text := c.tree.Text
	span := p.Formals[i].Span()
	if comma := c.skipBlank(span.End); comma < len(text) && text[comma] == ',' {
		return textedit.Edit{Start: span.Start, End: c.skipBlank(comma + 1)}
	}
	if i > 0 {
		return textedit.Edit{Start: p.Formals[i-1].Span().End, End: span.End}
	}
	return textedit.Edit{Start: span.Start, End: c.skipBlank(span.End)}
}

func (c *checker) aliasRemoval(p *syntax.PatternParam) textedit.Edit {
	text := c.tree.Text
	alias := p.Alias.Span()
	if p.AliasFirst {
		return textedit.Edit{Start: alias.Start, End: alias.End + strings.IndexByte(text[alias.End:], '{')}
	}
	return textedit.Edit{Start: strings.LastIndexByte(text[:alias.Start], '}') + 1, End: alias.End}
}
