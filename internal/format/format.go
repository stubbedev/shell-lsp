package format

import (
	"errors"
	"fmt"
	"strings"

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

func RangeEdits(edits []textedit.Edit, start, end int) []textedit.Edit {
	var out []textedit.Edit
	for _, e := range edits {
		if e.End >= start && e.Start <= end {
			out = append(out, e)
		}
	}
	return out
}
