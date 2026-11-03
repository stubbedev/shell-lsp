package server

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

func pullDiagnostics(t *testing.T, c *testClient, uri string) []lsp.Diagnostic {
	t.Helper()

	return decodeResult[lsp.FullDocumentDiagnosticReport](t, c.call("textDocument/diagnostic", `{"textDocument": {"uri": "`+uri+`"}}`)).Items
}

func embeddedOnly(items []lsp.Diagnostic) []lsp.Diagnostic {
	return slices.DeleteFunc(items, func(d lsp.Diagnostic) bool { return !strings.HasPrefix(d.Code, "SL300") })
}

func TestEmbeddedProgramDiagnostics(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+pullCapabilities+`}`)
	text := "#!/bin/bash\n# 😀 emoji shifts UTF-16 columns\nawk '{ print \"x }' a.txt\nsed -e 's/a/b/' -e 'k' f\nx=$(jq -r \".a | | .b\" f)\nawk -f - <<'EOF'\nBEGIN {\nEOF\n"
	c.notify("textDocument/didOpen", openParams("file:///w/e.bash", text))
	got := embeddedOnly(pullDiagnostics(t, c, "file:///w/e.bash"))
	type located struct {
		code, message      string
		line, start, endCh uint32
	}
	want := []located{
		{"SL3001", "awk: unterminated string", 2, 13, 17},
		{"SL3002", "sed: unknown command `k`", 3, 20, 21},
		{"SL3003", "jq: unexpected `|`", 4, 16, 17},
		{"SL3001", "awk: `{` is never closed", 6, 6, 7},
	}
	var have []located
	for _, d := range got {
		if d.Range.Start.Line != d.Range.End.Line || d.Source != "shell-lsp" || d.Severity != lsp.SeverityError {
			t.Errorf("diagnostic %+v", d)
		}
		have = append(have, located{d.Code, d.Message, d.Range.Start.Line, d.Range.Start.Character, d.Range.End.Character})
	}
	if !slices.Equal(have, want) {
		t.Errorf("embedded diagnostics\n got %+v\nwant %+v", have, want)
	}

	c.notify("textDocument/didChange", `{"textDocument": {"uri": "file:///w/e.bash", "version": 2}, "contentChanges": [{"text": "awk '{ print \"x\" }' a.txt\n"}]}`)
	if fixed := embeddedOnly(pullDiagnostics(t, c, "file:///w/e.bash")); len(fixed) != 0 {
		t.Errorf("diagnostics after the fix = %+v, want none", fixed)
	}
}

func TestEmbeddedProgramDiagnosticsFollowLintSettings(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\nawk '{' f\nsed 'k' f\n"
	for _, tt := range []struct {
		options string
		want    []string
	}{
		{`{}`, []string{"SL3001", "SL3002"}},
		{`{"lint": {"exclude": ["SL3001"]}}`, []string{"SL3002"}},
		{`{"lint": {"enable": false}}`, nil},
	} {
		c, _ := startClient(t, `{`+linkingCapabilities+`, "initializationOptions": `+tt.options+`}`)
		c.notify("textDocument/didOpen", openParams("file:///w/s.bash", text))
		var codes []string
		for _, d := range embeddedOnly(pullDiagnostics(t, c, "file:///w/s.bash")) {
			codes = append(codes, d.Code)
			if d.CodeDescription == nil || !strings.HasSuffix(d.CodeDescription.Href, "rules.md#"+strings.ToLower(d.Code)) {
				t.Errorf("%s links to %+v, want its docs/rules.md section", d.Code, d.CodeDescription)
			}
		}
		if !slices.Equal(codes, tt.want) {
			t.Errorf("with %s: codes = %v, want %v", tt.options, codes, tt.want)
		}
	}
}

func TestEmbeddedProgramSemanticTokens(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/t.bash", "awk '/x/ { print NR }' f\n"))
	full := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "file:///w/t.bash"}}`))
	want := []uint32{
		0, 0, 3, uint32(semantic.TypeFunction), uint32(semantic.ModExternal),
		0, 4, 1, uint32(semantic.TypeString), 0,
		0, 1, 3, uint32(semantic.TypeRegexp), 0,
		0, 6, 5, uint32(semantic.TypeKeyword), 0,
		0, 6, 2, uint32(semantic.TypeVariable), uint32(semantic.ModDefaultLibrary),
		0, 4, 1, uint32(semantic.TypeString), 0,
	}
	if !slices.Equal(full.Data, want) {
		t.Errorf("tokens = %v\nwant %v", full.Data, want)
	}
}

func TestEmbeddedProgramHover(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/h.bash", "awk '{ print length($0) }' f | jq 'map(.a)' | sed -n p\n"))
	for _, tt := range []struct {
		character  uint32
		want       string
		start, end uint32
	}{
		{15, "length([s])", 13, 19},
		{36, "map(f)", 35, 38},
		{53, "Prints the pattern space.", 53, 54},
	} {
		got := decodeResult[lsp.Hover](t, c.call("textDocument/hover", `{"textDocument": {"uri": "file:///w/h.bash"}, "position": {"line": 0, "character": `+strconv.FormatUint(uint64(tt.character), 10)+`}}`))
		if !strings.Contains(got.Contents.Value, tt.want) || got.Range == nil || got.Range.Start.Character != tt.start || got.Range.End.Character != tt.end {
			t.Errorf("hover at %d = %+v, want %q over %d-%d", tt.character, got, tt.want, tt.start, tt.end)
		}
	}
}

func TestEmbeddedProgramInNixDiagnostics(t *testing.T) {
	t.Parallel()

	const uri = "file:///w/p.nix"
	for _, tt := range []struct {
		name, line, code string
		col              int
	}{
		{"a bin path", "    ${pkgs.jq}/bin/jq '.a | | .b' f", "SL3003", len("    ${pkgs.jq}/bin/jq '.a | ")},
		{"an antiquotation splits the program", "    ${pkgs.jq}/bin/jq '.a${x} | | .b' f", "SL3003", len("    ${pkgs.jq}/bin/jq '.a${x} | ")},
		{"getExe", "    ${lib.getExe pkgs.gawk} '{ print \"x }' f", "SL3001", len("    ${lib.getExe pkgs.gawk} '{ print ")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := startClient(t, `{`+pullCapabilities+`}`)
			text := "{ pkgs, lib, x }: pkgs.writeShellScript \"s\" ''\n" + tt.line + "\n''\n"
			c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", text))
			got := embeddedOnly(pullDiagnostics(t, c, uri))
			if len(got) != 1 || got[0].Code != tt.code || got[0].Range.Start.Line != 1 || got[0].Range.Start.Character != uint32(tt.col) {
				t.Errorf("diagnostics = %+v, want %s at column %d", got, tt.code, tt.col)
			}
		})
	}
}

func TestEmbeddedProgramHoverInNix(t *testing.T) {
	t.Parallel()

	const uri = "file:///w/h.nix"
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	text := "{ pkgs }: pkgs.writeShellScript \"s\" ''\n  ${pkgs.jq}/bin/jq 'map(.a)' f\n''\n"
	c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", text))
	character := strconv.Itoa(len("  ${pkgs.jq}/bin/jq 'm"))
	got := decodeResult[lsp.Hover](t, c.call("textDocument/hover", `{"textDocument": {"uri": "`+uri+`"}, "position": {"line": 1, "character": `+character+`}}`))
	if !strings.Contains(got.Contents.Value, "map(f)") {
		t.Errorf("hover = %+v", got)
	}
}

func TestQuickFixesInsideANixStringEditTheHost(t *testing.T) {
	t.Parallel()

	const (
		name   = "embedded.nix"
		source = "{ writeText }:\nwriteText \"x.nix\" ''\n  { pkgs }:\n  pkgz.hello\n''\n"
	)
	p := openNativeProject(t, actionCapabilities, map[string]string{name: source}, name)
	at := p.at(name, "pkgz", 0).Range
	got := codeActions(t, p.c, fmt.Sprintf(`{"textDocument": {"uri": %q}, "range": {"start": {"line": %d, "character": %d}, "end": {"line": %d, "character": %d}}, "context": {"diagnostics": []}}`, p.uri(name), at.Start.Line, at.Start.Character, at.End.Line, at.End.Character))
	for title, want := range map[string]string{
		"Change to `pkgs`":   "{ writeText }:\nwriteText \"x.nix\" ''\n  { pkgs }:\n  pkgs.hello\n''\n",
		"Add `pkgz` to the ": "{ writeText }:\nwriteText \"x.nix\" ''\n  { pkgs, pkgz }:\n  pkgz.hello\n''\n",
	} {
		action, offered := titled(got, title)
		if !offered || action.Edit == nil {
			t.Fatalf("no action %q in %+v", title, got)
		}
		edits := action.Edit.Changes[p.uri(name)]
		if len(edits) == 0 && len(action.Edit.DocumentChanges) == 1 {
			edits = action.Edit.DocumentChanges[0].Edits
		}
		if edited := applyLSPEdits(source, edits); edited != want {
			t.Errorf("%s\n got %q\nwant %q", title, edited, want)
		}
	}
}
