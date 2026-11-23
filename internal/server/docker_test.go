package server

import (
	"encoding/json"
	"fmt"
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
	if got, want := codes(pullDiagnostics(t, p.c, uri)), []string{"DL3003", "DL3006", "DL3045", "SC2086", "SC2086", "SL7004"}; !slices.Equal(got, want) {
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

func TestDockerfileHonoursHadolintSettings(t *testing.T) {
	t.Parallel()

	project := map[string]string{
		".hadolint.yaml": "ignored: [DL3006]\noverride:\n  error: [DL3003]\n",
		"Dockerfile":     "FROM alpine\nRUN echo $1\n# hadolint ignore=SC2086,DL3059\nRUN echo $2\nRUN cd /tmp && echo \"$3\"\n# hadolint global ignore=DL3045\nCOPY a b\n",
		"a":              "",
	}
	p := openNativeProject(t, nativeCapabilities, project, "Dockerfile")
	items := pullDiagnostics(t, p.c, p.uri("Dockerfile"))
	var got []string
	for _, d := range items {
		got = append(got, fmt.Sprintf("%d %s %d", d.Range.Start.Line+1, d.Code, d.Severity))
	}
	slices.Sort(got)
	want := []string{"2 SC2086 3", "5 DL3003 1"}
	if !slices.Equal(got, want) {
		t.Errorf("diagnostics = %q, want %q", got, want)
	}
}

func TestDockerfileWithABrokenHadolintConfigurationIsCheckedWithout(t *testing.T) {
	t.Parallel()

	project := map[string]string{
		".hadolint.yaml": "ignored: DL3006\n",
		"Dockerfile":     "FROM alpine\n",
	}
	p := openNativeProject(t, nativeCapabilities, project, "Dockerfile")
	if got, want := codes(pullDiagnostics(t, p.c, p.uri("Dockerfile"))), []string{"DL3006"}; !slices.Equal(got, want) {
		t.Errorf("diagnostics = %v, want %v", got, want)
	}
	p.c.awaitNotification(logMessageMethod, func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), ".hadolint.yaml") && strings.Contains(string(raw), "line 1, column 10")
	})
}

func TestDockerfileHonoursBakeNamedContexts(t *testing.T) {
	t.Parallel()

	text := "FROM infra\nCOPY --from=deps / /x\n\nFROM base\n"
	project := map[string]string{
		"build.hcl":        "target \"t\" {\n  context = \"build\"\n  contexts = {\n    infra = \"docker-image://localhost:5000/infra:latest\"\n    deps  = \"target:deps\"\n    base  = \"docker-image://alpine\"\n  }\n}\n",
		"build/Dockerfile": text,
		"other/Dockerfile": text,
	}
	p := openNativeProject(t, nativeCapabilities, project, "build/Dockerfile", "other/Dockerfile")
	baked := pullDiagnostics(t, p.c, p.uri("build/Dockerfile"))
	if got, want := codes(baked), []string{"DL3006"}; !slices.Equal(got, want) {
		t.Errorf("diagnostics of the Dockerfile build.hcl names = %v, want %v", got, want)
	}
	if len(baked) == 1 && (baked[0].Range.Start.Line != 3 || !strings.Contains(baked[0].Message, "docker-image://alpine")) {
		t.Errorf("DL3006 = %+v, want it on `FROM base` naming the untagged context value", baked[0])
	}
	if got, want := codes(pullDiagnostics(t, p.c, p.uri("other/Dockerfile"))), []string{"DL3006", "DL3006", "SL7005"}; !slices.Equal(got, want) {
		t.Errorf("diagnostics of a Dockerfile no bake target names = %v, want %v", got, want)
	}
}
