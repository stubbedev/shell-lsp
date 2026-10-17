package filelint

import (
	"cmp"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Settings struct {
	RC       shellcheckrc.File
	Shell    *dialect.Dialect
	Exclude  lint.CodeSet
	Optional []string
	Profile  lint.Profile
	Extended *bool
}

func (s Settings) Forced() *dialect.Detection {
	switch {
	case s.Shell != nil:
		return dialect.Forced(*s.Shell, dialect.SourceOverride)
	case s.RC.Shell != nil:
		return dialect.Forced(*s.RC.Shell, dialect.SourceShellcheckrc)
	}
	return nil
}

func (s Settings) Excluded() lint.CodeSet { return s.Exclude.Union(s.RC.Disabled) }

func (s Settings) DialectSource(file *workspace.File) lint.DialectSource {
	switch {
	case s.Forced() != nil || file.Doc.Projected():
		return lint.DialectForced
	case !file.Detection.Named():
		return lint.DialectGuessed
	}
	return lint.DialectDetected
}

func Check(file *workspace.File, env navigation.Env, s Settings) lint.Report {
	var merged lint.Report
	for _, variant := range file.Variants() {
		report := check(variant, env, s)
		for _, d := range report.Disabled {
			for i := range d.Suppressed {
				d.Suppressed[i] += len(merged.Suppressed)
			}
			merged.Disabled = append(merged.Disabled, d)
		}
		merged.Findings = append(merged.Findings, report.Findings...)
		merged.Suppressed = append(merged.Suppressed, report.Suppressed...)
	}
	return merged
}

func check(file *workspace.File, env navigation.Env, s Settings) lint.Report {
	report := lint.Run(file.Tree, navigation.Imports(file, env), lint.Options{
		Exclude:       s.Excluded(),
		Optional:      slices.Concat(s.Optional, s.RC.Enabled),
		Profile:       s.Profile,
		DialectSource: s.DialectSource(file),
		Sources:       followed(file, env),
		RCFile:        dialect.IsRCFile(file.Path),

		Normalized: lint.Normalized{
			ByteOrderMark:   file.Doc.ByteOrderMark(),
			CarriageReturns: file.Doc.CarriageReturns(),
		},

		NoExtendedAnalysis: !*cmp.Or(s.Extended, s.RC.Extended, new(true)),
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
