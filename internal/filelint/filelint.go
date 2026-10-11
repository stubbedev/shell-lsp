package filelint

import (
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Options struct {
	Exclude       []lint.Code
	Optional      []string
	DialectSource lint.DialectSource

	NoExtendedAnalysis bool
}

func Check(file *workspace.File, env navigation.Env, opts Options) []lint.Finding {
	return lint.Check(file.Tree, navigation.Imports(file, env), lint.Options{
		Exclude:       opts.Exclude,
		Optional:      opts.Optional,
		DialectSource: opts.DialectSource,
		Sources:       followed(file, env),
		RCFile:        dialect.IsRCFile(file.Path),

		NoExtendedAnalysis: opts.NoExtendedAnalysis,
	})
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
