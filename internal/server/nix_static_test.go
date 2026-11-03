package server

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

var checkoutFiles = map[string]string{
	"flake.nix":                         "{ outputs = { self }: { }; }\n",
	"lib/default.nix":                   "let\n  lib = makeExtensible (self: {\n    licenses = import ./licenses.nix;\n    id = x: x;\n  });\nin\nlib\n",
	"lib/licenses.nix":                  "mapAttrs mk { mit = { }; asl20 = { }; }\n",
	"pkgs/top-level/all-packages.nix":   "{ lib }:\nres: pkgs: super:\nwith pkgs;\n{\n  zlib = callPackage ../tools/zlib { };\n}\n",
	"pkgs/tools/zlib/default.nix":       "{ stdenv }:\nstdenv.mkDerivation { meta.description = \"Compresses.\"; }\n",
	"pkgs/by-name/he/hello/package.nix": "{ stdenv }:\nstdenv.mkDerivation { meta.description = \"Says hello.\"; }\n",
	"pkgs/by-name/us/user/package.nix":  "{ lib, pkgs }:\n{\n  a = lib.id;\n  b = pkgs.hello;\n  c = pkgs.zlib;\n  d = with lib.licenses; [ mi ];\n  e = lib.licenses.as;\n  f = pkgs.absent;\n  g = { inherit (lib) id; };\n  h = { inherit (lib.licenses) mi; };\n  i = pkgs.writeShellScript \"i\" ''\n    ${pkgs.hello}/bin/hello\n    ${pkgs.absent}/bin/absent\n  '';\n}\n",
}

const checkoutUser = "pkgs/by-name/us/user/package.nix"

func checkoutDefinition(t *testing.T, p *nativeProject, sub string) []string {
	t.Helper()
	locations := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(checkoutUser, sub, 0, "")))
	out := make([]string, len(locations))
	for i, l := range locations {
		out[i] = l.URI
	}
	return out
}

func TestANixpkgsCheckoutAnswersFromItsOwnSources(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, checkoutFiles, checkoutUser)
	for _, tt := range []struct{ sub, file string }{
		{"id;", "lib/default.nix"},
		{"hello;", "pkgs/by-name/he/hello/package.nix"},
		{"zlib;", "pkgs/tools/zlib/default.nix"},
	} {
		got := checkoutDefinition(t, p, tt.sub)
		if !slices.Equal(got, []string{p.uri(filepath.FromSlash(tt.file))}) {
			t.Errorf("definition of %s = %q", tt.sub, got)
		}
	}
	if got := checkoutDefinition(t, p, "absent;"); len(got) != 0 {
		t.Errorf("definition of a package that is not there = %q", got)
	}
	want := []string{p.uri(filepath.FromSlash("pkgs/by-name/he/hello/package.nix"))}
	if got := checkoutDefinition(t, p, "hello}/bin"); !slices.Equal(got, want) {
		t.Errorf("definition of the package in the antiquotation = %q, want %q", got, want)
	}
	if got := checkoutDefinition(t, p, "bin/hello"); !slices.Equal(got, want) {
		t.Errorf("definition of the program next to the antiquotation = %q, want %q", got, want)
	}
	if got := checkoutDefinition(t, p, "bin/absent"); len(got) != 0 {
		t.Errorf("definition of a program of a package that is not there = %q", got)
	}
	hover := decodeResult[lsp.Hover](t, p.c.call("textDocument/hover", p.params(checkoutUser, "hello;", 0, "")))
	if !strings.Contains(hover.Contents.Value, "Says hello.") {
		t.Errorf("hover = %q", hover.Contents.Value)
	}
	for _, tt := range []struct{ sub, want string }{
		{"mi ]", "mit"},
		{"as;", "asl20"},
		{"id; }", "id"},
		{"mi; }", "mit"},
	} {
		line, character := p.pos(checkoutUser, tt.sub, 0)
		list := decodeResult[lsp.CompletionList](t, p.c.call("textDocument/completion", positionParams(p.uri(checkoutUser), line, character+2, "")))
		if !slices.ContainsFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == tt.want }) {
			t.Errorf("completion at %s lacks %s", tt.sub, tt.want)
		}
	}
}

func TestAProjectThatIsNotNixpkgsHasNoStaticPackages(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, map[string]string{"flake.nix": "{ outputs = { self }: { }; }\n", "default.nix": "{ pkgs }: pkgs.hello\n"}, "default.nix")
	if got := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params("default.nix", "hello", 0, ""))); len(got) != 0 {
		t.Errorf("definition = %+v", got)
	}
}

func TestTypeDefinitionOfAnOptionGoesToItsType(t *testing.T) {
	t.Parallel()

	src := "{ lib, config, ... }:\n{\n  options.foo = lib.mkOption { type = lib.types.port; };\n  x = config.foo;\n  y = lib;\n}\n"
	p := openNativeProject(t, nativeCapabilities, map[string]string{"default.nix": src}, "default.nix")
	got := decodeResult[[]lsp.Location](t, p.c.call("textDocument/typeDefinition", p.params("default.nix", "foo = lib", 0, "")))
	line, character := p.pos("default.nix", "lib.types", 0)
	if len(got) != 1 || int(got[0].Range.Start.Line) != line || int(got[0].Range.Start.Character) != character {
		t.Errorf("type definition = %+v, want line %d character %d", got, line, character)
	}
	if got := decodeResult[[]lsp.Location](t, p.c.call("textDocument/typeDefinition", p.params("default.nix", "lib;", 0, ""))); len(got) != 0 {
		t.Errorf("type definition of a plain name = %+v", got)
	}
}

func TestExtractToAFileNeedsAClientThatCreatesFiles(t *testing.T) {
	t.Parallel()

	src := "{ x }:\n{ cfg = { a = x; }; }\n"
	withCreate := `"capabilities": {"workspace":{"workspaceEdit":{"documentChanges":true,"resourceOperations":["create"]}},"textDocument":{"codeAction":{"codeActionLiteralSupport":{"codeActionKind":{"valueSet":["refactor"]}}}}}`
	type offer struct {
		Title string             `json:"title"`
		Edit  *lsp.WorkspaceEdit `json:"edit"`
	}
	titles := func(capabilities string, extra map[string]string) (map[string]offer, *nativeProject) {
		files := map[string]string{"default.nix": src}
		maps.Copy(files, extra)
		p := openNativeProject(t, capabilities, files, "default.nix")
		line, character := p.pos("default.nix", "cfg", 0)
		params, err := json.Marshal(map[string]any{
			"textDocument": map[string]any{"uri": p.uri("default.nix")},
			"range":        map[string]any{"start": map[string]any{"line": line, "character": character}, "end": map[string]any{"line": line, "character": character}},
			"context":      map[string]any{"diagnostics": []any{}},
		})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]offer{}
		for _, a := range decodeResult[[]offer](t, p.c.call("textDocument/codeAction", string(params))) {
			out[a.Title] = a
		}
		return out, p
	}
	const title = "Extract to `cfg.nix`"
	actions, p := titles(withCreate, nil)
	action, offered := actions[title]
	if !offered {
		t.Fatalf("not offered: %v", actions)
	}
	if action.Edit == nil || len(action.Edit.CreateFiles) != 1 || action.Edit.CreateFiles[0].URI != p.uri("cfg.nix") {
		t.Errorf("edit = %+v", action.Edit)
	}
	if actions, _ := titles(nativeCapabilities, nil); len(actions) > 0 {
		if _, offered := actions[title]; offered {
			t.Error("offered to a client that cannot create files")
		}
	}
	for name, capabilities := range map[string]string{
		"documentChanges without create": `"capabilities": {"workspace":{"workspaceEdit":{"documentChanges":true}},"textDocument":{"codeAction":{"codeActionLiteralSupport":{"codeActionKind":{"valueSet":["refactor"]}}}}}`,
		"rename and delete only":         `"capabilities": {"workspace":{"workspaceEdit":{"documentChanges":true,"resourceOperations":["rename","delete"]}},"textDocument":{"codeAction":{"codeActionLiteralSupport":{"codeActionKind":{"valueSet":["refactor"]}}}}}`,
		"create without documentChanges": `"capabilities": {"workspace":{"workspaceEdit":{"resourceOperations":["create"]}},"textDocument":{"codeAction":{"codeActionLiteralSupport":{"codeActionKind":{"valueSet":["refactor"]}}}}}`,
	} {
		actions, _ := titles(capabilities, nil)
		if len(actions) == 0 {
			t.Errorf("%s: no action at all, so the test proves nothing", name)
		}
		if _, offered := actions[title]; offered {
			t.Errorf("offered to a client with %s", name)
		}
	}
	if actions, _ := titles(withCreate, map[string]string{"cfg.nix": "{ }\n"}); len(actions) > 0 {
		if _, offered := actions[title]; offered {
			t.Error("offered over a file that exists")
		}
	}
}
