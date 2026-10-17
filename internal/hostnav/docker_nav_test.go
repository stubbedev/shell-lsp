package hostnav

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const navDockerfile = `ARG VERSION=1.0
FROM golang:1.23 AS Build
ARG VERSION
ENV APP_HOME=/app
WORKDIR $APP_HOME
COPY go.mod main.go ./
RUN go build -ldflags "-X main.version=$VERSION" -o "$APP_HOME/app" .

FROM build AS test
RUN go test ./...

FROM alpine:3.20
COPY --from=build /app/app /usr/local/bin/app
CMD ["app"]
`

func TestDockerVariablesAndStages(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{"Dockerfile": navDockerfile, "go.mod": "module app\n", "main.go": "package main\n"})
	g, sym := tr.at("Dockerfile", "APP_HOME", 0)
	expectStrings(t, "references of APP_HOME", tr.describe(References(g, sym, true)), []string{"Dockerfile:4:APP_HOME", "Dockerfile:5:APP_HOME", "Dockerfile:7:APP_HOME"})
	g, sym = tr.shellAt("Dockerfile", "VERSION", 2)
	expectStrings(t, "definition of $VERSION in RUN", tr.locations(g.definitions(sym)), []string{"Dockerfile:3:VERSION"})
	g, sym = tr.at("Dockerfile", "build", 1)
	expectStrings(t, "definition of the stage", tr.locations(g.definitions(sym)), []string{"Dockerfile:2:Build"})
	expectStrings(t, "references of the stage", tr.describe(References(g, sym, true)), []string{"Dockerfile:2:Build", "Dockerfile:9:build", "Dockerfile:13:build"})
	if findings := g.Findings(); len(findings) != 1 || findings[0].Code != lint.CodeDockerStageNameCasing {
		t.Errorf("findings = %+v, want only the casing of Build", findings)
	}
}

func TestDockerRenameStage(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{"Dockerfile": navDockerfile})
	g, sym := tr.at("Dockerfile", "build", 2)
	occs, err := g.Rename(sym, "compile")
	if err != nil {
		t.Fatal(err)
	}
	expectStrings(t, "renamed occurrences", tr.describe(occs), []string{"Dockerfile:2:Build", "Dockerfile:9:build", "Dockerfile:13:build"})
	if _, err := g.Rename(sym, "2bad"); err == nil {
		t.Error("renaming a stage to 2bad succeeded")
	}
}

func TestDockerHoverAndCompletion(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{"Dockerfile": navDockerfile})
	g := tr.group("Dockerfile")
	for _, tt := range []struct{ sub, want string }{
		{"WORKDIR", "working directory"},
		{"APP_HOME", "ENV APP_HOME=/app"},
		{"from=", "COPY"},
	} {
		h, ok := g.Hover(tr.offset("Dockerfile", tt.sub, 0))
		if !ok || !strings.Contains(h.Markdown, tt.want) {
			t.Errorf("hover on %s = %q, %v, want it to mention %q", tt.sub, h.Markdown, ok, tt.want)
		}
	}
	items := g.Complete(tr.offset("Dockerfile", "APP_HOME\n", 0))
	for _, label := range []string{"APP_HOME", "VERSION"} {
		if !slices.ContainsFunc(items, func(it Item) bool { return it.Label == label && it.Kind == model.KindVariable }) {
			t.Errorf("completion after $ lacks %s: %+v", label, items)
		}
	}
	items = g.Complete(tr.offset("Dockerfile", "build /app", 0))
	if !slices.ContainsFunc(items, func(it Item) bool { return it.Label == "build" && it.Kind == model.KindStage }) {
		t.Errorf("completion in --from= lacks the stage: %+v", items)
	}
	items = g.Complete(tr.offset("Dockerfile", "RUN go test", 0))
	if !slices.ContainsFunc(items, func(it Item) bool { return it.Label == "HEALTHCHECK" && it.Kind == model.KindKeyword }) {
		t.Errorf("completion of an instruction lacks HEALTHCHECK: %+v", items)
	}
}

func TestDockerSourceLinks(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{"Dockerfile": navDockerfile, "go.mod": "module app\n"})
	g := tr.group("Dockerfile")
	g.exists = func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Mode().IsRegular()
	}
	expectStrings(t, "source links", linkTargets(tr, g), []string{"go.mod"})
}
