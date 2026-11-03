package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const unformatted = "f(){\necho  a\n}\ng(){\necho  b\n}\n"

func TestFormatting(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", unformatted))
	edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/formatting",
		`{"textDocument": {"uri": "file:///w/a.sh"}, "options": {"tabSize": 2, "insertSpaces": true}}`))
	var newTexts []string
	for _, e := range edits {
		newTexts = append(newTexts, e.NewText)
	}
	joined := strings.Join(newTexts, "")
	if !strings.Contains(joined, "  echo a\n") || !strings.Contains(joined, "f() {\n") {
		t.Errorf("edits = %+v", edits)
	}
	if len(edits) != 2 {
		t.Errorf("got %d edits, want one hunk per function", len(edits))
	}

	ranged := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/rangeFormatting",
		`{"textDocument": {"uri": "file:///w/a.sh"}, "range": {"start": {"line": 4, "character": 0}, "end": {"line": 4, "character": 3}}, "options": {"tabSize": 4, "insertSpaces": false}}`))
	if len(ranged) != 1 || ranged[0].Range.Start.Line != 3 || !strings.Contains(ranged[0].NewText, "\techo b") {
		t.Errorf("range edits = %+v, want only the hunk of g", ranged)
	}
}

func TestFormattingErrors(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}, "initializationOptions": {"format": {"indent": 3}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/broken.sh", "if true; then\n"))
	msg := c.call("textDocument/formatting", `{"textDocument": {"uri": "file:///w/broken.sh"}, "options": {"tabSize": 2, "insertSpaces": true}}`)
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeRequestFailed || !strings.Contains(msg.Error.Message, "syntax errors") {
		t.Errorf("error = %+v", msg.Error)
	}

	c.notify("textDocument/didOpen", openParams("file:///w/ok.sh", unformatted))
	edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/formatting",
		`{"textDocument": {"uri": "file:///w/ok.sh"}, "options": {"tabSize": 2, "insertSpaces": true}}`))
	if len(edits) == 0 || !strings.Contains(edits[0].NewText, "   echo a") {
		t.Errorf("configured indent ignored: %+v", edits)
	}

	c.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"format": {"enable": false}}}}`)
	disabled := c.call("textDocument/formatting", `{"textDocument": {"uri": "file:///w/ok.sh"}, "options": {"tabSize": 2, "insertSpaces": true}}`)
	if disabled.Error == nil || !strings.Contains(disabled.Error.Message, "disabled") {
		t.Errorf("disabled formatting error = %+v", disabled.Error)
	}
}

func TestFormattingNixMatchesWholeFileInRanges(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	const uri = "file:///w/default.nix"
	c.notify("textDocument/didOpen", openParams(uri, "{\n  a=1;\n\n  b=2;\n}\n"))
	whole := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/formatting",
		`{"textDocument": {"uri": "`+uri+`"}, "options": {"tabSize": 8, "insertSpaces": false}}`))
	if len(whole) != 2 || whole[0].NewText != "  a = 1;\n" || whole[1].NewText != "  b = 2;\n" {
		t.Fatalf("whole-file edits = %+v", whole)
	}
	ranged := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/rangeFormatting",
		`{"textDocument": {"uri": "`+uri+`"}, "range": {"start": {"line": 3, "character": 0}, "end": {"line": 3, "character": 3}}, "options": {"tabSize": 8, "insertSpaces": false}}`))
	if len(ranged) != 1 || ranged[0] != whole[1] {
		t.Errorf("range edits = %+v, want only the whole-file edit of b: %+v", ranged, whole[1])
	}
}

func TestFormattingNixRefusesSyntaxErrors(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	const uri = "file:///w/broken.nix"
	c.notify("textDocument/didOpen", openParams(uri, "{ a = ; }\n"))
	resp := c.call("textDocument/formatting", `{"textDocument": {"uri": "`+uri+`"}, "options": {"tabSize": 2, "insertSpaces": true}}`)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "1:7") {
		t.Errorf("formatting a broken Nix file = %+v, want an error with its position", resp)
	}
}

func TestNixOnTypeFormatsTheBindingASemicolonEnds(t *testing.T) {
	t.Parallel()

	const uri = "file:///w/a.nix"
	text := "{\n  a   =   1;\n  b   =   2;\n}\n"
	c, _ := startClient(t, `{"capabilities": {}}`)
	c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", text))
	edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/onTypeFormatting", `{"textDocument": {"uri": "`+uri+`"}, "position": {"line": 1, "character": 13}, "ch": ";", "options": {"tabSize": 2, "insertSpaces": true}}`))
	if len(edits) != 1 || edits[0].Range.Start.Line != 1 || !strings.Contains(edits[0].NewText, "a = 1;") {
		t.Errorf("edits = %+v, want only the binding on line 1", edits)
	}
	nothing := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/onTypeFormatting", `{"textDocument": {"uri": "`+uri+`"}, "position": {"line": 1, "character": 3}, "ch": ";", "options": {"tabSize": 2, "insertSpaces": true}}`))
	if len(nothing) != 0 {
		t.Errorf("a position that ends no binding got edits %+v", nothing)
	}
}

func TestRangeFormattingKeepsWhatAStringMeans(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, file, src, selected, want, shell string
	}{
		{
			name: "writeShellScript whose lines a blank line divides", file: "default.nix",
			src:      "{ pkgs }:\n{\n  a = pkgs.writeShellScript \"x\" ''\n          first\n\n          second\n  '';\n}\n",
			selected: "second",
			want:     "{ pkgs }:\n{\n  a = pkgs.writeShellScript \"x\" ''\n    first\n\n    second\n  '';\n}\n",
			shell:    "first\n\nsecond\n",
		},
		{
			name: "writeShellScript selected at its first line", file: "default.nix",
			src:      "{ pkgs }:\n{\n  a = pkgs.writeShellScript \"x\" ''\n          first\n\n            second\n  '';\n}\n",
			selected: "first",
			want:     "{ pkgs }:\n{\n  a = pkgs.writeShellScript \"x\" ''\n    first\n\n      second\n  '';\n}\n",
			shell:    "first\n\n  second\n",
		},
		{
			name: "runCommand already laid out keeps its shell", file: "default.nix",
			src:      "{ pkgs }:\npkgs.runCommand \"x\" { } ''\n  mkdir   $out\n\n  echo a > $out/a\n''\n",
			selected: "echo a",
			want:     "{ pkgs }:\npkgs.runCommand \"x\" { } ''\n  mkdir   $out\n\n  echo a > $out/a\n''\n",
			shell:    "mkdir   $out\n\necho a > $out/a\n",
		},
		{
			name: "runCommand out of place", file: "default.nix",
			src:      "{ pkgs }:\npkgs.runCommand \"x\" { } ''\n      mkdir $out\n\n      echo a > $out/a\n    ''\n",
			selected: "mkdir",
			want:     "{ pkgs }:\npkgs.runCommand \"x\" { } ''\n  mkdir $out\n\n  echo a > $out/a\n''\n",
			shell:    "mkdir $out\n\necho a > $out/a\n",
		},
		{
			name: "a phase with an interpolation", file: "default.nix",
			src:      "{ stdenv, foo }:\nstdenv.mkDerivation {\n  name = \"x\";\n  buildPhase = ''\n        make ${ foo }\n          make  install\n\n     ${foo}/bin/x\n  '';\n}\n",
			selected: "make  install",
			want:     "{ stdenv, foo }:\nstdenv.mkDerivation {\n  name = \"x\";\n  buildPhase = ''\n       make ${foo}\n         make  install\n\n    ${foo}/bin/x\n  '';\n}\n",
			shell:    "   make ",
		},
		{
			name: "home.file", file: "default.nix",
			src:      "{\n  home.file.\"bin/x.sh\".text = ''\n         echo a\n\n         echo b\n    '';\n}\n",
			selected: "echo b",
			want:     "{\n  home.file.\"bin/x.sh\".text = ''\n    echo a\n\n    echo b\n  '';\n}\n",
			shell:    "echo a\n\necho b\n",
		},
		{
			name: "a second string stays out of it", file: "default.nix",
			src:      "{ pkgs }:\n{\n  a = pkgs.writeShellScript \"x\" ''\n        one\n\n        two\n  '';\n  b = pkgs.writeShellScript \"y\" ''\n        three\n\n        four\n  '';\n}\n",
			selected: "four",
			want:     "{ pkgs }:\n{\n  a = pkgs.writeShellScript \"x\" ''\n        one\n\n        two\n  '';\n  b = pkgs.writeShellScript \"y\" ''\n    three\n\n    four\n  '';\n}\n",
			shell:    "three\n\nfour\n",
		},
		{
			name: "a justfile recipe keeps one indentation", file: "justfile",
			src:      "build:\n  echo   a\n\n  echo   b\n",
			selected: "echo   b",
			want:     "build:\n    echo   a\n\n    echo   b\n",
			shell:    "echo   b\n",
		},
		{
			name: "a Makefile recipe stays verbatim", file: "Makefile",
			src:      "X=1\n\nbuild:\n\techo   a\n",
			selected: "echo   a",
			want:     "X=1\n\nbuild:\n\techo   a\n",
			shell:    "echo   a\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := openNativeProject(t, `"capabilities": {}`, map[string]string{tt.file: tt.src}, tt.file)
			line, character := p.pos(tt.file, tt.selected, 0)
			params, err := json.Marshal(map[string]any{
				"textDocument": map[string]any{"uri": p.uri(tt.file)},
				"range": map[string]any{
					"start": map[string]any{"line": line, "character": character},
					"end":   map[string]any{"line": line, "character": character + len(tt.selected)},
				},
				"options": map[string]any{"tabSize": 2, "insertSpaces": true},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := applyLSPEdits(tt.src, decodeResult[[]lsp.TextEdit](t, p.c.call("textDocument/rangeFormatting", string(params))))
			if got != tt.want {
				t.Fatalf("after the edits = %q, want %q", got, tt.want)
			}
			language, ok := host.Of(p.uri(tt.file), "")
			if !ok {
				t.Fatalf("%s is no host language", tt.file)
			}
			if projected := language.Project(docstore.NewDocument(p.uri(tt.file), "", 0, got)); !strings.Contains(projected.Doc.Text(), tt.shell) {
				t.Errorf("shell after the edits = %q, want it to hold %q", projected.Doc.Text(), tt.shell)
			}
		})
	}
}

func TestRangeFormattingOnlyTouchesTheLinesOfTheRange(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	const uri = "file:///w/default.nix"
	c.notify("textDocument/didOpen", openParams(uri, "{\n  a=1;\n  c = 3;\n  b=2;\n}\n"))
	for _, tt := range []struct {
		name string
		rng  string
		want int
	}{
		{"from the start of the tidy line", `{"start": {"line": 2, "character": 0}, "end": {"line": 2, "character": 3}}`, 0},
		{"a whole line up to the next", `{"start": {"line": 2, "character": 0}, "end": {"line": 3, "character": 0}}`, 0},
		{"a cursor at the start of the tidy line", `{"start": {"line": 2, "character": 0}, "end": {"line": 2, "character": 0}}`, 0},
		{"a cursor on the line", `{"start": {"line": 3, "character": 0}, "end": {"line": 3, "character": 0}}`, 1},
	} {
		edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/rangeFormatting",
			`{"textDocument": {"uri": "`+uri+`"}, "range": `+tt.rng+`, "options": {"tabSize": 2, "insertSpaces": true}}`))
		if len(edits) != tt.want {
			t.Errorf("%s: edits = %+v, want %d", tt.name, edits, tt.want)
		}
	}
}
