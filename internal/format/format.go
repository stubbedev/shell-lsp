package format

import (
	"errors"
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var ErrNotIdempotent = errors.New("formatter output is not stable")

type Options struct {
	Indent           uint
	BinaryNextLine   bool
	SwitchCaseIndent bool
	SpaceRedirects   bool
	KeepPadding      bool
	FunctionNextLine bool
	Simplify         bool
	Minify           bool
}

type Style struct {
	Indent           *uint
	BinaryNextLine   *bool
	SwitchCaseIndent *bool
	SpaceRedirects   *bool
	KeepPadding      *bool
	FunctionNextLine *bool
	Simplify         *bool
	Minify           *bool
}

func (s Style) Over(base Style) Style {
	return Style{
		Indent:           firstSet(s.Indent, base.Indent),
		BinaryNextLine:   firstSet(s.BinaryNextLine, base.BinaryNextLine),
		SwitchCaseIndent: firstSet(s.SwitchCaseIndent, base.SwitchCaseIndent),
		SpaceRedirects:   firstSet(s.SpaceRedirects, base.SpaceRedirects),
		KeepPadding:      firstSet(s.KeepPadding, base.KeepPadding),
		FunctionNextLine: firstSet(s.FunctionNextLine, base.FunctionNextLine),
		Simplify:         firstSet(s.Simplify, base.Simplify),
		Minify:           firstSet(s.Minify, base.Minify),
	}
}

func (s Style) Options() Options {
	return Options{
		Indent:           valueOf(s.Indent),
		BinaryNextLine:   valueOf(s.BinaryNextLine),
		SwitchCaseIndent: valueOf(s.SwitchCaseIndent),
		SpaceRedirects:   valueOf(s.SpaceRedirects),
		KeepPadding:      valueOf(s.KeepPadding),
		FunctionNextLine: valueOf(s.FunctionNextLine),
		Simplify:         valueOf(s.Simplify),
		Minify:           valueOf(s.Minify),
	}
}

func firstSet[T any](preferred, fallback *T) *T {
	if preferred != nil {
		return preferred
	}
	return fallback
}

func valueOf[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func Format(tree *parser.Tree, opts Options) (string, error) {
	if len(tree.Errors) > 0 {
		first := tree.Errors[0]
		return "", fmt.Errorf("cannot format a document with syntax errors: line %d: %s", tree.Line(first.Span.Start), first.Message)
	}
	file := tree.File
	if opts.Simplify {
		fresh, err := parse(tree.Text(), tree.Dialect, true)
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
	reparsed, err := parse(once, tree.Dialect, opts.Simplify)
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

func parse(text string, d dialect.Dialect, simplify bool) (*syntax.File, error) {
	file, err := syntax.NewParser(syntax.Variant(parser.Language(d)), syntax.KeepComments(true)).Parse(strings.NewReader(text), "")
	if err != nil {
		return nil, fmt.Errorf("parsing %s for formatting: %w", d, err)
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
