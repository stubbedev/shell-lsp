package hostnav

import (
	"path/filepath"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/assist"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func (g *Group) Signature(offset int) (assist.Signature, bool) {
	if g.Language == host.Nix {
		if sig, found, hosted := g.embeddedSignature(offset); hosted {
			return sig, found
		}
		return nixSignature(g.Self.Doc.Source().Text(), offset, g.nixFiles())
	}
	inv, ok := g.Self.Native.Invocation(offset)
	if !ok {
		return assist.Signature{}, false
	}
	return g.signatureOf(inv)
}

func (g *Group) nixFiles() nix.Files {
	if g.builder == nil {
		return nix.NoFiles
	}
	return NixFiles(g.builder.env.Loader, g.Self)
}

func NixFiles(loader Loader, file *workspace.File) nix.Files {
	if !file.Doc.Local {
		return nix.NoFiles
	}
	dir := filepath.Dir(file.Path)
	return func(path string) (string, bool) {
		named := filepath.Join(dir, filepath.FromSlash(path))
		for _, candidate := range []string{named, filepath.Join(named, nix.DirectoryEntry)} {
			if target, err := loader.LoadAs(candidate, host.Nix); err == nil {
				return target.Doc.Source().Text(), true
			}
		}
		return "", false
	}
}

func nixSignature(text string, offset int, files nix.Files) (assist.Signature, bool) {
	sig, ok := nix.SignatureAt(text, offset, files)
	if !ok {
		return assist.Signature{}, false
	}
	params := make([]assist.ParamLabel, len(sig.Params))
	for i, p := range sig.Params {
		params[i] = assist.ParamLabel{Start: p.Start, End: p.End}
	}
	return assist.Signature{Label: sig.Label, Doc: sig.Doc, Params: params, Active: sig.Active}, true
}

func (g *Group) ShellSignature(offset int) (assist.Signature, bool) {
	at := g.Self.Doc.SourceOffset(offset)
	if g.Language == host.Nix {
		if sig, found, hosted := g.embeddedSignature(at); hosted {
			return sig, found
		}
	}
	for _, r := range regions(g.Self) {
		for _, run := range r.runs {
			if len(run.refs) == 0 || at < run.refs[0].Span.End+1 || at > run.span.End+1 {
				continue
			}
			first := run.refs[0]
			active := 0
			for _, arg := range run.args {
				if arg.Start > first.Span.Start && arg.End < at {
					active++
				}
			}
			sig, ok := g.signatureOf(model.Invocation{Name: first.Name, Qualifier: first.Qualifier(), Kind: first.Kind, Active: active, Scope: first.Scope})
			if !ok || len(sig.Params) == 0 {
				return assist.Signature{}, false
			}
			return sig, true
		}
	}
	return assist.Signature{}, false
}

func (g *Group) signatureOf(inv model.Invocation) (assist.Signature, bool) {
	res := g.Module.Resolve(g.selfAt(), model.Ref{Name: inv.Name, Kind: inv.Kind, Scope: inv.Scope, Call: model.Qualified(inv.Qualifier)})
	switch {
	case res.Builtin != nil:
		b := res.Builtin
		return signature(b.Name, b.Signature, b.Params, b.Doc, clamp(inv.Active, b.Params, b.Arity)), true
	case len(res.Targets) == 0:
		return assist.Signature{}, false
	}
	t := res.Targets[0]
	s := t.Info()
	ix := t.Module.Files[t.File]
	var params []string
	for _, i := range ix.Children(t.Symbol) {
		if c := ix.Symbols[i]; c.Kind == model.KindParameter {
			params = append(params, c.Description())
		}
	}
	label := s.Name + " " + strings.Join(params, " ")
	if s.Kind == model.KindFunction {
		label = s.Name + "(" + strings.Join(params, ", ") + ")"
	}
	return signature(s.Name, strings.TrimSpace(label), params, s.Doc, clamp(inv.Active, params, s.Arity)), true
}

func signature(name, label string, params []string, doc string, active int) assist.Signature {
	sig := assist.Signature{Label: label, Doc: doc, Active: active}
	from := strings.Index(label, name) + len(name)
	for _, p := range params {
		i := strings.Index(label[from:], p)
		if i < 0 {
			panic("hostnav: parameter " + p + " is not in " + label)
		}
		sig.Params = append(sig.Params, assist.ParamLabel{Start: from + i, End: from + i + len(p)})
		from += i + len(p)
	}
	return sig
}

func clamp(active int, params []string, arity model.Arity) int {
	if active >= len(params) && arity.Max == model.Variadic && len(params) > 0 {
		return len(params) - 1
	}
	return active
}
