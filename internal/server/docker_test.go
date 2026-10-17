package server

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

var dockerProject = map[string]string{
	"Dockerfile": "FROM golang:1.23 AS build\nENV APP_HOME=/app\nCOPY go.mod ./\nRUN cd \"$APP_HOME\" && echo $1 $UNSET\nCMD echo done\n\nFROM alpine\nCOPY --from=build /app /app\n",
	"go.mod":     "module app\n",
}

func TestDockerfileThroughTheServer(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, dockerProject, "Dockerfile")
	uri := p.uri("Dockerfile")
	if got, want := codes(pullDiagnostics(t, p.c, uri)), []string{"SC2086", "SC2086", "SL7004"}; !slices.Equal(got, want) {
		t.Errorf("diagnostics = %v, want %v", got, want)
	}
	defs := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params("Dockerfile", "APP_HOME", 1, "")))
	if want := p.at("Dockerfile", "APP_HOME", 0); len(defs) != 1 || defs[0] != want {
		t.Errorf("definition of $APP_HOME in RUN = %+v, want %+v", defs, want)
	}
	defs = decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params("Dockerfile", "build", 1, "")))
	if want := p.at("Dockerfile", "build", 0); len(defs) != 1 || defs[0] != want {
		t.Errorf("definition of --from=build = %+v, want %+v", defs, want)
	}
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params("Dockerfile", "COPY", 0, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "COPY") {
		t.Errorf("hover on COPY = %+v", hover)
	}
	symbols := decodeResult[[]lsp.DocumentSymbol](t, p.c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "`+uri+`"}}`))
	if !slices.ContainsFunc(symbols, func(s lsp.DocumentSymbol) bool { return s.Name == "build" && s.Kind == lsp.SymbolModule }) {
		t.Errorf("document symbols = %+v, want the build stage as a module", symbols)
	}
	links := decodeResult[[]lsp.DocumentLink](t, p.c.call("textDocument/documentLink", `{"textDocument": {"uri": "`+uri+`"}}`))
	if len(links) != 1 || links[0].Target != p.uri("go.mod") {
		t.Errorf("links = %+v, want go.mod", links)
	}
	edit := decodeResult[*lsp.WorkspaceEdit](t, p.c.call("textDocument/rename", p.params("Dockerfile", "build", 0, `, "newName": "compile"`)))
	if edit == nil || len(edit.DocumentChanges) != 1 || len(edit.DocumentChanges[0].Edits) != 2 {
		t.Errorf("rename of the stage = %+v, want both occurrences", edit)
	}
}
