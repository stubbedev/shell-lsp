package format

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var ErrNotIdempotent = errors.New("formatter output is not stable")

type settings[B, U any] struct {
	Indent           U
	BinaryNextLine   B
	SwitchCaseIndent B
	SpaceRedirects   B
	KeepPadding      B
	FunctionNextLine B
	Simplify         B
	Minify           B
}

func zip[B1, U1, B2, U2 any](a, b settings[B1, U1], flag func(B1, B1) B2, size func(U1, U1) U2) settings[B2, U2] {
	return settings[B2, U2]{
		Indent:           size(a.Indent, b.Indent),
		BinaryNextLine:   flag(a.BinaryNextLine, b.BinaryNextLine),
		SwitchCaseIndent: flag(a.SwitchCaseIndent, b.SwitchCaseIndent),
		SpaceRedirects:   flag(a.SpaceRedirects, b.SpaceRedirects),
		KeepPadding:      flag(a.KeepPadding, b.KeepPadding),
		FunctionNextLine: flag(a.FunctionNextLine, b.FunctionNextLine),
		Simplify:         flag(a.Simplify, b.Simplify),
		Minify:           flag(a.Minify, b.Minify),
	}
}

type Options settings[bool, uint]

type Style settings[*bool, *uint]

func (s Style) Over(base Style) Style {
	return Style(zip(settings[*bool, *uint](s), settings[*bool, *uint](base), firstSet[bool], firstSet[uint]))
}

func (s Style) Options() Options {
	return Options(zip(settings[*bool, *uint](s), settings[*bool, *uint]{}, orZero[bool], orZero[uint]))
}

func firstSet[T any](preferred, fallback *T) *T {
	if preferred != nil {
		return preferred
	}
	return fallback
}

func orZero[T any](preferred, fallback *T) T {
	if set := firstSet(preferred, fallback); set != nil {
		return *set
	}
	var zero T
	return zero
}

func Format(tree *parser.Tree, opts Options) (string, error) {
	if len(tree.Errors) > 0 {
		first := tree.Errors[0]
		return "", fmt.Errorf("cannot format a document with syntax errors: line %d: %s", tree.Line(first.Span.Start), first.Message)
	}
	file := tree.File
	if opts.Simplify {
		fresh, err := parse(tree.Text(), tree.Lang(), true)
		if err != nil {
			return "", err
		}
		file = fresh
	}
	once, err := render(file, tree.Dialect, opts, len(tree.Text()))
	if err != nil {
		return "", err
	}
	if once == tree.Text() {
		return once, nil
	}
	reparsed, err := parse(once, tree.Lang(), opts.Simplify)
	if err != nil {
		return "", fmt.Errorf("%w: the formatted %s no longer parses: %w", ErrNotIdempotent, tree.Dialect, err)
	}
	twice, err := render(reparsed, tree.Dialect, opts, len(once))
	if err != nil {
		return "", err
	}
	if once != twice {
		return "", fmt.Errorf("%w for this %s document; refusing to format", ErrNotIdempotent, tree.Dialect)
	}
	return once, nil
}

func parse(text string, lang syntax.LangVariant, simplify bool) (*syntax.File, error) {
	file, err := syntax.NewParser(syntax.Variant(lang), syntax.KeepComments(true)).Parse(strings.NewReader(text), "")
	if err != nil {
		return nil, fmt.Errorf("parsing %s for formatting: %w", lang, err)
	}
	if simplify {
		syntax.Simplify(file)
	}
	return file, nil
}

func render(file *syntax.File, d dialect.Dialect, opts Options, sizeHint int) (string, error) {
	printer := syntax.NewPrinter(
		syntax.Indent(opts.Indent),
		syntax.BinaryNextLine(opts.BinaryNextLine),
		syntax.SwitchCaseIndent(opts.SwitchCaseIndent),
		syntax.SpaceRedirects(opts.SpaceRedirects),
		syntax.KeepPadding(opts.KeepPadding), //nolint:staticcheck // shfmt still honours keep_padding from .editorconfig, so matching its output needs it
		syntax.FunctionNextLine(opts.FunctionNextLine),
		syntax.Minify(opts.Minify),
	)
	var out strings.Builder
	out.Grow(sizeHint)
	if err := printer.Print(&out, file); err != nil {
		return "", fmt.Errorf("printing formatted %s: %w", d, err)
	}
	return out.String(), nil
}

func RangeEdits(text string, edits []textedit.Edit, selection, together []parser.Span) []textedit.Edit {
	var out []textedit.Edit
	first, reach, next := 0, 0, 0
	flush := func(end int) {
		group := edits[first:end]
		if slices.ContainsFunc(group, func(e textedit.Edit) bool { return touchesAny(e, selection) }) {
			out = append(out, group...)
		}
		first = end
	}
	aligned := alignedAfter(text, edits)
	for i, e := range edits {
		if e.Start >= reach && (i == 0 || aligned[i-1]) {
			flush(i)
		}
		for next < len(together) && together[next].Start < extent(e.Start, e.End) {
			reach = max(reach, together[next].End)
			next++
		}
	}
	flush(len(edits))
	return out
}

func alignedAfter(text string, edits []textedit.Edit) []bool {
	moved := diff(wordsOf(text), wordsOf(textedit.Apply(text, edits)))
	out := make([]bool, len(edits))
	x, y, delta, last := 0, 0, 0, 0
	for i, e := range edits {
		kept := len(wordsOf(text[last:e.Start]))
		x += kept + len(wordsOf(text[e.Start:e.End]))
		y += kept + len(wordsOf(e.NewText))
		last = e.End
		for len(moved) > 0 && moved[0].aEnd <= x && moved[0].bEnd <= y {
			delta = moved[0].bEnd - moved[0].aEnd
			moved = moved[1:]
		}
		out[i] = y-x == delta && (len(moved) == 0 || x <= moved[0].aStart && y <= moved[0].bStart)
	}
	return out
}

func wordsOf(s string) []string {
	var out []string
	for start := 0; start < len(s); {
		c := s[start]
		if c <= ' ' {
			start++
			continue
		}
		end := start + 1
		for wordByte(c) && end < len(s) && wordByte(s[end]) {
			end++
		}
		out = append(out, s[start:end])
		start = end
	}
	return out
}

func wordByte(c byte) bool {
	return c >= utf8.RuneSelf || c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func touchesAny(e textedit.Edit, spans []parser.Span) bool {
	return slices.ContainsFunc(spans, func(s parser.Span) bool {
		return e.Start < extent(s.Start, s.End) && s.Start < extent(e.Start, e.End)
	})
}

func extent(start, end int) int { return max(end, start+1) }
