package assist

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const script = `#!/bin/bash
# Copies files.
# $1 - what to copy
copy() {
	local src="$1" dest="$2"
	cp "$src" "$dest" "$@"
}
declare -A seen
readonly LIMIT=3
dir=$(dirname "$0")
source "$dir/lib.sh"
# docs: https://example.com/guide.
copy "$dest" README.md extra
`

func setup(t *testing.T) (*workspace.File, navigation.Env, FunctionLookup) {
	t.Helper()

	root := t.TempDir()
	for name, text := range map[string]string{"main.sh": script, "lib.sh": "helper() { :; }\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.DetectDialect)
	file, err := ws.Load(filepath.Join(root, "main.sh"))
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(name string) (*parser.Tree, parser.Function, bool) {
		fn, ok := file.Tree.FunctionNamed(name)
		return file.Tree, fn, ok
	}
	return file, navigation.Env{Loader: ws, Roots: []string{root}}, lookup
}

func TestSignatureHelp(t *testing.T) {
	t.Parallel()

	file, _, lookup := setup(t)
	text := file.Tree.Text()
	call := strings.LastIndex(text, "copy ")
	tests := []struct {
		offset int
		active int
	}{
		{offset: call + len("copy "), active: 0},
		{offset: call + len("copy \"$dest\" "), active: 1},
		{offset: call + len("copy \"$dest\" README.md extra"), active: 2},
	}
	for _, tc := range tests {
		sig, ok := SignatureHelp(file.Tree, tc.offset, lookup)
		if !ok || sig.Label != "copy src dest [args...]" || sig.Doc != "Copies files." {
			t.Fatalf("signature = %+v, %t", sig, ok)
		}
		if sig.Active != tc.active {
			t.Errorf("offset %d: active = %d, want %d", tc.offset, sig.Active, tc.active)
		}
		if p := sig.Params[0]; sig.Label[p.Start:p.End] != "src" || p.Doc != "what to copy" {
			t.Errorf("first param = %+v", p)
		}
		if p := sig.Params[2]; sig.Label[p.Start:p.End] != "[args...]" {
			t.Errorf("variadic param = %+v", p)
		}
	}
	builtin, ok := SignatureHelp(parser.Parse("read -r ", dialect.Bash), 8, lookup)
	if !ok || !strings.HasPrefix(builtin.Label, "read [-r]") || builtin.Active != -1 {
		t.Errorf("builtin signature = %+v", builtin)
	}
	if _, ok := SignatureHelp(file.Tree, strings.Index(text, "copy() {"), lookup); ok {
		t.Error("signature help inside a function definition header")
	}
}

func TestInlayHints(t *testing.T) {
	t.Parallel()

	file, env, lookup := setup(t)
	text := file.Tree.Text()
	var got []string
	for _, h := range InlayHints(file, parser.Span{Start: 0, End: len(text)}, lookup, env) {
		line := strings.Count(text[:h.Offset], "\n") + 1
		col := h.Offset - strings.LastIndexByte(text[:h.Offset], '\n')
		got = append(got, fmt.Sprintf("%d:%d %s", line, col, h.Label))
	}
	want := []string{
		"8:16 : associative array",
		"9:15 : readonly",
		"11:21 → lib.sh",
		"13:6 src:",
		"13:14 dest:",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("hints:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	none := InlayHints(file, parser.Span{Start: 0, End: 10}, lookup, env)
	if len(none) != 0 {
		t.Errorf("hints outside range = %+v", none)
	}
}

func TestDocumentLinks(t *testing.T) {
	t.Parallel()

	file, env, _ := setup(t)
	text := file.Tree.Text()
	links := DocumentLinks(file, env)
	if len(links) != 2 {
		t.Fatalf("links = %+v", links)
	}
	if text[links[0].Span.Start:links[0].Span.End] != `"$dir/lib.sh"` || !strings.HasSuffix(links[0].Target, "/lib.sh") {
		t.Errorf("source link = %+v", links[0])
	}
	if links[1].Target != "https://example.com/guide" || text[links[1].Span.Start:links[1].Span.End] != "https://example.com/guide" {
		t.Errorf("url link = %+v", links[1])
	}
}
