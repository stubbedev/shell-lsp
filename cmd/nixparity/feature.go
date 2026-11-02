package main

import (
	"encoding"
	"fmt"
	"slices"
	"time"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

type feature uint8

const (
	featureDiagnostics feature = iota
	featureSymbols
	featureFolding
	featureDefinition
	featureReferences
	featureRename
	featureHover
	featureCompletion
	featureFormatting
)

type featureSpec struct {
	name       string
	method     string
	capability string
	positional bool
	decode     decoder
	agrees     func(a, b []string) bool
}

var featureSpecs = [...]featureSpec{
	featureDiagnostics: {name: "diagnostics", method: "textDocument/publishDiagnostics", decode: decodeDiagnostics, agrees: slices.Equal[[]string]},
	featureSymbols:     {name: "documentSymbol", method: "textDocument/documentSymbol", capability: "documentSymbolProvider", decode: decodeSymbols, agrees: slices.Equal[[]string]},
	featureFolding:     {name: "foldingRange", method: "textDocument/foldingRange", capability: "foldingRangeProvider", decode: decodeFolding, agrees: slices.Equal[[]string]},
	featureDefinition:  {name: "definition", method: "textDocument/definition", capability: "definitionProvider", positional: true, decode: decodeLocations, agrees: slices.Equal[[]string]},
	featureReferences:  {name: "references", method: "textDocument/references", capability: "referencesProvider", positional: true, decode: decodeLocations, agrees: slices.Equal[[]string]},
	featureRename:      {name: "rename", method: "textDocument/rename", capability: "renameProvider", positional: true, decode: decodeRename, agrees: slices.Equal[[]string]},
	featureHover:       {name: "hover", method: "textDocument/hover", capability: "hoverProvider", positional: true, decode: decodeHover, agrees: hoverAgrees},
	featureCompletion:  {name: "completion", method: "textDocument/completion", capability: "completionProvider", positional: true, decode: decodeCompletion, agrees: slices.Equal[[]string]},
	featureFormatting:  {name: "formatting", method: "textDocument/formatting", capability: "documentFormattingProvider", decode: decodeFormatting, agrees: slices.Equal[[]string]},
}

func hoverAgrees(a, b []string) bool {
	return len(a) == 0 || len(b) == 0 || slices.Equal(a, b)
}

func allFeatures() []feature {
	out := make([]feature, len(featureSpecs))
	for i := range out {
		out[i] = feature(i)
	}
	return out
}

func (f feature) spec() featureSpec { return featureSpecs[f] }

func (f feature) String() string { return featureSpecs[f].name }

var _ encoding.TextMarshaler = feature(0)

func (f feature) MarshalText() ([]byte, error) { return []byte(f.String()), nil }

func (f *feature) UnmarshalText(text []byte) error {
	for _, candidate := range allFeatures() {
		if candidate.String() == string(text) {
			*f = candidate
			return nil
		}
	}
	return fmt.Errorf("%q is not a compared feature", text)
}

const completionPrefix = 2

func (f feature) probeOffset(t token) int {
	if f == featureCompletion {
		return min(t.start+completionPrefix, t.end)
	}
	return t.start
}

const renameTarget = "parity_renamed"

func requestParams(q query, uri string) any {
	document := lsp.TextDocumentIdentifier{URI: uri}
	at := lsp.TextDocumentPositionParams{TextDocument: document, Position: q.position}
	switch q.feature {
	case featureSymbols:
		return lsp.DocumentSymbolParams{TextDocument: document}
	case featureFolding:
		return lsp.FoldingRangeParams{TextDocument: document}
	case featureFormatting:
		return lsp.DocumentFormattingParams{TextDocument: document, Options: lsp.FormattingOptions{TabSize: formattingTabSize, InsertSpaces: true}}
	case featureReferences:
		params := lsp.ReferenceParams{TextDocumentPositionParams: at}
		params.Context.IncludeDeclaration = true
		return params
	case featureRename:
		return lsp.RenameParams{TextDocumentPositionParams: at, NewName: renameTarget}
	case featureDefinition, featureHover, featureCompletion:
		return at
	default:
		panic("diagnostics are published, not requested")
	}
}

const formattingTabSize = 2

type outcome uint8

const (
	outcomeAnswered outcome = iota
	outcomeEmpty
	outcomeRefused
	outcomeUnsupported
	outcomeSilent
	outcomeMalformed
	outcomeTimeout
	outcomeCrashed
)

func (o outcome) failed() bool {
	return o == outcomeMalformed || o == outcomeTimeout || o == outcomeCrashed
}

type answer struct {
	outcome outcome
	items   []string
	latency time.Duration
}

func (a answer) responded() bool {
	switch a.outcome {
	case outcomeAnswered, outcomeEmpty, outcomeRefused, outcomeMalformed:
		return true
	case outcomeUnsupported, outcomeSilent, outcomeTimeout, outcomeCrashed:
		return false
	}
	panic(fmt.Sprintf("unknown outcome %d", a.outcome))
}

type verdict uint8

const (
	verdictAgree verdict = iota
	verdictDisagree
	verdictBothEmpty
	verdictOursMissing
	verdictTheirsMissing
	verdictFailed
)

var verdictNames = [...]string{
	verdictAgree:         "agree",
	verdictDisagree:      "disagree",
	verdictBothEmpty:     "both empty",
	verdictOursMissing:   "ours missing",
	verdictTheirsMissing: "theirs missing",
	verdictFailed:        "failed",
}

func (v verdict) String() string { return verdictNames[v] }

var _ encoding.TextMarshaler = verdict(0)

func (v verdict) MarshalText() ([]byte, error) { return []byte(v.String()), nil }

func (v *verdict) UnmarshalText(text []byte) error {
	i := slices.Index(verdictNames[:], string(text))
	if i < 0 {
		return fmt.Errorf("%q is not a verdict", text)
	}
	*v = verdict(i) //nolint:gosec
	return nil
}
