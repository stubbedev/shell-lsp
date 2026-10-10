package format

import (
	"bytes"
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
	FunctionNextLine bool
	Simplify         bool
	Minify           bool
}

func Format(text string, d dialect.Dialect, opts Options) (string, error) {
	once, err := formatOnce(text, d, opts)
	if err != nil {
		return "", err
	}
	twice, err := formatOnce(once, d, opts)
	if err != nil {
		return "", fmt.Errorf("%w: the formatted %s no longer parses: %w", ErrNotIdempotent, d, err)
	}
	if once != twice {
		return "", fmt.Errorf("%w for this %s document; refusing to format", ErrNotIdempotent, d)
	}
	return once, nil
}

func formatOnce(text string, d dialect.Dialect, opts Options) (string, error) {
	file, err := syntax.NewParser(syntax.Variant(parser.Language(d)), syntax.KeepComments(true)).Parse(strings.NewReader(text), "")
	if err != nil {
		return "", fmt.Errorf("cannot format a document with syntax errors: %w", err)
	}
	if opts.Simplify {
		syntax.Simplify(file)
	}
	printer := syntax.NewPrinter(
		syntax.Indent(opts.Indent),
		syntax.BinaryNextLine(opts.BinaryNextLine),
		syntax.SwitchCaseIndent(opts.SwitchCaseIndent),
		syntax.SpaceRedirects(opts.SpaceRedirects),
		syntax.FunctionNextLine(opts.FunctionNextLine),
		syntax.Minify(opts.Minify),
	)
	var out bytes.Buffer
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
