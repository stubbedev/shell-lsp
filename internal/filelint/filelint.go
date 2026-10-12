package filelint

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Options struct {
	Exclude       lint.CodeSet
	Optional      []string
	Profile       lint.Profile
	DialectSource lint.DialectSource

	NoExtendedAnalysis bool
}

func Check(file *workspace.File, env navigation.Env, opts Options) lint.Report {
	report := lint.Run(file.Tree, navigation.Imports(file, env), lint.Options{
		Exclude:       opts.Exclude,
		Optional:      opts.Optional,
		Profile:       opts.Profile,
		DialectSource: opts.DialectSource,
		Sources:       followed(file, env),
		RCFile:        dialect.IsRCFile(file.Path),

		Normalized: lint.Normalized{
			ByteOrderMark:   file.Doc.ByteOrderMark(),
			CarriageReturns: file.Doc.CarriageReturns(),
		},

		NoExtendedAnalysis: opts.NoExtendedAnalysis,
	})
	if file.Doc.Projected() {
		report.Findings = slices.DeleteFunc(report.Findings, func(f lint.Finding) bool { return file.Doc.Synthetic(f.Span.Start) })
		for i, f := range report.Findings {
			report.Findings[i].Fixes = expressible(file.Doc, f.Fixes)
		}
	}
	return report
}

func expressible(doc *docstore.Document, fixes []lint.Fix) []lint.Fix {
	var out []lint.Fix
	for _, fix := range fixes {
		if _, ok := doc.SourceEdits(fix.Edits); ok {
			out = append(out, fix)
		}
	}
	return out
}

func followed(file *workspace.File, env navigation.Env) []lint.Source {
	var out []lint.Source
	for _, ref := range navigation.Sources(file, env) {
		if ref.Target != nil {
			out = append(out, lint.Source{Command: ref.Command.Span, Tree: ref.Target.Tree})
		}
	}
	return out
}
