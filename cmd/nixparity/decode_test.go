package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

func TestNormalizerURI(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := filepath.Join(base, "w")
	n := normalizer{root: root}
	outside := filepath.Join(base, "w2", "x.nix")
	tests := []struct {
		name, uri, want string
	}{
		{"inside the root", fileuri.FromPath(filepath.Join(root, "a", "b.nix")), "a/b.nix"},
		{"escaped characters", fileuri.FromPath(filepath.Join(root, "a b.nix")), "a b.nix"},
		{"a sibling sharing the prefix", fileuri.FromPath(outside), filepath.ToSlash(outside)},
		{"not a file", "untitled:Untitled-1", "untitled:Untitled-1"},
	}
	for _, tt := range tests {
		if got := n.uri(tt.uri); got != tt.want {
			t.Errorf("%s: uri(%q) = %q, want %q", tt.name, tt.uri, got, tt.want)
		}
	}
}

func decodeContextFor(t *testing.T, text string) decodeContext {
	t.Helper()

	root := t.TempDir()
	return decodeContext{norm: normalizer{root: root}, doc: docstore.NewDocument(fileuri.FromPath(filepath.Join(root, "a.nix")), languageID, 1, text)}
}

func TestDecodersNormalizeEveryAnswerShape(t *testing.T) {
	t.Parallel()

	ctx := decodeContextFor(t, "abc\n")
	uri := ctx.doc.URI
	span := `{"start":{"line":1,"character":2},"end":{"line":1,"character":4}}`
	location := `{"uri":"` + uri + `","range":` + span + `}`
	edit := `{"range":` + span + `,"newText":"x"}`
	tests := []struct {
		name    string
		decode  decoder
		raw     string
		outcome outcome
		items   []string
	}{
		{"one location", decodeLocations, location, outcomeAnswered, []string{"a.nix:1:2-1:4"}},
		{"location array, deduplicated", decodeLocations, `[` + location + `,` + location + `]`, outcomeAnswered, []string{"a.nix:1:2-1:4"}},
		{"location links", decodeLocations, `[{"targetUri":"` + uri + `","targetRange":` + span + `,"targetSelectionRange":` + span + `}]`, outcomeAnswered, []string{"a.nix:1:2-1:4"}},
		{"no locations", decodeLocations, `[]`, outcomeEmpty, nil},
		{"diagnostics by start", decodeDiagnostics, `{"uri":"` + uri + `","diagnostics":[{"range":` + span + `,"code":7,"message":"m"}]}`, outcomeAnswered, []string{"1:2"}},
		{"no diagnostics", decodeDiagnostics, `{"uri":"` + uri + `","diagnostics":[]}`, outcomeEmpty, nil},
		{"nested symbols", decodeSymbols, `[{"name":"a","selectionRange":` + span + `,"children":[{"name":"b","selectionRange":` + span + `}]}]`, outcomeAnswered, []string{"1:2 a", "1:2 b"}},
		{"flat symbols", decodeSymbols, `[{"name":"a","location":` + location + `}]`, outcomeAnswered, []string{"1:2 a"}},
		{"folding ranges", decodeFolding, `[{"startLine":3,"endLine":5,"kind":"region"},{"startLine":1,"endLine":2}]`, outcomeAnswered, []string{"1-2", "3-5"}},
		{"rename through changes", decodeRename, `{"changes":{"` + uri + `":[` + edit + `]}}`, outcomeAnswered, []string{"a.nix:1:2-1:4=x"}},
		{"rename through document changes", decodeRename, `{"documentChanges":[{"textDocument":{"uri":"` + uri + `","version":null},"edits":[` + edit + `]}]}`, outcomeAnswered, []string{"a.nix:1:2-1:4=x"}},
		{"an empty rename", decodeRename, `{"changes":{}}`, outcomeEmpty, nil},
		{"hover with a range", decodeHover, `{"contents":{"kind":"markdown","value":"v"},"range":` + span + `}`, outcomeAnswered, []string{"1:2-1:4"}},
		{"hover without a range", decodeHover, `{"contents":"v"}`, outcomeAnswered, nil},
		{"hover as marked strings", decodeHover, `{"contents":["",{"language":"nix","value":"v"}]}`, outcomeAnswered, nil},
		{"hover without text", decodeHover, `{"contents":{"kind":"markdown","value":" "}}`, outcomeEmpty, nil},
		{"completion list", decodeCompletion, `{"isIncomplete":true,"items":[{"label":"b","documentation":"d"},{"label":"a"}]}`, outcomeAnswered, []string{"a", "b"}},
		{"completion array", decodeCompletion, `[{"label":"a","kind":6}]`, outcomeAnswered, []string{"a"}},
		{"empty completion list", decodeCompletion, `{"items":[]}`, outcomeEmpty, nil},
	}
	for _, tt := range tests {
		got, err := tt.decode(ctx, json.RawMessage(tt.raw))
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got.outcome != tt.outcome || !slices.Equal(got.items, tt.items) {
			t.Errorf("%s: got %d %q, want %d %q", tt.name, got.outcome, got.items, tt.outcome, tt.items)
		}
	}
}

func TestDecodersRejectMalformedAnswers(t *testing.T) {
	t.Parallel()

	ctx := decodeContextFor(t, "abc\n")
	tests := []struct {
		name   string
		decode decoder
		raw    string
	}{
		{"location without a uri", decodeLocations, `[{"range":{}}]`},
		{"symbol without a position", decodeSymbols, `[{"name":"a"}]`},
		{"folding as an object", decodeFolding, `{"startLine":1}`},
		{"hover contents as a number", decodeHover, `{"contents":3}`},
		{"completion as a number", decodeCompletion, `3`},
		{"overlapping edits", decodeFormatting, `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":2}},"newText":"x"},{"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":3}},"newText":"y"}]`},
		{"backward edit", decodeFormatting, `[{"range":{"start":{"line":0,"character":2},"end":{"line":0,"character":1}},"newText":"x"}]`},
	}
	for _, tt := range tests {
		if got, err := tt.decode(ctx, json.RawMessage(tt.raw)); err == nil {
			t.Errorf("%s: decoded as %+v", tt.name, got)
		}
	}
}

func TestDecodeFormattingComparesTheResultingText(t *testing.T) {
	t.Parallel()

	ctx := decodeContextFor(t, "a  b\nc\n")
	whole := `[{"range":{"start":{"line":0,"character":0},"end":{"line":2,"character":0}},"newText":"a b\nc\n"}]`
	minimal := `[{"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":2}},"newText":""}]`
	a, err := decodeFormatting(ctx, json.RawMessage(whole))
	if err != nil {
		t.Fatal(err)
	}
	b, err := decodeFormatting(ctx, json.RawMessage(minimal))
	if err != nil {
		t.Fatal(err)
	}
	if a.outcome != outcomeAnswered || !slices.Equal(a.items, b.items) {
		t.Errorf("edits with the same result gave %+v and %+v", a, b)
	}
	unchanged, err := decodeFormatting(ctx, json.RawMessage(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.outcome != outcomeAnswered || slices.Equal(unchanged.items, a.items) {
		t.Errorf("no edits gave %+v, want an answer naming the unchanged text", unchanged)
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	ctx := decodeContextFor(t, "abc\n")
	spec := featureDefinition.spec()
	const elapsed = 3 * time.Millisecond
	tests := []struct {
		name    string
		reply   reply
		outcome outcome
		latency time.Duration
		wantErr string
	}{
		{"timed out", reply{state: replyTimedOut}, outcomeTimeout, 0, ""},
		{"died", reply{state: replyDied}, outcomeCrashed, 0, ""},
		{"method not found", reply{rpcErr: jsonrpc.NewError(jsonrpc.CodeMethodNotFound, "no"), elapsed: elapsed}, outcomeUnsupported, 0, ""},
		{"refused", reply{rpcErr: jsonrpc.NewError(-32001, "no"), elapsed: elapsed}, outcomeRefused, elapsed, ""},
		{"null", reply{result: json.RawMessage("null"), elapsed: elapsed}, outcomeEmpty, elapsed, ""},
		{"empty array", reply{result: json.RawMessage("[]"), elapsed: elapsed}, outcomeEmpty, elapsed, ""},
		{"malformed", reply{result: json.RawMessage(`"x"`), elapsed: elapsed}, outcomeMalformed, elapsed, "definition"},
	}
	for _, tt := range tests {
		got, err := classify(spec, ctx, tt.reply)
		if got.outcome != tt.outcome || got.latency != tt.latency {
			t.Errorf("%s: got %+v, want outcome %d after %s", tt.name, got, tt.outcome, tt.latency)
		}
		if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("%s: error = %v, want one naming %q", tt.name, err, tt.wantErr)
		}
	}
}
