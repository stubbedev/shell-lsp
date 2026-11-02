package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const nixMarker = "# nix"

var nixDelimiters = []string{"NIX", "nix", "EOF_NIX"}

type nixHeredoc struct {
	span model.Span
}

func (f *file) nixHeredocs(str *syntax.String) []nixHeredoc {
	literalOnly := func(body model.Span) bool {
		return !slices.ContainsFunc(str.Parts, func(p syntax.Part) bool {
			_, literal := p.(*syntax.Literal)
			return !literal && p.Span().Start < body.End && body.Start < p.Span().End
		})
	}
	var out []nixHeredoc
	for _, body := range f.heredocBodies(str, nixOpener) {
		if literalOnly(body) {
			out = append(out, nixHeredoc{span: body})
		}
	}
	return out
}

func (f *file) heredocBodies(str *syntax.String, opener func(string) (string, bool)) []model.Span {
	var out []model.Span
	end := str.Content.End
	at := str.Content.Start
	for at < end {
		stop := lineEnd(f.src, at, end)
		line := f.src[at:stop]
		at = min(stop+1, end)
		delimiter, opens := opener(line)
		if !opens {
			continue
		}
		start := at
		for at < end {
			next := lineEnd(f.src, at, end)
			if strings.TrimSpace(f.src[at:next]) == delimiter {
				out = append(out, model.Span{Start: start, End: at})
				at = min(next+1, end)
				break
			}
			at = min(next+1, end)
			if next == end {
				break
			}
		}
	}
	return out
}

func lineEnd(src string, at, end int) int {
	if nl := strings.IndexByte(src[at:end], '\n'); nl >= 0 {
		return at + nl
	}
	return end
}

func heredocDelimiter(line string) (string, bool) {
	delimiter, _, _, opens := heredocParts(line)
	return delimiter, opens
}

func heredocParts(line string) (string, string, string, bool) {
	before, after, found := strings.Cut(line, "<<")
	if !found || strings.HasPrefix(after, "<") {
		return "", "", "", false
	}
	after = strings.TrimLeft(strings.TrimPrefix(after, "-"), " \t")
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return "", "", "", false
	}
	delimiter := strings.Trim(fields[0], `'"`)
	return delimiter, before, strings.Join(fields[1:], " "), delimiter != ""
}

func nixOpener(line string) (string, bool) {
	delimiter, before, rest, opens := heredocParts(line)
	if !opens {
		return "", false
	}
	switch {
	case slices.Contains(nixDelimiters, delimiter):
	case strings.Contains(rest, nixMarker):
	case writesNixFile(before), writesNixFile(rest):
	default:
		return "", false
	}
	return delimiter, true
}

func writesNixFile(text string) bool {
	return slices.ContainsFunc(strings.Fields(text), func(word string) bool { return strings.HasSuffix(strings.Trim(word, `'"`), nixExtension) })
}

func (b *symbolTable) embedHeredocs(found []shellString) {
	for _, s := range found {
		for _, h := range b.f.nixHeredocs(s.str) {
			b.embed(h.span)
		}
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
		sym.Span, sym.Selection, sym.Rewrite, sym.Listed = place(sym.Span), place(sym.Selection), nil, false
		sym.Scope += base
		if sym.Parent != model.NoParent {
			sym.Parent += symbols
		}
		sym.Hidden = true
		b.ix.Symbols = append(b.ix.Symbols, sym)
		b.owners = append(b.owners, owner{})
	}
	for _, r := range inner.Refs {
		r.Span, r.Rewrite, r.ArgSpans = place(r.Span), nil, nil
		if r.Horizon > 0 {
			r.Horizon = place(model.Span{Start: r.Horizon, End: r.Horizon}).Start
		}
		r.Scope += base
		if r.Owner != model.NoSymbol {
			r.Owner += symbols
		}
		b.ix.Refs = append(b.ix.Refs, r)
	}
	for _, fold := range inner.Folds {
		fold.Span = place(fold.Span)
		b.ix.Folds = append(b.ix.Folds, fold)
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
	return func(s model.Span) model.Span {
		start, end := h.via.HostSpan(s.Start, s.End)
		return model.Span{Start: start, End: end}
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
		for _, findings := range [][]lint.Finding{h.inner.Index.Findings, h.inner.Findings} {
			for _, finding := range findings {
				if !h.via.TouchesStandIn(finding.Span.Start, finding.Span.End) {
					finding.Span, finding.Fixes = place(finding.Span), nil
					b.ix.Findings = append(b.ix.Findings, finding)
				}
			}
		}
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
