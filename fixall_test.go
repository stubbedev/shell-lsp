package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/cli"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/server"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var fixAllScripts = map[string]struct{ text, fixed string }{
	"fixes.bash": {
		"#!/bin/bash\nnow=`date`\ncp -r src dst\n[[ x$now = xmonday ]] && echo \"$now\"\n[ -a /etc/passwd ] && echo $1\nslots[$((1+1))]=x\ncd /tmp\necho \"${slots[*]}\"\n",
		"#!/bin/bash\nnow=$(date)\ncp -R src dst\n[[ $now = monday ]] && echo \"$now\"\n[ -e /etc/passwd ] && echo $1\nslots[1+1]=x\ncd /tmp\necho \"${slots[*]}\"\n",
	},
	"fixes.sh": {
		"#!/bin/sh\nsource ./lib.sh\nfunction f { :; }\ntrap f SIGINT\nif [ \"$1\" == x ]; then egrep 'a|b' f &> log; fi\nx=$(< /etc/hostname)\necho $x\n",
		"#!/bin/sh\n. ./lib.sh\nf() { :; }\ntrap f INT\nif [ \"$1\" = x ]; then grep -E 'a|b' f > log 2>&1; fi\nx=$(cat /etc/hostname)\necho $x\n",
	},
}

func TestCheckDiffAgreesWithServerFixAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, script := range fixAllScripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script.text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	client := startFixAllClient(t, dir)
	for name, script := range fixAllScripts {
		fixed := client.fixAll(fileuri.FromPath(filepath.Join(dir, name)), script.text)
		if fixed != script.fixed {
			t.Errorf("%s: the server's fix-all gives %q, want %q", name, fixed, script.fixed)
		}
		want := format.Unified("a/"+name, "b/"+name, script.text, fixed) + "\n"
		var stdout, stderr bytes.Buffer
		cli.Run(cli.Env{Args: []string{"check", "-f", "diff", name}, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Version: "test", Dir: dir})
		if stdout.String() != want {
			t.Errorf("%s: check -f diff =\n%s\nthe server's fix-all gives\n%s(stderr %q)", name, stdout.String(), want, stderr.String())
		}
	}
}

type fixAllClient struct {
	t      *testing.T
	conn   *jsonrpc.Conn
	nextID int
}

func startFixAllClient(t *testing.T, dir string) *fixAllClient {
	t.Helper()
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	srv := server.New(jsonrpc.NewConn(serverReader, serverWriter), "test")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Run(t.Context())
		_ = serverWriter.Close()
	}()
	t.Cleanup(func() {
		_ = clientWriter.Close()
		<-done
	})
	c := &fixAllClient{t: t, conn: jsonrpc.NewConn(clientReader, clientWriter)}
	root := fileuri.FromPath(dir)
	c.call("initialize", map[string]any{
		"rootUri":          root,
		"workspaceFolders": []map[string]string{{"uri": root, "name": "w"}},
		"capabilities":     map[string]any{"workspace": map[string]any{"applyEdit": true}},
	}, nil)
	if err := c.conn.Notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *fixAllClient) fixAll(uri, text string) string {
	c.t.Helper()
	if err := c.conn.Notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "shellscript", "version": 1, "text": text}}); err != nil {
		c.t.Fatal(err)
	}
	var edits []lsp.TextEdit
	c.call("workspace/executeCommand", lsp.ExecuteCommandParams{Command: "shellLsp.fixAll", Arguments: []json.RawMessage{mustJSON(c.t, uri)}}, func(edit lsp.WorkspaceEdit) {
		if len(edit.Changes) != 1 {
			c.t.Fatalf("fix-all edited %d documents, want only %s", len(edit.Changes), uri)
		}
		for _, changes := range edit.Changes {
			edits = append(edits, changes...)
		}
	})
	doc := docstore.NewDocument(uri, "shellscript", 1, text)
	offsets := make([]textedit.Edit, len(edits))
	for i, e := range edits {
		offsets[i] = textedit.Edit{Start: doc.PositionToOffset(e.Range.Start, docstore.EncodingUTF16), End: doc.PositionToOffset(e.Range.End, docstore.EncodingUTF16), NewText: e.NewText}
	}
	return textedit.Apply(text, offsets)
}

func (c *fixAllClient) call(method string, params any, applyEdit func(lsp.WorkspaceEdit)) {
	c.t.Helper()
	c.nextID++
	id := mustJSON(c.t, c.nextID)
	if err := c.conn.Request(id, method, params); err != nil {
		c.t.Fatal(err)
	}
	for {
		msg, err := c.conn.Read()
		if err != nil {
			c.t.Fatalf("reading the reply to %s: %v", method, err)
		}
		switch {
		case msg.IsResponse() && string(msg.ID) == string(id):
			if msg.Error != nil {
				c.t.Fatalf("%s failed: %v", method, msg.Error)
			}
			return
		case msg.IsRequest() && msg.Method == "workspace/applyEdit":
			var p lsp.ApplyWorkspaceEditParams
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				c.t.Fatal(err)
			}
			applyEdit(p.Edit)
			if err := c.conn.Reply(msg.ID, lsp.ApplyWorkspaceEditResult{Applied: true}, nil); err != nil {
				c.t.Fatal(err)
			}
		case msg.IsRequest():
			if err := c.conn.Reply(msg.ID, nil, nil); err != nil {
				c.t.Fatal(err)
			}
		}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
