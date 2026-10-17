package hostnav

import (
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/assist"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const maxHintValue = 32

func (g *Group) InlayHints(within parser.Span) []assist.Hint {
	var out []assist.Hint
	ix := g.Index()
	text := g.Self.Doc.Source().Text()
	inside := func(s parser.Span) bool { return within.Start <= s.Start && s.End <= within.End }
	for _, r := range ix.Refs {
		if !inside(r.Span) {
			continue
		}
		if len(r.ArgSpans) > 0 {
			out = append(out, g.parameterHints(r, r.ArgSpans, text)...)
		}
		if r.Kind != model.KindVariable {
			continue
		}
		for _, t := range g.Module.Resolve(g.self, r).Targets {
			if s := t.Info(); s.Constant {
				out = append(out, assist.Hint{Offset: r.Span.End, Label: "= " + truncate(s.Value), Kind: assist.HintValue})
				break
			}
		}
	}
	for _, region := range regions(g.Self) {
		for _, run := range region.runs {
			if len(run.refs) == 0 || !inside(run.span) {
				continue
			}
			first := run.refs[0]
			var args []parser.Span
			for _, a := range run.args {
				if a.Start > first.Span.Start {
					args = append(args, a)
				}
			}
			out = append(out, g.parameterHints(first, args, text)...)
		}
	}
	return out
}

func (g *Group) parameterHints(r model.Ref, args []parser.Span, text string) []assist.Hint {
	names := g.parameterNames(r)
	var out []assist.Hint
	for i, arg := range args {
		if i >= len(names) {
			break
		}
		if text[arg.Start:arg.End] == names[i] {
			continue
		}
		out = append(out, assist.Hint{Offset: arg.Start, Label: names[i] + ":", Kind: assist.HintParameter})
	}
	return out
}

func (g *Group) parameterNames(r model.Ref) []string {
	res := g.Module.Resolve(g.self, model.Ref{Name: r.Name, Kind: r.Kind, Scope: r.Scope, Qualifier: r.Qualifier})
	if res.Builtin != nil {
		return res.Builtin.Params
	}
	if len(res.Targets) == 0 {
		return nil
	}
	t := res.Targets[0]
	ix := t.Module.Files[t.File]
	var names []string
	for _, i := range ix.Children(t.Symbol) {
		if c := ix.Symbols[i]; c.Kind == model.KindParameter {
			names = append(names, c.Name)
		}
	}
	return names
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxHintValue {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxHintValue-1]) + "…"
}
