package server

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

type projectedHover struct {
	at       string
	contains string
	shown    bool
}

type projectedToken struct {
	at, kind string
}

type projectedShellCase struct {
	name, uri, languageID string
	lines                 []string
	hovers                []projectedHover
	extglobNeeded         []string
	tokens                []projectedToken
}

var projectedShellCases = []projectedShellCase{
	{
		name: "justfile shebang recipes", uri: "file:///w/justfile", languageID: "just",
		lines: []string{
			`set shell := ["bash", "-uc"]`,
			`a:`,
			`    #!/usr/bin/env bash`,
			`    shopt -s nullglob extglob`,
			`    va=a.b`,
			`    echo "${va%.*}" *.c @(a|b)`,
			`    [[ $va =~ ^[a-z]+\.b$ ]] && echo`,
			`b:`,
			`    #!/usr/bin/env bash`,
			`    echo "${va%.?}" *.h @(c|d)`,
		},
		hovers: []projectedHover{
			{"%.*", "**Result**: `a`", true},
			{"%.?", "**Result**", false},
			{"*.c", "`nullglob` is set", true},
			{"*.h", "`nullglob` is set", false},
			{"[a-z]", "**Result**: `a.b` matches", true},
		},
		extglobNeeded: []string{"@(c|d)"},
		tokens:        []projectedToken{{"[a-z]", "regexp"}, {"$ ]]", "operator"}, {"@(a|b)", "operator"}},
	},
	{
		name: "justfile line recipes", uri: "file:///w/justfile", languageID: "just",
		lines: []string{
			`set shell := ["bash", "-uc"]`,
			`a:`,
			`    shopt -s extglob`,
			`    echo @(c|d)`,
			`    va=a.b; echo "${va%.*}"`,
			`    echo "${va%.?}"`,
			`    shopt -s nullglob; echo *.c`,
			`    echo *.h`,
			`    [[ a.b =~ ^[a-z]+\.b$ ]] && echo`,
		},
		hovers: []projectedHover{
			{"%.*", "**Result**: `a`", true},
			{"%.?", "**Result**", false},
			{"*.c", "`nullglob` is set", true},
			{"*.h", "`nullglob` is set", false},
		},
		extglobNeeded: []string{"@(c|d)"},
		tokens:        []projectedToken{{"[a-z]", "regexp"}, {"$ ]]", "operator"}},
	},
	{
		name: "Makefile line recipes", uri: "file:///w/Makefile", languageID: "makefile",
		lines: []string{
			`SHELL := /bin/bash`,
			`a:`,
			"\tshopt -s extglob",
			"\techo @(c|d)",
			"\tva=a.b; echo \"$${va%.*}\"",
			"\techo \"$${va%.?}\"",
			"\tshopt -s nullglob; echo *.c",
			"\techo *.h",
			"\t[[ a.b =~ ^[a-z]+\\.b$$ ]] && echo",
		},
		hovers: []projectedHover{
			{"%.*", "**Result**: `a`", true},
			{"%.?", "**Result**", false},
			{"*.c", "`nullglob` is set", true},
			{"*.h", "`nullglob` is set", false},
		},
		extglobNeeded: []string{"@(c|d)"},
		tokens:        []projectedToken{{"[a-z]", "regexp"}, {"$$ ]]", "operator"}},
	},
	{
		name: "Makefile one-shell recipes", uri: "file:///w/Makefile", languageID: "makefile",
		lines: []string{
			`SHELL := /bin/bash`,
			`.ONESHELL:`,
			`a:`,
			"\tshopt -s nullglob extglob",
			"\tva=a.b",
			"\techo \"$${va%.*}\" *.c @(a|b)",
			"\t[[ $$va =~ ^[a-z]+\\.b$$ ]] && echo",
			`b:`,
			"\techo \"$${va%.?}\" *.h @(c|d)",
		},
		hovers: []projectedHover{
			{"%.*", "**Result**: `a`", true},
			{"%.?", "**Result**", false},
			{"*.c", "`nullglob` is set", true},
			{"*.h", "`nullglob` is set", false},
			{"[a-z]", "**Result**: `a.b` matches", true},
		},
		extglobNeeded: []string{"@(c|d)"},
		tokens:        []projectedToken{{"[a-z]", "regexp"}, {"$$ ]]", "operator"}, {"@(a|b)", "operator"}},
	},
	{
		name: "Nix scripts", uri: "file:///w/default.nix", languageID: "nix",
		lines: []string{
			`{ pkgs }: {`,
			`  a = pkgs.writeShellScript "a" ''`,
			`    shopt -s nullglob extglob`,
			`    va=a.b`,
			`    echo "''${va%.*}" *.c @(a|b)`,
			`    [[ $va =~ ^[a-z]+\.b$ ]] && echo`,
			`  '';`,
			`  b = pkgs.writeShellScript "b" ''`,
			`    echo "''${va%.?}" *.h @(c|d)`,
			`  '';`,
			`}`,
		},
		hovers: []projectedHover{
			{"%.*", "**Result**: `a`", true},
			{"%.?", "**Result**", false},
			{"*.c", "`nullglob` is set", true},
			{"*.h", "`nullglob` is set", false},
			{"[a-z]", "**Result**: `a.b` matches", true},
		},
		extglobNeeded: []string{"@(c|d)"},
		tokens:        []projectedToken{{"[a-z]", "regexp"}, {"$ ]]", "operator"}, {"@(a|b)", "operator"}},
	},
	{
		name: "Nix derivation phases", uri: "file:///w/package.nix", languageID: "nix",
		lines: []string{
			`stdenv.mkDerivation {`,
			`  buildPhase = ''`,
			`    shopt -s nullglob extglob`,
			`  '';`,
			`  installPhase = ''`,
			`    va=a.b`,
			`    echo "''${va%.*}" *.c @(a|b)`,
			`    [[ $va =~ ^[a-z]+\.b$ ]] && echo`,
			`  '';`,
			`}`,
		},
		hovers: []projectedHover{
			{"%.*", "**Result**: `a`", true},
			{"*.c", "`nullglob` is set", true},
			{"[a-z]", "**Result**: `a.b` matches", true},
		},
		tokens: []projectedToken{{"[a-z]", "regexp"}, {"$ ]]", "operator"}, {"@(a|b)", "operator"}},
	},
}

func (pc projectedShellCase) position(t *testing.T, sub string) lsp.Position {
	t.Helper()

	var found []lsp.Position
	for line, text := range pc.lines {
		if i := strings.Index(text, sub); i >= 0 {
			found = append(found, lsp.Position{Line: uint32(line), Character: uint32(i)})
		}
	}
	if len(found) != 1 {
		t.Fatalf("%q is on %d lines of the document, want one", sub, len(found))
	}
	return found[0]
}

func openProjectedShells(t *testing.T, pc projectedShellCase) *testClient {
	t.Helper()

	c, _ := startClient(t, `{`+pullCapabilities+`}`)
	c.notify("textDocument/didOpen", openLanguageParams(pc.uri, pc.languageID, strings.Join(pc.lines, "\n")+"\n"))
	return c
}

func TestProjectedShellsKeepAssignmentsAndOptionsToThemselves(t *testing.T) {
	t.Parallel()

	for _, pc := range projectedShellCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			c := openProjectedShells(t, pc)
			for _, h := range pc.hovers {
				at := pc.position(t, h.at)
				got := decodeResult[lsp.Hover](t, c.call("textDocument/hover", positionParams(pc.uri, int(at.Line), int(at.Character), ""))).Contents.Value
				if strings.Contains(got, h.contains) != h.shown {
					t.Errorf("hover on %q shows %q = %v, want %v:\n%s", h.at, h.contains, !h.shown, h.shown, got)
				}
			}
		})
	}
}

func TestPhasesOfADerivationAreOneScriptForNavigation(t *testing.T) {
	t.Parallel()

	pc := projectedShellCase{
		uri: "file:///w/phases.nix", languageID: "nix",
		lines: []string{
			`stdenv.mkDerivation {`,
			`  configurePhase = ''`,
			`    carried=1`,
			`  '';`,
			`  buildPhase = ''`,
			`    echo "$carried"`,
			`    echo "$car"`,
			`  '';`,
			`}`,
		},
	}
	c := openProjectedShells(t, pc)
	assignment, read := pc.position(t, "carried=1"), pc.position(t, `"$carried"`)
	read.Character += uint32(len(`"$`))
	typed := pc.position(t, `"$car"`)
	typed.Character += uint32(len(`"$car`))
	textDocument := fmt.Sprintf(`"textDocument":{"uri":%q}`, pc.uri)
	at := func(p lsp.Position) string {
		return fmt.Sprintf(`%s,"position":{"line":%d,"character":%d}`, textDocument, p.Line, p.Character)
	}

	definition := decodeResult[[]lsp.Location](t, c.call("textDocument/definition", "{"+at(read)+"}"))
	if len(definition) != 1 || definition[0].Range.Start.Line != assignment.Line {
		t.Errorf("definition of the read = %+v, want the assignment on line %d", definition, assignment.Line)
	}
	for name, from := range map[string]lsp.Position{"assignment": assignment, "read": read} {
		refs := decodeResult[[]lsp.Location](t, c.call("textDocument/references", "{"+at(from)+`,"context":{"includeDeclaration":true}}`))
		lines := make([]uint32, 0, len(refs))
		for _, ref := range refs {
			lines = append(lines, ref.Range.Start.Line)
		}
		slices.Sort(lines)
		if !slices.Equal(lines, []uint32{assignment.Line, read.Line}) {
			t.Errorf("references from the %s are on lines %v, want %d and %d", name, lines, assignment.Line, read.Line)
		}
		edit := decodeResult[lsp.WorkspaceEdit](t, c.call("textDocument/rename", "{"+at(from)+`,"newName":"moved"}`))
		if edits := edit.Changes[pc.uri]; len(edits) != 2 {
			t.Errorf("rename from the %s edits %+v, want the assignment and the read", name, edit)
		}
	}
	hover := decodeResult[lsp.Hover](t, c.call("textDocument/hover", "{"+at(read)+"}"))
	if !strings.Contains(hover.Contents.Value, "carried=1") {
		t.Errorf("hover on the read = %q, want the assignment", hover.Contents.Value)
	}
	if labels := completionLabels(decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", "{"+at(typed)+"}"))); !slices.Contains(labels, "carried") {
		t.Errorf("completion in the later phase = %v, want carried", labels)
	}
}

func TestProjectedShellsNeedExtglobOnlyWhereTheirShellLacksIt(t *testing.T) {
	t.Parallel()

	for _, pc := range projectedShellCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			c := openProjectedShells(t, pc)
			var got []lsp.Range
			for _, d := range pullDiagnostics(t, c, pc.uri) {
				if d.Code == "SL2016" {
					got = append(got, d.Range)
				}
			}
			want := make([]lsp.Range, 0, len(pc.extglobNeeded))
			for _, sub := range pc.extglobNeeded {
				start := pc.position(t, sub)
				want = append(want, lsp.Range{Start: start, End: lsp.Position{Line: start.Line, Character: start.Character + uint32(len(sub))}})
			}
			if !slices.Equal(got, want) {
				t.Errorf("SL2016 at %+v, want at %+v", got, want)
			}
		})
	}
}

func TestProjectedPatternTokensCoverTheirHostText(t *testing.T) {
	t.Parallel()

	for _, pc := range projectedShellCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			c := openProjectedShells(t, pc)
			data := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+pc.uri+`"}}`)).Data
			kinds := map[lsp.Position]string{}
			var line, start uint32
			for i := 0; i+4 < len(data); i += 5 {
				if data[i] > 0 {
					line += data[i]
					start = data[i+1]
				} else {
					start += data[i+1]
				}
				if end := start + data[i+2]; int(line) >= len(pc.lines) || int(end) > len(pc.lines[line]) {
					t.Errorf("token %d at %d:%d-%d runs past the host text", i/5, line, start, end)
					continue
				}
				kinds[lsp.Position{Line: line, Character: start}] = semantic.TypeNames[data[i+3]]
			}
			for _, tok := range pc.tokens {
				if got := kinds[pc.position(t, tok.at)]; got != tok.kind {
					t.Errorf("token at %q is %q, want %q", tok.at, got, tok.kind)
				}
			}
		})
	}
}
