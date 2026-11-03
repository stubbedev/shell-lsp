package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/projection"
	shellsyntax "github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	nixMarker = "nix"
	shownHole = "${…}"
)

var nixDelimiters = []string{"NIX", "nix", "EOF_NIX"}

func nixHeredocs(m *projection.Map) []model.Span {
	var out []model.Span
	for _, h := range m.Heredocs() {
		if h.Body.Start == h.Body.End || !opensNix(h) || !m.Literal(h.Body.Start, h.Body.End) {
			continue
		}
		start, end := m.HostSpan(h.Body.Start, h.Body.End)
		out = append(out, model.Span{Start: start, End: end})
	}
	return out
}

func opensNix(h projection.Heredoc) bool {
	if slices.Contains(nixDelimiters, h.Delimiter) {
		return true
	}
	first, _, _ := strings.Cut(strings.TrimSpace(h.Comment), " ")
	return first == nixMarker || writesNixFile(h.Stmt)
}

func writesNixFile(stmt *shellsyntax.Stmt) bool {
	named := func(w *shellsyntax.Word) bool {
		text, literal := parser.Literal(w)
		return literal && strings.HasSuffix(text, nixExtension)
	}
	if slices.ContainsFunc(stmt.Redirs, func(r *shellsyntax.Redirect) bool { return r.Word != nil && named(r.Word) }) {
		return true
	}
	call, isCall := stmt.Cmd.(*shellsyntax.CallExpr)
	return isCall && slices.ContainsFunc(call.Args, named)
}

func (b *symbolTable) embedHeredocs(m *projection.Map) {
	for _, body := range nixHeredocs(m) {
		b.embed(body)
	}
	b.publish()
}

func (b *symbolTable) embed(span model.Span) {
	inner := Analyze(b.f.src[span.Start:span.End])
	shift := func(s model.Span) model.Span { return model.Span{Start: s.Start + span.Start, End: s.End + span.Start} }
	for _, e := range inner.Errors {
		b.ix.Findings = append(b.ix.Findings, lint.HostFinding(lint.CodeNixSyntax, shift(e.Span), e.Message))
	}
	base := b.next
	b.next += innerScopes(inner.Index)
	b.merge(inner.Index, base, shift)
}

func (b *symbolTable) merge(inner *model.Index, base model.Scope, place func(model.Span) model.Span) {
	symbols := len(b.ix.Symbols)
	for _, sym := range inner.Symbols {
		sym.Span, sym.Selection, sym.Listed = place(sym.Span), place(sym.Selection), false
		sym.Detail = WithoutStandIns(sym.Detail)
		sym.Scope += base
		if sym.Parent != model.NoParent {
			sym.Parent += symbols
		}
		sym.Hidden = true
		b.ix.Symbols = append(b.ix.Symbols, sym)
		b.owners = append(b.owners, owner{})
	}
	for _, r := range inner.Refs {
		r.Span, r.Call = place(r.Span), nil
		if r.Horizon > 0 {
			r.Horizon = model.Narrow(place(model.Span{Start: int(r.Horizon), End: int(r.Horizon)}).Start)
		}
		r.Scope += base
		if int(r.Owner) != model.NoSymbol {
			r.Owner += model.Narrow(symbols)
		}
		b.ix.Refs = append(b.ix.Refs, r)
	}
	for _, fold := range inner.Folds {
		fold.Span = place(fold.Span)
		b.ix.Folds = append(b.ix.Folds, fold)
	}
	for shell, parents := range inner.Extends {
		if b.ix.Extends == nil {
			b.ix.Extends = map[model.Scope][]model.Scope{}
		}
		for _, parent := range parents {
			b.ix.Extends[shell+base] = append(b.ix.Extends[shell+base], parent+base)
		}
	}
}

func innerScopes(ix *model.Index) model.Scope {
	r, analyzed := ix.Renames.(renames)
	if !analyzed {
		panic("nix: a heredoc was not analyzed as Nix")
	}
	return r.unit.next
}

func hostScopes(ix *model.Index) model.Scope {
	var top model.Scope
	for _, s := range ix.Symbols {
		top = max(top, s.Scope)
	}
	for _, r := range ix.Refs {
		top = max(top, r.Scope)
	}
	for _, s := range ix.Shells {
		top = max(top, s.Scope)
	}
	return top
}

func (b *symbolTable) reserveHosted(s shellString) {
	h := s.hosted
	h.base = b.newScope()
	top := hostScopes(h.inner.Index)
	b.next += top
	place := h.placer()
	for _, shell := range h.inner.Index.Shells {
		whole := place(shell.Span)
		for _, piece := range b.f.pieces(s.str) {
			if lo, hi := max(piece.Start, whole.Start), min(piece.End, whole.End); lo < hi {
				b.ix.Shells = append(b.ix.Shells, model.Shell{Span: model.Span{Start: lo, End: hi}, Scope: h.base + shell.Scope})
			}
		}
	}
	if top == 0 {
		return
	}
	if b.ix.Extends == nil {
		b.ix.Extends = map[model.Scope][]model.Scope{}
	}
	for scope := model.Scope(1); scope <= top; scope++ {
		b.ix.Extends[h.base+scope] = []model.Scope{h.base}
	}
}

func (h *hostedText) placer() func(model.Span) model.Span {
	return func(s model.Span) model.Span { return placedSpan(h.via, s) }
}

func WithoutStandIns(text string) string {
	return strings.ReplaceAll(text, holeStandIn, shownHole)
}

func (t HostedText) Findings(found []lint.Finding) []lint.Finding {
	return placedFindings(t.Via, found)
}

func placedSpan(via *projection.Map, s model.Span) model.Span {
	start, end := via.HostSpan(s.Start, s.End)
	return model.Span{Start: start, End: end}
}

func placedFindings(via *projection.Map, found []lint.Finding) []lint.Finding {
	out := make([]lint.Finding, 0, len(found))
	sets := make([][]lint.Fix, 0, len(found))
	for _, finding := range found {
		if via.TouchesStandIn(finding.Span.Start, finding.Span.End) {
			continue
		}
		placed, kept := placedFixes(via, finding.Fixes)
		finding.Span, finding.Fixes = placedSpan(via, finding.Span), placed
		out = append(out, finding)
		sets = append(sets, kept)
	}
	if _, together := via.Edits(lint.SafeEdits(sets)); !together {
		reviewFixes(out)
	}
	return out
}

func placedFixes(via *projection.Map, fixes []lint.Fix) (placed, kept []lint.Fix) {
	for _, fix := range fixes {
		if edits, mapped := via.Edits(fix.Edits); mapped {
			kept = append(kept, fix)
			fix.Edits = edits
			placed = append(placed, fix)
		}
	}
	return placed, kept
}

func reviewFixes(findings []lint.Finding) {
	for i := range findings {
		findings[i].Fixes = slices.Clone(findings[i].Fixes)
		for j := range findings[i].Fixes {
			findings[i].Fixes[j].Safety = lint.Review
		}
	}
}

func (b *symbolTable) embedHosted(found []shellString) {
	added := false
	for _, s := range found {
		h := s.hosted
		if h == nil {
			continue
		}
		added = true
		place := h.placer()
		b.ix.Findings = append(b.ix.Findings, placedFindings(h.via, slices.Concat(h.inner.Index.Findings, h.inner.Findings))...)
		for _, t := range h.inner.Index.Tokens {
			if !h.via.TouchesStandIn(t.Span.Start, t.Span.End) {
				t.Span = place(t.Span)
				b.ix.Tokens = append(b.ix.Tokens, t)
			}
		}
		b.merge(h.inner.Index, h.base, place)
	}
	if added {
		lint.SortHostFindings(b.ix.Findings)
		b.publish()
	}
}
