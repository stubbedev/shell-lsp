package server

import (
	"context"

	"github.com/stubbedev/shell-lsp/internal/assist"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type SignatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters"`
	RetriggerCharacters []string `json:"retriggerCharacters"`
}

type InlayHintOptions struct {
	ResolveProvider bool `json:"resolveProvider"`
}

type DocumentLinkOptions struct {
	ResolveProvider bool `json:"resolveProvider"`
}

func (s *Server) functionLookup(file *workspace.File) assist.FunctionLookup {
	var chain []*workspace.File
	loaded := false
	return func(name string) (*parser.Tree, parser.Function, bool) {
		if fn, ok := file.Tree.FunctionNamed(name); ok {
			return file.Tree, fn, true
		}
		if !loaded {
			chain, loaded = navigation.Chain(file, s.navigationEnv()), true
		}
		for _, f := range chain {
			if fn, ok := f.Tree.FunctionNamed(name); ok {
				return f.Tree, fn, true
			}
		}
		return nil, parser.Function{}, false
	}
}

func (s *Server) handleSignatureHelp(ctx context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams) (any, error) {
	here, inside, err := s.at(docs, position)
	if err != nil {
		return nil, err
	}
	if native, ok := nativeLocation(here, position.Position, inside); ok {
		g, err := s.group(ctx, docs, native.host)
		if err != nil {
			return nil, err
		}
		var sig assist.Signature
		var found bool
		if inside {
			sig, found = g.ShellSignature(here.offset)
		} else {
			sig, found = g.Signature(native.source)
		}
		if found {
			return signatureHelp(sig, docs.Encoding()), nil
		}
	}
	if !inside {
		return nil, nil
	}
	sig, ok := assist.SignatureHelp(here.Tree, here.offset, s.functionLookup(here.File))
	if !ok {
		return nil, nil
	}
	return signatureHelp(sig, docs.Encoding()), nil
}

func signatureHelp(sig assist.Signature, enc docstore.Encoding) lsp.SignatureHelp {
	info := lsp.SignatureInformation{Label: sig.Label, Parameters: []lsp.ParameterInformation{}}
	if sig.Doc != "" {
		info.Documentation = lsp.Markdown(sig.Doc)
	}
	for _, p := range sig.Params {
		param := lsp.ParameterInformation{Label: [2]uint32{enc.Units(sig.Label[:p.Start]), enc.Units(sig.Label[:p.End])}}
		if p.Doc != "" {
			param.Documentation = lsp.Markdown(p.Doc)
		}
		info.Parameters = append(info.Parameters, param)
	}
	help := lsp.SignatureHelp{Signatures: []lsp.SignatureInformation{info}}
	if sig.Active >= 0 {
		active := uint32(sig.Active) //nolint:gosec
		help.ActiveParameter = &active
		help.Signatures[0].ActiveParameter = &active
	}
	return help
}

type hintData struct {
	Tooltip string `json:"tooltip"`
}

func (s *Server) handleInlayHint(ctx context.Context, docs *docstore.View, p lsp.InlayHintParams) (any, error) {
	file, err := s.workspaceFile(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	at := docs.Positions(file.Doc)
	out := []lsp.InlayHint{}
	if h, ok := hostnav.Of(file); ok {
		g, err := s.group(ctx, docs, h)
		if err != nil {
			return nil, err
		}
		source := sourcePositions(file, docs.Encoding())
		within, _ := source.Span(p.Range)
		if out, err = appendHints(out, g.InlayHints(within), source); err != nil {
			return nil, err
		}
	}
	within, inShell := at.Span(p.Range)
	if !inShell {
		return out, nil
	}
	return appendHints(out, assist.InlayHints(file, within, s.functionLookup(file), s.navigationEnv()), at)
}

func appendHints(out []lsp.InlayHint, hints []assist.Hint, at docstore.Positions) ([]lsp.InlayHint, error) {
	for _, h := range hints {
		hint := lsp.InlayHint{Position: at.Position(h.Offset), Label: h.Label}
		switch h.Kind {
		case assist.HintType:
			hint.Kind = lsp.InlayHintType
		case assist.HintParameter:
			hint.Kind, hint.PaddingRight = lsp.InlayHintParameter, true
		case assist.HintPath, assist.HintValue:
			hint.PaddingLeft = true
		}
		if h.Tooltip != "" {
			data, err := encodeData(hintData{Tooltip: h.Tooltip})
			if err != nil {
				return nil, err
			}
			hint.Data = data
		}
		out = append(out, hint)
	}
	return out, nil
}

func (s *Server) handleInlayHintResolve(_ context.Context, _ *docstore.View, hint lsp.InlayHint) (any, error) {
	if len(hint.Data) == 0 {
		return hint, nil
	}
	data, err := decodeData[hintData]("inlay hint", hint.Data)
	if err != nil {
		return nil, err
	}
	hint.Tooltip = lsp.Markdown(data.Tooltip)
	hint.Data = nil
	return hint, nil
}

func (s *Server) handleDocumentLink(ctx context.Context, docs *docstore.View, p lsp.DocumentLinkParams) (any, error) {
	file, err := s.workspaceFile(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	links := assist.DocumentLinks(file, s.navigationEnv())
	out := make([]lsp.DocumentLink, len(links))
	for i, l := range links {
		out[i] = lsp.DocumentLink{Range: docs.Positions(file.Doc).Range(l.Span), Target: l.Target}
	}
	h, ok := hostnav.Of(file)
	if !ok {
		return out, nil
	}
	g, err := s.group(ctx, docs, h)
	if err != nil {
		return nil, err
	}
	for _, l := range g.Links() {
		out = append(out, lsp.DocumentLink{Range: sourcePositions(file, docs.Encoding()).Range(l.Span), Target: fileuri.FromPath(l.Path)})
	}
	return out, nil
}
