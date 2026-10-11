package server

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func applyLSPEdits(text string, edits []lsp.TextEdit) string {
	doc := docstore.NewDocument("file:///scratch", "shellscript", 1, text)
	out := make([]textedit.Edit, len(edits))
	for i, e := range edits {
		out[i] = textedit.Edit{
			Start:   doc.PositionToOffset(e.Range.Start, docstore.EncodingUTF16),
			End:     doc.PositionToOffset(e.Range.End, docstore.EncodingUTF16),
			NewText: e.NewText,
		}
	}
	return textedit.Apply(text, out)
}

func rangeParams(uri string, line, character int, only string) string {
	at := `{"line": ` + strconv.Itoa(line) + `, "character": ` + strconv.Itoa(character) + `}`
	return `{"textDocument": {"uri": "` + uri + `"}, "range": {"start": ` + at + `, "end": ` + at + `}, "context": {"diagnostics": [], "only": ["` + only + `"]}}`
}

func resolveAction(t *testing.T, c *testClient, action lsp.CodeAction) *lsp.WorkspaceEdit {
	t.Helper()

	if action.Edit != nil || len(action.Data) == 0 {
		t.Fatalf("%q was not deferred to resolve: %+v", action.Title, action)
	}
	raw, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CodeAction](t, c.call("codeAction/resolve", string(raw)))
	if resolved.Edit == nil {
		t.Fatalf("%q resolved without an edit", action.Title)
	}
	return resolved.Edit
}

func TestRefactoringsOfferedAndResolved(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lib := "helper() { :; }\n"
	writeFiles(t, root, map[string]string{"lib.sh": lib, "reader.sh": "greet() { echo \"$name\"; }\n"})
	mainURI := fileuri.FromPath(filepath.Join(root, "main.bash"))
	libURI := fileuri.FromPath(filepath.Join(root, "lib.sh"))
	main := "#!/bin/bash\n. ./lib.sh\n\ngreet() {\n\tnow=`date`\n\tif [ \"$now\" = a ]; then\n\t\techo a\n\telif [ \"$now\" = b ]; then\n\t\techo b\n\tfi\n}\n\nfunction bye {\n\tname=world\n\techo \"bye $name\"\n}\n"
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", `+actionCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams(mainURI, main))

	resolve := func(action lsp.CodeAction) *lsp.WorkspaceEdit {
		t.Helper()
		return resolveAction(t, c, action)
	}
	single := func(title string, edit *lsp.WorkspaceEdit) string {
		t.Helper()
		if len(edit.DocumentChanges) != 1 || edit.DocumentChanges[0].TextDocument.URI != mainURI {
			t.Fatalf("%q edit = %+v", title, edit)
		}
		return applyLSPEdits(main, edit.DocumentChanges[0].Edits)
	}

	for _, tc := range []struct {
		line, character int
		only, title     string
		want            string
	}{
		{4, 7, "refactor.rewrite", "Convert backticks to $(...)", "#!/bin/bash\n. ./lib.sh\n\ngreet() {\n\tnow=$(date)\n\tif [ \"$now\" = a ]; then\n\t\techo a\n\telif [ \"$now\" = b ]; then\n\t\techo b\n\tfi\n}\n\nfunction bye {\n\tname=world\n\techo \"bye $name\"\n}\n"},
		{5, 5, "refactor.rewrite", "Convert to [[ ]]", "#!/bin/bash\n. ./lib.sh\n\ngreet() {\n\tnow=`date`\n\tif [[ \"$now\" = a ]]; then\n\t\techo a\n\telif [ \"$now\" = b ]; then\n\t\techo b\n\tfi\n}\n\nfunction bye {\n\tname=world\n\techo \"bye $name\"\n}\n"},
		{5, 5, "refactor.rewrite", "Convert to case", "#!/bin/bash\n. ./lib.sh\n\ngreet() {\n\tnow=`date`\n\tcase \"$now\" in\n\ta)\n\t\techo a\n\t\t;;\n\tb)\n\t\techo b\n\t\t;;\n\tesac\n}\n\nfunction bye {\n\tname=world\n\techo \"bye $name\"\n}\n"},
		{12, 10, "refactor.rewrite", "Declare bye() without the function keyword", "#!/bin/bash\n. ./lib.sh\n\ngreet() {\n\tnow=`date`\n\tif [ \"$now\" = a ]; then\n\t\techo a\n\telif [ \"$now\" = b ]; then\n\t\techo b\n\tfi\n}\n\nbye() {\n\tname=world\n\techo \"bye $name\"\n}\n"},
		{0, 0, "source", "Add set -euo pipefail", "#!/bin/bash\nset -euo pipefail\n\n. ./lib.sh\n\ngreet() {\n\tnow=`date`\n\tif [ \"$now\" = a ]; then\n\t\techo a\n\telif [ \"$now\" = b ]; then\n\t\techo b\n\tfi\n}\n\nfunction bye {\n\tname=world\n\techo \"bye $name\"\n}\n"},
	} {
		action, ok := titled(codeActions(t, c, rangeParams(mainURI, tc.line, tc.character, tc.only)), tc.title)
		if !ok {
			t.Errorf("%q not offered at %d:%d", tc.title, tc.line, tc.character)
			continue
		}
		if got := single(tc.title, resolve(action)); got != tc.want {
			t.Errorf("%q:\n got %q\nwant %q", tc.title, got, tc.want)
		}
	}

	sourcing, ok := titled(codeActions(t, c, rangeParams(mainURI, 13, 2, "refactor.inline")), "Inline variable name")
	if !ok {
		t.Fatal("inline not offered although the sourced lib.sh never mentions the variable")
	}
	if got, want := single("Inline variable name", resolve(sourcing)), "#!/bin/bash\n. ./lib.sh\n\ngreet() {\n\tnow=`date`\n\tif [ \"$now\" = a ]; then\n\t\techo a\n\telif [ \"$now\" = b ]; then\n\t\techo b\n\tfi\n}\n\nfunction bye {\n\techo \"bye world\"\n}\n"; got != want {
		t.Errorf("inline next to a source:\n got %q\nwant %q", got, want)
	}
	for file, src := range map[string]string{
		"reads.bash":   ". ./reader.sh\nname=world\ngreet\necho \"$name\"\n",
		"dynamic.bash": ". \"$PLUGIN\"\nname=world\necho \"$name\"\n",
	} {
		uri := fileuri.FromPath(filepath.Join(root, file))
		c.notify("textDocument/didOpen", openParams(uri, src))
		if _, ok := titled(codeActions(t, c, rangeParams(uri, 1, 1, "refactor.inline")), "Inline variable name"); ok {
			t.Errorf("inline offered in %s, whose sourced code could read the variable", file)
		}
	}
	otherURI := fileuri.FromPath(filepath.Join(root, "other.bash"))
	other := "name=world\necho \"bye $name\"\n"
	c.notify("textDocument/didOpen", openParams(otherURI, other))
	inline, ok := titled(codeActions(t, c, rangeParams(otherURI, 0, 1, "refactor.inline")), "Inline variable name")
	if !ok || inline.Kind != "refactor.inline" {
		t.Fatalf("inline action = %+v (%t)", inline, ok)
	}
	if edit := resolve(inline); len(edit.DocumentChanges) != 1 || applyLSPEdits(other, edit.DocumentChanges[0].Edits) != "echo \"bye world\"\n" {
		t.Errorf("inline edit = %+v", edit)
	}

	move, ok := titled(codeActions(t, c, rangeParams(mainURI, 3, 1, "refactor.move")), "Move greet to lib.sh")
	if !ok || move.Kind != "refactor.move" {
		t.Fatalf("move action = %+v (%t)", move, ok)
	}
	edit := resolve(move)
	if len(edit.DocumentChanges) != 2 || edit.DocumentChanges[1].TextDocument.URI != libURI || edit.DocumentChanges[1].TextDocument.Version != nil {
		t.Fatalf("move edit = %+v", edit)
	}
	if got, want := applyLSPEdits(main, edit.DocumentChanges[0].Edits), "#!/bin/bash\n. ./lib.sh\n\nfunction bye {\n\tname=world\n\techo \"bye $name\"\n}\n"; got != want {
		t.Errorf("move left main %q, want %q", got, want)
	}
	if got, want := applyLSPEdits(lib, edit.DocumentChanges[1].Edits), "helper() { :; }\n\ngreet() {\n\tnow=`date`\n\tif [ \"$now\" = a ]; then\n\t\techo a\n\telif [ \"$now\" = b ]; then\n\t\techo b\n\tfi\n}\n"; got != want {
		t.Errorf("move made lib %q, want %q", got, want)
	}
}

func TestMoveFunctionEagerlyIntoOpenFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFiles(t, root, map[string]string{"lib.sh": "on disk\n"})
	mainURI := fileuri.FromPath(filepath.Join(root, "main.sh"))
	libURI := fileuri.FromPath(filepath.Join(root, "lib.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {}}}, "workspace": {"workspaceEdit": {"documentChanges": true}}}}`)
	c.notify("textDocument/didOpen", openParams(libURI, "helper() { :; }\n"))
	c.notify("textDocument/didOpen", openParams(mainURI, ". ./lib.sh\ng() { :; }\n"))
	move, ok := titled(codeActions(t, c, rangeParams(mainURI, 1, 0, "refactor.move")), "Move g to lib.sh")
	if !ok || move.Edit == nil || len(move.Edit.DocumentChanges) != 2 {
		t.Fatalf("eager move = %+v (%t)", move, ok)
	}
	lib := move.Edit.DocumentChanges[1]
	if lib.TextDocument.URI != libURI || lib.TextDocument.Version == nil || *lib.TextDocument.Version != 1 {
		t.Errorf("open target edit = %+v", lib.TextDocument)
	}
	if got := applyLSPEdits("helper() { :; }\n", lib.Edits); got != "helper() { :; }\n\ng() { :; }\n" {
		t.Errorf("move into the open buffer made %q", got)
	}
}

func TestIfToCaseFollowsTheProjectStyle(t *testing.T) {
	t.Parallel()

	src := "if [ \"$1\" = a ]; then up; elif [ \"$1\" = b ]; then down; fi\n"
	for _, tc := range []struct {
		editorconfig, options, want string
	}{
		{
			editorconfig: "root = true\n\n[*.sh]\nindent_style = space\nindent_size = 4\nswitch_case_indent = true\n",
			want:         "case \"$1\" in\n    a)\n        up\n        ;;\n    b)\n        down\n        ;;\nesac\n",
		},
		{
			editorconfig: "root = true\n\n[*.sh]\nswitch_case_indent = true\n",
			options:      `, "initializationOptions": {"format": {"switchCaseIndent": false, "indent": 2}}`,
			want:         "case \"$1\" in\na)\n  up\n  ;;\nb)\n  down\n  ;;\nesac\n",
		},
	} {
		root := t.TempDir()
		writeFiles(t, root, map[string]string{".editorconfig": tc.editorconfig})
		uri := fileuri.FromPath(filepath.Join(root, "main.sh"))
		c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", `+actionCapabilities+tc.options+`}`)
		c.notify("textDocument/didOpen", openParams(uri, src))
		action, ok := titled(codeActions(t, c, rangeParams(uri, 0, 0, "refactor.rewrite")), "Convert to case")
		if !ok {
			t.Fatalf("Convert to case not offered with %q%s", tc.editorconfig, tc.options)
		}
		edit := resolveAction(t, c, action)
		if got := applyLSPEdits(src, edit.DocumentChanges[0].Edits); got != tc.want {
			t.Errorf("with %q%s:\n got %q\nwant %q", tc.editorconfig, tc.options, got, tc.want)
		}
	}
}
