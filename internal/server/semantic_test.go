package server

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

func applyTokenEdits(data []uint32, edits []lsp.SemanticTokensEdit) []uint32 {
	out := slices.Clone(data)
	for _, e := range slices.Backward(edits) {
		out = slices.Replace(out, int(e.Start), int(e.Start+e.DeleteCount), e.Data...)
	}
	return out
}

func TestSemanticTokens(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "x=1\necho \"😀$x\"\n"))
	full := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "file:///w/a.bash"}}`))
	want := []uint32{
		0, 0, 1, uint32(semantic.TypeVariable), uint32(semantic.ModModification),
		1, 0, 4, uint32(semantic.TypeFunction), uint32(semantic.ModDefaultLibrary),
		0, 5, 1, uint32(semantic.TypeString), 0,
		0, 1, 2, uint32(semantic.TypeString), 0,
		0, 3, 1, uint32(semantic.TypeVariable), 0,
		0, 1, 1, uint32(semantic.TypeString), 0,
	}
	if !slices.Equal(full.Data, want) || full.ResultID == "" {
		t.Errorf("full = %v (id %q)\nwant %v", full.Data, full.ResultID, want)
	}

	c.notify("textDocument/didChange", `{"textDocument": {"uri": "file:///w/a.bash", "version": 2}, "contentChanges": [{"text": "x=1\necho \"😀$x\"\nreadonly y=2\n"}]}`)
	delta := decodeResult[lsp.SemanticTokensDelta](t, c.call("textDocument/semanticTokens/full/delta", `{"textDocument": {"uri": "file:///w/a.bash"}, "previousResultId": "`+full.ResultID+`"}`))
	again := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "file:///w/a.bash"}}`))
	if got := applyTokenEdits(full.Data, delta.Edits); !slices.Equal(got, again.Data) || len(delta.Edits) != 1 {
		t.Errorf("delta %+v applied = %v, want %v", delta.Edits, got, again.Data)
	}

	stale := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full/delta", `{"textDocument": {"uri": "file:///w/a.bash"}, "previousResultId": "unknown"}`))
	if !slices.Equal(stale.Data, again.Data) {
		t.Errorf("unknown previousResultId should answer with full tokens, got %+v", stale)
	}

	ranged := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/range", `{"textDocument": {"uri": "file:///w/a.bash"}, "range": {"start": {"line": 2, "character": 0}, "end": {"line": 3, "character": 0}}}`))
	if len(ranged.Data) != 10 || ranged.Data[0] != 2 {
		t.Errorf("range tokens = %v", ranged.Data)
	}
}

func TestSelectionRanges(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "echo \"$HOME\" | wc\n"))
	got := decodeResult[[]lsp.SelectionRange](t, c.call("textDocument/selectionRange", `{"textDocument": {"uri": "file:///w/a.sh"}, "positions": [{"line": 0, "character": 8}]}`))
	if len(got) != 1 {
		t.Fatalf("selection ranges = %+v", got)
	}
	depth := 0
	var prev *lsp.Range
	for r := &got[0]; r != nil; r = r.Parent {
		if prev != nil && (r.Range.Start.Character > prev.Start.Character || (r.Range.End.Line == prev.End.Line && r.Range.End.Character < prev.End.Character)) {
			t.Errorf("parent %+v does not contain child %+v", r.Range, *prev)
		}
		prev = &r.Range
		depth++
	}
	if depth < 5 || got[0].Range != (lsp.Range{Start: lsp.Position{Character: 7}, End: lsp.Position{Character: 11}}) {
		t.Errorf("innermost = %+v, depth %d", got[0].Range, depth)
	}
}

type spelledToken struct {
	line uint32
	text string
}

func unitsOf(encoding, line string, start, length uint32) (string, bool) {
	end := start + length
	switch encoding {
	case "utf-8":
		if int(end) > len(line) {
			return "", false
		}
		return line[start:end], true
	case "utf-32":
		runes := []rune(line)
		if int(end) > len(runes) {
			return "", false
		}
		return string(runes[start:end]), true
	}
	units := utf16.Encode([]rune(line))
	if int(end) > len(units) {
		return "", false
	}
	return string(utf16.Decode(units[start:end])), true
}

func spellTokens(t *testing.T, src, encoding string, data []uint32) []spelledToken {
	t.Helper()

	if len(data)%5 != 0 {
		t.Fatalf("%d values do not make whole tokens", len(data))
	}
	lines := strings.Split(src, "\n")
	out := make([]spelledToken, 0, len(data)/5)
	var line, start, end uint32
	for i := 0; i < len(data); i += 5 {
		if data[i] > 0 {
			line += data[i]
			start, end = data[i+1], 0
		} else {
			start += data[i+1]
		}
		if start < end || data[i+2] == 0 {
			t.Fatalf("token %d at %d:%d length %d overlaps the previous one ending at %d or is empty", i/5, line, start, data[i+2], end)
		}
		end = start + data[i+2]
		if int(line) >= len(lines) {
			t.Fatalf("token %d is on line %d of %d", i/5, line, len(lines))
		}
		text, ok := unitsOf(encoding, lines[line], start, data[i+2])
		if !ok {
			t.Fatalf("token %d at %d:%d length %d runs past %q", i/5, line, start, data[i+2], lines[line])
		}
		spelled := text + " " + semantic.TypeNames[data[i+3]]
		var mods []string
		for bit, name := range semantic.ModifierNames {
			if data[i+4]&(1<<bit) != 0 {
				mods = append(mods, name)
			}
		}
		if len(mods) > 0 {
			spelled += " " + strings.Join(mods, ",")
		}
		out = append(out, spelledToken{line: line, text: spelled})
	}
	return out
}

func texts(tokens []spelledToken) []string {
	out := make([]string, len(tokens))
	for i, tok := range tokens {
		out[i] = tok.text
	}
	return out
}

func checkInOrder(t *testing.T, got, want []string) {
	t.Helper()

	at := 0
	for _, w := range want {
		i := slices.Index(got[at:], w)
		if i < 0 {
			t.Errorf("no %q after position %d of %q", w, at, got)
			return
		}
		at += i + 1
	}
}

const nixPackage = "{ stdenv, tool }:\nstdenv.mkDerivation {\n  name = \"a\";\n  buildPhase = ''\n    echo \"😀${tool}ü\" $out\n    ls\n  '';\n}\n"

var nixPackageTokens = []string{
	"tool parameter declaration", "stdenv parameter", "mkDerivation property", "name property declaration", "a string",
	"buildPhase property declaration", "'' string", "echo function defaultLibrary", `" string`, "😀 string", "${ operator", "tool parameter", "} operator",
	"ü string", `" string`, "ls function external", "'' string",
}

func TestNixSemanticTokens(t *testing.T) {
	t.Parallel()

	const uri = "file:///w/package.nix"
	doc := `{"textDocument": {"uri": "` + uri + `"}}`
	c, _ := startClient(t, `{"capabilities": {"general": {"positionEncodings": ["utf-16"]}, "textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", nixPackage))

	full := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", doc))
	if full.ResultID == "" {
		t.Error("full tokens have no result id")
	}
	tokens := spellTokens(t, nixPackage, "utf-16", full.Data)
	checkInOrder(t, texts(tokens), nixPackageTokens)
	if !slices.ContainsFunc(tokens, func(tok spelledToken) bool { return strings.HasPrefix(tok.text, "out variable") }) {
		t.Errorf("no shell variable among %v", tokens)
	}

	const line = 4
	ranged := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/range", `{"textDocument": {"uri": "`+uri+`"}, "range": {"start": {"line": 4, "character": 0}, "end": {"line": 5, "character": 0}}}`))
	onLine := slices.DeleteFunc(slices.Clone(tokens), func(tok spelledToken) bool { return tok.line != line })
	if got := spellTokens(t, nixPackage, "utf-16", ranged.Data); len(onLine) == 0 || !slices.Equal(got, onLine) {
		t.Errorf("range tokens = %v, want %v", got, onLine)
	}

	changed := strings.Replace(nixPackage, "    ls\n", "    ls\n    pwd\n", 1)
	c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": 2}, "contentChanges": [{"text": %q}]}`, uri, changed))
	delta := decodeResult[lsp.SemanticTokensDelta](t, c.call("textDocument/semanticTokens/full/delta", `{"textDocument": {"uri": "`+uri+`"}, "previousResultId": "`+full.ResultID+`"}`))
	again := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", doc))
	if got := applyTokenEdits(full.Data, delta.Edits); !slices.Equal(got, again.Data) || len(delta.Edits) != 1 {
		t.Errorf("delta %+v applied = %v, want %v", delta.Edits, got, again.Data)
	}
	checkInOrder(t, texts(spellTokens(t, changed, "utf-16", again.Data)), []string{"ls function external", "pwd function defaultLibrary", "'' string"})
}

func TestNixSemanticTokensInEveryEncoding(t *testing.T) {
	t.Parallel()

	const (
		uri = "file:///w/plain.nix"
		src = "# ü😀\n{ a = \"😀${1}ü\"; \"ñ\" = 2; b = ./x; }\n"
	)
	want := []string{
		"# ü😀 comment", "a property declaration", "= operator", `" string`, "😀 string", "${ operator", "1 number", "} operator", "ü string", `" string`,
		`" string`, "ñ property declaration", `" string`, "= operator", "2 number", "b property declaration", "= operator", "./x string external",
	}
	lengths := map[string]uint32{"utf-8": uint32(len("😀")), "utf-16": 2, "utf-32": 1}
	for _, encoding := range []string{"utf-8", "utf-16", "utf-32"} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()

			c, _ := startClient(t, `{"capabilities": {"general": {"positionEncodings": ["`+encoding+`"]}, "textDocument": {"diagnostic": {}}}}`)
			c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", src))
			data := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+uri+`"}}`)).Data
			if got := texts(spellTokens(t, src, encoding, data)); !slices.Equal(got, want) {
				t.Errorf("tokens\n got %q\nwant %q", got, want)
			}
			if got := data[4*5+2]; got != lengths[encoding] {
				t.Errorf("the emoji is %d units long, want %d", got, lengths[encoding])
			}

			const shellURI = "file:///w/package.nix"
			c.notify("textDocument/didOpen", openLanguageParams(shellURI, "nix", nixPackage))
			shell := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+shellURI+`"}}`)).Data
			checkInOrder(t, texts(spellTokens(t, nixPackage, encoding, shell)), nixPackageTokens)
		})
	}
}

func TestNixSemanticTokensWithSyntaxErrors(t *testing.T) {
	t.Parallel()

	const (
		uri = "file:///w/broken.nix"
		src = "{ pkgs }: {\n  buildPhase = ''\n    echo ${pkgs.hello}\n  '';\n  a = ;\n  b = (1;\n}\n"
	)
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", src))
	data := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+uri+`"}}`)).Data
	checkInOrder(t, texts(spellTokens(t, src, "utf-16", data)), []string{
		"pkgs parameter declaration", "buildPhase property declaration", "echo function defaultLibrary", "${ operator", "pkgs parameter", "hello property", "} operator",
		"a property declaration", "b property declaration", "1 number",
	})
}

func TestNixSemanticTokensOnTheCorpus(t *testing.T) {
	t.Parallel()

	const root = "../../corpus/nix"
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".nix" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		uri := "file:///w/" + filepath.ToSlash(rel)
		c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", string(raw)))
		data := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+uri+`"}}`)).Data
		tokens := spellTokens(t, string(raw), "utf-16", data)
		if len(tokens) == 0 {
			t.Errorf("%s has no tokens", path)
		}
		if rel == filepath.Join("flake-parts", "pkgs", "mirrorctl.nix") {
			checkInOrder(t, texts(tokens), []string{
				"lib parameter declaration", "mkDerivation property", "pname property declaration", "inherit keyword", "version property declaration",
				"${ operator", "lib parameter", "makeBinPath property", "syncTools parameter", "} operator", "mainProgram property declaration",
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func selectionChain(t *testing.T, p *nativeProject, name, sub string) []string {
	t.Helper()
	text := p.files[name]
	line, at := p.pos(name, sub, 0)
	ranges := decodeResult[[]lsp.SelectionRange](t, p.c.call("textDocument/selectionRange", fmt.Sprintf(`{"textDocument": {"uri": %q}, "positions": [{"line": %d, "character": %d}]}`, p.uri(name), line, at+1)))
	var out []string
	for r := &ranges[0]; r != nil; r = r.Parent {
		out = append(out, text[offsetOf(text, r.Range.Start):offsetOf(text, r.Range.End)])
	}
	return out
}

func TestSelectionInOneOfSeveralScriptsKeepsItsOwnRanges(t *testing.T) {
	t.Parallel()

	nix := "{\n  buildPhase = ''\n    echo $out\n  '';\n  installPhase = ''\n    echo $prefix\n  '';\n}\n"
	for _, tt := range []struct {
		name, text, at string
		has            []string
		without        string
	}{
		{"default.nix", nix, "$out", []string{"''\n    echo $out\n  ''", "buildPhase = ''\n    echo $out\n  '';", nix}, "installPhase"},
		{"Makefile", "a:\n\techo one\nb:\n\techo two\n", "one", []string{"a:\n\techo one"}, "two"},
		{"justfile", "a:\n    echo one\n\nb:\n    echo two\n", "one", []string{"a:\n    echo one"}, "two"},
		{"Dockerfile", "FROM a\nRUN echo one\nRUN echo two\n", "one", []string{"RUN echo one"}, "two"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := openNativeProject(t, nativeCapabilities, map[string]string{tt.name: tt.text}, tt.name)
			chain := selectionChain(t, p, tt.name, tt.at)
			for _, want := range tt.has {
				if !slices.Contains(chain, want) {
					t.Errorf("selection = %q, want it to hold %q", chain, want)
				}
			}
			if chain[len(chain)-1] != tt.text {
				t.Errorf("selection ends at %q, want the whole file", chain[len(chain)-1])
			}
			for i := 1; i < len(chain); i++ {
				if !strings.Contains(chain[i], chain[i-1]) {
					t.Errorf("selection does not nest at %d: %q", i, chain)
				}
			}
			if chain[0] != tt.at && strings.Contains(chain[0], tt.without) {
				t.Errorf("innermost selection %q reaches the other script", chain[0])
			}
		})
	}
}
