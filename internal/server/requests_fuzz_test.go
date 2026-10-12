package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/clienttext"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const (
	fuzzInitialize   = `{"capabilities": {"textDocument": {"diagnostic": {}, "completion": {"completionItem": {"snippetSupport": true, "insertReplaceSupport": true, "labelDetailsSupport": true}}, "codeAction": {"resolveSupport": {"properties": ["edit"]}}, "rename": {"prepareSupport": true}}, "workspace": {"workspaceEdit": {"documentChanges": true}}}}`
	maxFuzzDocument  = 4 << 10
	resultBudgetBase = 256 << 10
	resultBudgetRate = 512
	completionBudget = 8 << 20
	maxResolved      = 4
)

type fuzzKind struct {
	suffix, languageID string
}

var fuzzKinds = []fuzzKind{
	{".sh", "shellscript"},
	{".bash", "shellscript"},
	{".zsh", "shellscript"},
	{".ksh", "shellscript"},
	{".dash", "shellscript"},
	{".bats", "shellscript"},
	{"/Makefile", "makefile"},
	{"/justfile", "just"},
	{".nix", "nix"},
}

type fuzzDocument struct {
	t          *testing.T
	c          *testClient
	uri        string
	languageID string
	text       string
	lines      []clienttext.Line
}

func FuzzRequests(f *testing.F) {
	f.Add("#!/bin/bash\ngreet() { local who=$1; echo \"hi $who\"; }\ngreet `date` $x\n", byte(1), []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add("#!/bin/zsh\nfor f (*.txt(.N)) print -r -- ${(U)f}\ncase $x { a) ;; }\n", byte(2), []byte{9, 3, 1, 4, 1, 5, 9, 2, 6})
	f.Add("cat <<EOF\n$x\nEOF\nif [ $a = 1 ]\nthen echo \"${b:-c}\"\nfi\r\nx=$((1+2))\r", byte(0), []byte{2, 7, 1, 8, 2, 8, 1, 8})
	f.Add("@test \"adds\" {\n  run echo 1\n  [ \"$output\" = 1 ]\n}\n", byte(5), []byte{1, 1, 1, 1, 1, 1})
	f.Add("#!/bin/bash\ndeclare -A m=([e/]=0 [foo bar]=1)\nm[a.b]=2\necho \"${m[a.b]} ${!m[@]}\"\nfor k in \"${!m[@]}\"; do unset 'm[$k]'; done\n", byte(1), []byte{2, 9, 3, 4, 1, 8})
	f.Add("#!/bin/sh\necho ${.sh.file} ${(%):-%x} ${@name@}\nexport NIX_@suffix@=1\n", byte(0), []byte{1, 6, 1, 20, 2, 3})
	f.Add("SHELL := bash\nall: a ; echo `x` $$y\n\t@for f in $(F); do \\\n\t  echo $$f; \\\n\tdone\n.ONESHELL:\n", byte(6), []byte{1, 9, 2, 12, 3, 0, 4, 5})
	f.Add("x:\n\t# shellcheck disable=SC2086\n\tgreet() { echo $$1; }; greet `date`\ndefine y\n\techo\nendef\n", byte(6), []byte{2, 20, 2, 4, 1, 3})
	f.Add("set shell := [\"bash\", \"-uc\"]\nexport t := 'x'\n[private]\nrun $a b='{{x}}': dep\n    @echo `date` {{a}} '{{b}}' \\\n      $t\n\nscript:\n    #!/usr/bin/env bash\n    cat <<EOF\n    it's {{{{x}}\n    EOF\n", byte(7), []byte{3, 12, 4, 9, 8, 2, 5})
	f.Add("{ src = ~/x; installPhase = attrs.installPhase or (''\n    if true; then cp ${src} $out\n  '' + lib.optionalString x ''\n    fi '''a ''${b}\n  ''); text = writeShellApplication { name = \"x\"; text = \"echo \\\"$1\\\"\"; }; }\n", byte(8), []byte{1, 8, 2, 30, 3, 6, 4})
	seeds, err := filepath.Glob(filepath.Join("..", "parser", "testdata", "corpus", "*", "*"))
	if err != nil {
		f.Fatal(err)
	}
	for i, path := range seeds {
		if filepath.Ext(path) == ".golden" {
			continue
		}
		text, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		if len(text) <= maxFuzzDocument {
			kind := slices.IndexFunc(fuzzKinds, func(k fuzzKind) bool { return k.suffix == filepath.Ext(path) })
			f.Add(string(text), byte(kind), []byte{byte(i), byte(i * 7), byte(i * 13), byte(i * 31), 3, 1})
		}
	}
	c, _ := startClientIn(f, fuzzProcess(f), fuzzInitialize)
	opened := 0
	f.Fuzz(func(t *testing.T, text string, kind byte, script []byte) {
		if !utf8.ValidString(text) || len(text) > maxFuzzDocument || len(script) == 0 {
			return
		}
		c.t = t
		opened++
		k := fuzzKinds[int(kind)%len(fuzzKinds)]
		doc := &fuzzDocument{t: t, c: c, uri: "file:///fuzz/" + strconv.Itoa(opened) + k.suffix, languageID: k.languageID}
		doc.open(text)
		defer doc.close()
		doc.exercise(script)
	})
}

func fuzzProcess(f *testing.F) Process {
	home, work, bin, man := f.TempDir(), f.TempDir(), f.TempDir(), f.TempDir()
	for _, dir := range []string{filepath.Join(home, "notes"), filepath.Join(work, "lib")} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			f.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(work, "main.sh"), filepath.Join(bin, "tool")} {
		if err := os.WriteFile(file, []byte("#!/bin/sh\n"), 0o755); err != nil {
			f.Fatal(err)
		}
	}
	return Process{Path: bin, ManPath: man, Home: home, WorkDir: work, Environ: []string{"HOME=" + home, "PATH=" + bin}}
}

func (d *fuzzDocument) open(text string) {
	d.text = strings.TrimPrefix(text, "\ufeff")
	d.lines = clienttext.Lines(d.text)
	d.c.notify("textDocument/didOpen", openLanguageParams(d.uri, d.languageID, text))
}

func (d *fuzzDocument) close() {
	d.c.notify("textDocument/didClose", fmt.Sprintf(`{"textDocument": {"uri": %q}}`, d.uri))
	d.c.drain()
}

func (d *fuzzDocument) position(script []byte, at int) lsp.Position {
	line := int(script[at%len(script)]) % (len(d.lines) + 1)
	width := 0
	if line < len(d.lines) {
		width = int(clienttext.Width(d.text, d.lines[line], clienttext.UTF16))
	}
	return lsp.Position{Line: uint32(line), Character: uint32(int(script[(at+1)%len(script)]) % (width + 2))}
}

func (d *fuzzDocument) positionJSON(p lsp.Position) string {
	return fmt.Sprintf(`{"line": %d, "character": %d}`, p.Line, p.Character)
}

func (d *fuzzDocument) at(p lsp.Position) string {
	return fmt.Sprintf(`{"textDocument": {"uri": %q}, "position": %s`, d.uri, d.positionJSON(p))
}

func (d *fuzzDocument) span(a, b lsp.Position) string {
	if b.Line < a.Line || (b.Line == a.Line && b.Character < a.Character) {
		a, b = b, a
	}
	return fmt.Sprintf(`{"start": %s, "end": %s}`, d.positionJSON(a), d.positionJSON(b))
}

func (d *fuzzDocument) exercise(script []byte) {
	a, b := d.position(script, 0), d.position(script, 2)
	doc := fmt.Sprintf(`{"textDocument": {"uri": %q}`, d.uri)
	d.request("textDocument/diagnostic", doc+`}`)
	d.request("textDocument/completion", d.at(a)+`}`)
	d.request("textDocument/hover", d.at(a)+`}`)
	d.request("textDocument/signatureHelp", d.at(a)+`}`)
	d.request("textDocument/definition", d.at(a)+`}`)
	d.request("textDocument/documentHighlight", d.at(a)+`}`)
	d.request("textDocument/references", d.at(a)+`, "context": {"includeDeclaration": true}}`)
	d.request("textDocument/prepareRename", d.at(a)+`}`)
	d.request("textDocument/rename", d.at(a)+`, "newName": "renamed_1"}`)
	d.request("textDocument/inlayHint", doc+`, "range": `+d.span(a, b)+`}`)
	d.request("textDocument/foldingRange", doc+`}`)
	d.request("textDocument/selectionRange", doc+`, "positions": [`+d.positionJSON(a)+`, `+d.positionJSON(b)+`]}`)
	d.request("textDocument/documentSymbol", doc+`}`)
	d.request("textDocument/documentLink", doc+`}`)
	d.request("textDocument/codeLens", doc+`}`)
	d.request("textDocument/semanticTokens/range", doc+`, "range": `+d.span(a, b)+`}`)
	d.formats(doc)
	d.codeActions(doc, a, b)
	d.semanticDelta(doc, script)
}

func (d *fuzzDocument) request(method, params string) json.RawMessage {
	d.t.Helper()

	msg := d.c.call(method, params)
	if msg.Error != nil {
		if msg.Error.Code == jsonrpc.CodeInternalError {
			d.t.Fatalf("%s failed on %q: %s", method, d.text, msg.Error.Message)
		}
		return nil
	}
	budget := resultBudgetBase + resultBudgetRate*len(d.text)
	if method == "textDocument/completion" {
		budget = completionBudget
	}
	if len(msg.Result) > budget {
		d.t.Fatalf("%s answered %d bytes for a %d-byte document, over %d", method, len(msg.Result), len(d.text), budget)
	}
	var result any
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		d.t.Fatalf("%s result does not decode: %v", method, err)
	}
	d.checkRanges(method, result)
	return msg.Result
}

func (d *fuzzDocument) checkRanges(method string, node any) {
	switch n := node.(type) {
	case []any:
		for _, item := range n {
			d.checkRanges(method, item)
		}
	case map[string]any:
		if uri, ok := n["uri"].(string); ok && uri != d.uri {
			return
		}
		if start, ok := asPosition(n["start"]); ok {
			if end, ok := asPosition(n["end"]); ok {
				d.checkPosition(method, start)
				d.checkPosition(method, end)
				if end.Line < start.Line || (end.Line == start.Line && end.Character < start.Character) {
					d.t.Fatalf("%s returned a range ending before it starts: %+v-%+v in %q", method, start, end, d.text)
				}
			}
		}
		for key, value := range n {
			if key != "data" {
				d.checkRanges(method, value)
			}
		}
	}
}

func asPosition(node any) (lsp.Position, bool) {
	m, ok := node.(map[string]any)
	if !ok {
		return lsp.Position{}, false
	}
	line, lineOK := m["line"].(float64)
	character, characterOK := m["character"].(float64)
	if !lineOK || !characterOK || line < 0 || character < 0 {
		return lsp.Position{}, false
	}
	return lsp.Position{Line: uint32(line), Character: uint32(character)}, true
}

func (d *fuzzDocument) checkPosition(method string, p lsp.Position) {
	if int(p.Line) >= len(d.lines) {
		d.t.Fatalf("%s returned line %d of a %d-line document %q", method, p.Line, len(d.lines), d.text)
	}
	if width := clienttext.Width(d.text, d.lines[p.Line], clienttext.UTF16); p.Character > width {
		d.t.Fatalf("%s returned character %d on line %d, which is %d wide, in %q", method, p.Character, p.Line, width, d.text)
	}
}

func (d *fuzzDocument) offset(p lsp.Position) int {
	return clienttext.Offset(d.text, d.lines, p, clienttext.UTF16)
}

func (d *fuzzDocument) apply(method string, edits []lsp.TextEdit) string {
	type span struct {
		start, end int
		text       string
	}
	spans := make([]span, 0, len(edits))
	for _, e := range edits {
		spans = append(spans, span{d.offset(e.Range.Start), d.offset(e.Range.End), e.NewText})
	}
	slices.SortStableFunc(spans, func(x, y span) int { return x.start - y.start })
	var sb strings.Builder
	at := 0
	for _, s := range spans {
		if s.start < at {
			d.t.Fatalf("%s returned overlapping edits %+v for %q", method, edits, d.text)
		}
		sb.WriteString(d.text[at:s.start])
		sb.WriteString(s.text)
		at = s.end
	}
	sb.WriteString(d.text[at:])
	return sb.String()
}

func (d *fuzzDocument) formats(doc string) {
	options := `, "options": {"tabSize": 4, "insertSpaces": false}`
	for _, call := range []struct{ method, params string }{
		{"textDocument/formatting", doc + options + `}`},
		{"textDocument/rangeFormatting", doc + `, "range": ` + d.span(lsp.Position{}, lsp.Position{Line: uint32(len(d.lines))}) + options + `}`},
	} {
		var edits []lsp.TextEdit
		if raw := d.request(call.method, call.params); raw != nil && json.Unmarshal(raw, &edits) == nil {
			d.apply(call.method, edits)
		}
	}
}

func (d *fuzzDocument) codeActions(doc string, a, b lsp.Position) {
	raw := d.request("textDocument/codeAction", doc+`, "range": `+d.span(a, b)+`, "context": {"diagnostics": []}}`)
	var actions []lsp.CodeAction
	if raw == nil || json.Unmarshal(raw, &actions) != nil {
		return
	}
	resolved := 0
	for _, action := range actions {
		if action.Edit == nil && action.Data != nil && resolved < maxResolved {
			resolved++
			encoded, err := json.Marshal(action)
			if err != nil {
				d.t.Fatalf("encoding %q: %v", action.Title, err)
			}
			if raw := d.request("codeAction/resolve", string(encoded)); raw != nil && json.Unmarshal(raw, &action) != nil {
				d.t.Fatalf("codeAction/resolve result for %q does not decode", action.Title)
			}
		}
		if action.Edit != nil {
			d.applyWorkspaceEdit(action.Title, action.Edit)
		}
	}
}

func (d *fuzzDocument) applyWorkspaceEdit(title string, edit *lsp.WorkspaceEdit) {
	d.apply(title, edit.Changes[d.uri])
	for _, change := range edit.DocumentChanges {
		if change.TextDocument.URI == d.uri {
			d.apply(title, change.Edits)
		}
	}
}

func (d *fuzzDocument) semanticDelta(doc string, script []byte) {
	var before lsp.SemanticTokens
	if raw := d.request("textDocument/semanticTokens/full", doc+`}`); raw == nil || json.Unmarshal(raw, &before) != nil {
		return
	}
	d.checkTokens(before.Data)
	at := d.position(script, 4)
	insert := []string{"x", "\n", " $y", "\"", "(", "\r\n", "é"}[int(script[len(script)-1])%7]
	encoded, err := json.Marshal(lsp.DidChangeTextDocumentParams{
		TextDocument:   lsp.VersionedTextDocumentIdentifier{URI: d.uri, Version: 2},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{{Range: &lsp.Range{Start: at, End: at}, Text: insert}},
	})
	if err != nil {
		d.t.Fatalf("encoding the change: %v", err)
	}
	offset := d.offset(at)
	d.text = d.text[:offset] + insert + d.text[offset:]
	d.lines = clienttext.Lines(d.text)
	d.c.notify("textDocument/didChange", string(encoded))
	var delta lsp.SemanticTokensDelta
	raw := d.request("textDocument/semanticTokens/full/delta", doc+fmt.Sprintf(`, "previousResultId": %q}`, before.ResultID))
	if raw == nil {
		return
	}
	var after lsp.SemanticTokens
	if json.Unmarshal(raw, &delta) != nil || delta.Edits == nil {
		if json.Unmarshal(raw, &after) != nil {
			d.t.Fatalf("semantic token delta does not decode: %s", raw)
		}
		d.checkTokens(after.Data)
		return
	}
	data := slices.Clone(before.Data)
	for _, e := range slices.Backward(delta.Edits) {
		if int(e.Start)+int(e.DeleteCount) > len(data) {
			d.t.Fatalf("semantic token edit %+v past %d tokens", e, len(data))
		}
		data = slices.Concat(data[:e.Start], e.Data, data[e.Start+e.DeleteCount:])
	}
	if raw := d.request("textDocument/semanticTokens/full", doc+`}`); raw == nil || json.Unmarshal(raw, &after) != nil {
		d.t.Fatalf("no full semantic tokens after the delta")
	}
	if !slices.Equal(data, after.Data) {
		d.t.Fatalf("semantic token delta gives %v, full gives %v for %q", data, after.Data, d.text)
	}
	d.checkTokens(after.Data)
}

func (d *fuzzDocument) checkTokens(data []uint32) {
	if len(data)%5 != 0 {
		d.t.Fatalf("semantic tokens hold %d integers, not a multiple of 5", len(data))
	}
	var line, start uint32
	for i := 0; i < len(data); i += 5 {
		if data[i] > 0 {
			line += data[i]
			start = data[i+1]
		} else {
			start += data[i+1]
		}
		d.checkPosition("semantic tokens", lsp.Position{Line: line, Character: start + data[i+2]})
	}
}
