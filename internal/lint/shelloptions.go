package lint

import (
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func (p *pass) shellOptions() shelloptions.PerShell {
	if p.perShell == nil {
		options := shelloptions.Scan(p.tree)
		p.perShell = &options
	}
	return *p.perShell
}

func (p *pass) optionsAt(n syntax.Node) shelloptions.Options {
	return p.shellOptions().At(parser.Offset(n.Pos()))
}

func runsLastPipeInShell(d dialect.Dialect, opts shelloptions.Options) bool {
	return d.Features().LastPipeInShell || d == dialect.Bash && opts.Lastpipe
}

func (p *pass) implicitlySplit(pe *syntax.ParamExp) bool {
	return p.dialect.Features().SplitsUnquoted || pe.Split == syntax.OptUnset && p.optionsAt(pe).ShWordSplit
}

func (p *pass) implicitlyGlobbed(pe *syntax.ParamExp) bool {
	return p.dialect.Features().GlobsUnquoted || pe.GlobSubst == syntax.OptUnset && p.optionsAt(pe).GlobSubst
}

func (p *pass) substitutionGlobbed(cs *syntax.CmdSubst) bool {
	return p.dialect.Features().GlobsUnquoted || p.optionsAt(cs).GlobSubst
}
