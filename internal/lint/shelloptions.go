package lint

import (
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func (p *pass) options() shelloptions.Options {
	if p.shellOptions == nil {
		opts := shelloptions.Scan(p.tree)
		p.shellOptions = &opts
	}
	return *p.shellOptions
}

func (p *pass) implicitlySplit(pe *syntax.ParamExp) bool {
	return p.dialect.Features().SplitsUnquoted || pe.Split == syntax.OptUnset && p.options().ShWordSplit
}

func (p *pass) implicitlyGlobbed(pe *syntax.ParamExp) bool {
	return p.dialect.Features().GlobsUnquoted || pe.GlobSubst == syntax.OptUnset && p.options().GlobSubst
}

func (p *pass) substitutionGlobbed() bool {
	return p.dialect.Features().GlobsUnquoted || p.options().GlobSubst
}
