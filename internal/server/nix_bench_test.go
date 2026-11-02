package server

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type nixPosition struct {
	uri             string
	line, character int
}

func benchPositions(b *testing.B, files map[string]string, root string) []nixPosition {
	b.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	var out []nixPosition
	for _, name := range names {
		text := files[name]
		syntax.Inspect(syntax.Parse(text).Root, func(n syntax.Node) bool {
			if id, ok := n.(*syntax.Ident); ok {
				at := id.Span().Start
				line := strings.Count(text[:at], "\n")
				out = append(out, nixPosition{uri: fileuri.FromPath(filepath.Join(root, name)), line: line, character: at - (strings.LastIndexByte(text[:at], '\n') + 1)})
			}
			return true
		})
	}
	return out
}

func corpusBenchProject(b *testing.B, fixture string) (*testClient, []nixPosition) {
	b.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "corpus", "nix", fixture))
	if err != nil {
		b.Fatal(err)
	}
	files := corpusFiles(b, root)
	c, _ := startClient(b, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {}}`)
	for name, text := range files {
		c.notify("textDocument/didOpen", openLanguageParams(fileuri.FromPath(filepath.Join(root, name)), "nix", text))
	}
	return c, benchPositions(b, files, root)
}

func BenchmarkNixNavigation(b *testing.B) {
	for _, method := range []string{"definition", "references", "hover"} {
		b.Run(method, func(b *testing.B) {
			c, positions := corpusBenchProject(b, "dendritic")
			first := positions[0]
			c.call("textDocument/"+method, positionParams(first.uri, first.line, first.character, ""))
			i := 0
			b.ResetTimer()
			for b.Loop() {
				at := positions[i%len(positions)]
				i++
				c.call("textDocument/"+method, positionParams(at.uri, at.line, at.character, `, "context": {"includeDeclaration": true}`))
			}
		})
	}
}

func BenchmarkNixFirstDefinition(b *testing.B) {
	for b.Loop() {
		c, positions := corpusBenchProject(b, "dendritic")
		at := positions[0]
		start := time.Now()
		c.call("textDocument/definition", positionParams(at.uri, at.line, at.character, ""))
		b.ReportMetric(float64(time.Since(start).Microseconds()), "first-us")
		c.close()
	}
}

func BenchmarkNixRelinkAfterAnEdit(b *testing.B) {
	c, positions := corpusBenchProject(b, "dendritic")
	at := positions[0]
	params := positionParams(at.uri, at.line, at.character, "")
	c.call("textDocument/definition", params)
	version := 1
	b.ResetTimer()
	for b.Loop() {
		version++
		c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": %d}, "contentChanges": [{"range": {"start": {"line": 0, "character": 0}, "end": {"line": 0, "character": 0}}, "text": " "}]}`, at.uri, version))
		c.call("textDocument/definition", params)
	}
}

func largeNixFile(lines int) string {
	var sb strings.Builder
	sb.WriteString("{ lib, pkgs, ... }:\nrec {\n")
	for i := range lines {
		fmt.Fprintf(&sb, "  attr%d = lib.mkIf (attr%d != null) (pkgs.callPackage ./pkg%d.nix { inherit lib; });\n", i, max(i-1, 0), i)
	}
	sb.WriteString("}\n")
	return sb.String()
}

func BenchmarkNixKeystroke(b *testing.B) {
	for _, lines := range []int{5000, 12000} {
		b.Run(fmt.Sprintf("lines=%d", lines), func(b *testing.B) {
			root := b.TempDir()
			uri := fileuri.FromPath(filepath.Join(root, "big.nix"))
			c, _ := startClient(b, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
			c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", largeNixFile(lines)))
			doc := `{"textDocument": {"uri": "` + uri + `"}}`
			c.call("textDocument/diagnostic", doc)
			version := 1
			for b.Loop() {
				version++
				c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": %d}, "contentChanges": [{"range": {"start": {"line": 2, "character": 2}, "end": {"line": 2, "character": 2}}, "text": "x"}]}`, uri, version))
				c.call("textDocument/diagnostic", doc)
			}
		})
	}
}
