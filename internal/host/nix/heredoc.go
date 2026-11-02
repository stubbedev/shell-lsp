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
	end := str.Content.End
	at := str.Content.Start
	for at < end {
		stop := lineEnd(f.src, at, end)
		line := f.src[at:stop]
		at = min(stop+1, end)
		delimiter, isNix := nixOpener(line)
		if !isNix {
			continue
		}
		start := at
		for at < end {
			next := lineEnd(f.src, at, end)
			if strings.TrimSpace(f.src[at:next]) == delimiter {
				if body := (model.Span{Start: start, End: at}); literalOnly(body) {
					out = append(out, nixHeredoc{span: body})
				}
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

func nixOpener(line string) (string, bool) {
	before, after, found := strings.Cut(line, "<<")
	if !found || strings.HasPrefix(after, "<") {
		return "", false
	}
	after = strings.TrimLeft(strings.TrimPrefix(after, "-"), " \t")
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return "", false
	}
	delimiter := strings.Trim(fields[0], `'"`)
	rest := strings.Join(fields[1:], " ")
	switch {
	case slices.Contains(nixDelimiters, delimiter):
	case strings.Contains(rest, nixMarker):
	case writesNixFile(before), writesNixFile(rest):
	default:
		return "", false
	}
	return delimiter, delimiter != ""
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
	symbols := len(b.ix.Symbols)
	b.next += innerScopes(inner.Index)
	for _, sym := range inner.Index.Symbols {
		sym.Span, sym.Selection, sym.Rewrite, sym.Listed = shift(sym.Span), shift(sym.Selection), nil, false
		sym.Scope += base
		if sym.Parent != model.NoParent {
			sym.Parent += symbols
		}
		sym.Hidden = true
		b.ix.Symbols = append(b.ix.Symbols, sym)
		b.owners = append(b.owners, owner{})
	}
	for _, r := range inner.Index.Refs {
		r.Span, r.Rewrite = shift(r.Span), nil
		r.Scope += base
		if r.Owner != model.NoSymbol {
			r.Owner += symbols
		}
		b.ix.Refs = append(b.ix.Refs, r)
	}
	for _, fold := range inner.Index.Folds {
		fold.Span = shift(fold.Span)
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
