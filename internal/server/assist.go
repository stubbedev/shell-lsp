package server

import (
	"context"
	"encoding/json"
	"unicode/utf16"

	"github.com/stubbedev/shell-lsp/internal/assist"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
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

func (s *Server) handleSignatureHelp(_ context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams) (any, error) {
	here, inside, err := s.at(docs, position)
	if err != nil || !inside {
		return nil, err
	}
	sig, ok := assist.SignatureHelp(here.Tree, here.offset, s.functionLookup(here.File))
	if !ok {
		return nil, nil
	}
	info := lsp.SignatureInformation{Label: sig.Label, Parameters: []lsp.ParameterInformation{}}
	if sig.Doc != "" {
		info.Documentation = &lsp.MarkupContent{Kind: lsp.MarkupMarkdown, Value: sig.Doc}
	}
	for _, p := range sig.Params {
		param := lsp.ParameterInformation{Label: [2]uint32{utf16Len(sig.Label[:p.Start]), utf16Len(sig.Label[:p.End])}}
		if p.Doc != "" {
			param.Documentation = &lsp.MarkupContent{Kind: lsp.MarkupMarkdown, Value: p.Doc}
		}
		info.Parameters = append(info.Parameters, param)
	}
	help := lsp.SignatureHelp{Signatures: []lsp.SignatureInformation{info}}
	if sig.Active >= 0 {
		active := uint32(sig.Active) //nolint:gosec
		help.ActiveParameter = &active
		help.Signatures[0].ActiveParameter = &active
	}
	return help, nil
}

func utf16Len(s string) uint32 {
	return uint32(len(utf16.Encode([]rune(s)))) //nolint:gosec
}

type hintData struct {
	Tooltip string `json:"tooltip"`
}

func (s *Server) handleInlayHint(_ context.Context, docs *docstore.View, p lsp.InlayHintParams) (any, error) {
	file, err := s.workspaceFile(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	enc := docs.Encoding()
	within := parser.Span{Start: file.Doc.PositionToOffset(p.Range.Start, enc), End: file.Doc.PositionToOffset(p.Range.End, enc)}
	hints := assist.InlayHints(file, within, s.functionLookup(file), s.navigationEnv())
	out := make([]lsp.InlayHint, 0, len(hints))
	for _, h := range hints {
		hint := lsp.InlayHint{Position: file.Doc.OffsetToPosition(h.Offset, enc), Label: h.Label}
		switch h.Kind {
		case assist.HintType:
			hint.Kind = lsp.InlayHintType
		case assist.HintParameter:
			hint.Kind, hint.PaddingRight = lsp.InlayHintParameter, true
		case assist.HintPath:
			hint.PaddingLeft = true
		}
		if h.Tooltip != "" {
			data, err := json.Marshal(hintData{Tooltip: h.Tooltip})
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
	var data hintData
	if err := json.Unmarshal(hint.Data, &data); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "inlay hint data %s is not from this server: %v", hint.Data, err)
	}
	hint.Tooltip = &lsp.MarkupContent{Kind: lsp.MarkupMarkdown, Value: data.Tooltip}
	hint.Data = nil
	return hint, nil
}

func (s *Server) handleDocumentLink(_ context.Context, docs *docstore.View, p lsp.DocumentLinkParams) (any, error) {
	file, err := s.workspaceFile(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	links := assist.DocumentLinks(file, s.navigationEnv())
	out := make([]lsp.DocumentLink, len(links))
	for i, l := range links {
		out[i] = lsp.DocumentLink{Range: file.Doc.RangeFor(l.Span.Start, l.Span.End, docs.Encoding()), Target: l.Target}
	}
	return out, nil
}
