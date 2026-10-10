package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const (
	diagnosticsWait = 20 * time.Second
	requestTimeout  = 30 * time.Second
	recallPrefix    = 2
)

type client struct {
	name     string
	cmd      *exec.Cmd
	conn     *jsonrpc.Conn
	incoming chan *jsonrpc.Message
	nextID   int
	diags    map[string][]lsp.Diagnostic
}

func start(ctx context.Context, name string, argv []string, root string) (*client, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec
	cmd.Dir = root
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", name, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s (%s): %w", name, strings.Join(argv, " "), err)
	}
	c := &client{name: name, cmd: cmd, conn: jsonrpc.NewConn(stdout, stdin), incoming: make(chan *jsonrpc.Message, 256), diags: map[string][]lsp.Diagnostic{}}
	go func() {
		defer close(c.incoming)
		for {
			msg, err := c.conn.Read()
			if err != nil {
				return
			}
			c.incoming <- msg
		}
	}()
	init := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   fileuri.FromPath(root),
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"completion":         map[string]any{"completionItem": map[string]any{"snippetSupport": false}},
				"publishDiagnostics": map[string]any{},
			},
		},
	}
	if _, err := c.request("initialize", init); err != nil {
		return nil, err
	}
	return c, c.conn.Notify("initialized", map[string]any{})
}

func (c *client) handle(msg *jsonrpc.Message) error {
	switch {
	case msg.IsRequest():
		var result any
		if msg.Method == "workspace/configuration" {
			result = []any{}
		}
		return c.conn.Reply(msg.ID, result, nil)
	case msg.Method == "textDocument/publishDiagnostics":
		var p lsp.PublishDiagnosticsParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return fmt.Errorf("%s published malformed diagnostics: %w", c.name, err)
		}
		c.diags[p.URI] = p.Diagnostics
	}
	return nil
}

func (c *client) request(method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := json.RawMessage(strconv.Itoa(c.nextID))
	if err := c.conn.Request(id, method, params); err != nil {
		return nil, fmt.Errorf("%s %s: %w", c.name, method, err)
	}
	deadline := time.After(requestTimeout)
	for {
		select {
		case msg, ok := <-c.incoming:
			if !ok {
				return nil, fmt.Errorf("%s exited during %s", c.name, method)
			}
			if msg.IsResponse() && string(msg.ID) == string(id) {
				if msg.Error != nil {
					return nil, fmt.Errorf("%s %s: %w", c.name, method, msg.Error)
				}
				return msg.Result, nil
			}
			if err := c.handle(msg); err != nil {
				return nil, err
			}
		case <-deadline:
			return nil, fmt.Errorf("%s %s timed out", c.name, method)
		}
	}
}

func (c *client) waitDiagnostics(uri string) ([]lsp.Diagnostic, bool) {
	deadline := time.After(diagnosticsWait)
	settle := time.After(diagnosticsWait)
	for {
		if _, ok := c.diags[uri]; ok {
			settle = time.After(500 * time.Millisecond)
		}
		select {
		case msg, ok := <-c.incoming:
			if !ok {
				return nil, false
			}
			if err := c.handle(msg); err != nil {
				return nil, false
			}
		case <-settle:
			d, ok := c.diags[uri]
			return d, ok
		case <-deadline:
			d, ok := c.diags[uri]
			return d, ok
		}
	}
}

func (c *client) stop() {
	_, _ = c.request("shutdown", nil)
	_ = c.conn.Notify("exit", nil)
	done := make(chan struct{})
	go func() {
		_ = c.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = c.cmd.Process.Kill()
	}
}

type probe struct {
	want     string
	position lsp.Position
}

func probes(text string, d dialect.Dialect) []probe {
	tree := parser.Parse(text, d)
	doc := docPositions(text)
	var out []probe
	for _, cmd := range tree.Commands {
		if len(cmd.Name) > recallPrefix && cmd.NameSpan.End-cmd.NameSpan.Start == len(cmd.Name) {
			out = append(out, probe{want: cmd.Name, position: doc(cmd.NameSpan.Start + recallPrefix)})
		}
	}
	for _, ref := range tree.References {
		if len(ref.Name) > recallPrefix && tree.Visible(ref.Name, ref.Within).Len() > 0 {
			out = append(out, probe{want: ref.Name, position: doc(ref.Span.Start + recallPrefix)})
		}
	}
	return out
}

func docPositions(text string) func(int) lsp.Position {
	return func(offset int) lsp.Position {
		line := strings.Count(text[:offset], "\n")
		col := offset - (strings.LastIndexByte(text[:offset], '\n') + 1)
		return lsp.Position{Line: uint32(line), Character: uint32(len([]rune(text[offset-col : offset])))} //nolint:gosec
	}
}

func completionLabels(raw json.RawMessage) ([]string, error) {
	var list lsp.CompletionList
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		return labelsOf(list.Items), nil
	}
	var items []lsp.CompletionItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decoding completion result %s: %w", raw, err)
	}
	return labelsOf(items), nil
}

func labelsOf(items []lsp.CompletionItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

type fileReport struct {
	path       string
	ours       []string
	theirs     []string
	theirsOK   bool
	probes     int
	oursHit    int
	theirsHit  int
	oursMissed []string
	theirsMiss []string
}

func diagnosticKeys(diags []lsp.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		code := d.Code
		if code == "" || code == "syntax" || code == "dialect" {
			code = d.Source + ":" + strings.SplitN(d.Message, " ", 4)[0]
		}
		out = append(out, fmt.Sprintf("%d:%s", d.Range.Start.Line+1, code))
	}
	slices.Sort(out)
	return out
}

func check(ours, theirs *client, path string) (fileReport, error) {
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return fileReport{}, err
	}
	text := string(raw)
	d, ok := dialect.FromName(filepath.Base(filepath.Dir(path)))
	if !ok {
		return fileReport{}, fmt.Errorf("%s is not in a dialect directory", path)
	}
	uri := fileuri.FromPath(path)
	open := lsp.DidOpenTextDocumentParams{TextDocument: lsp.TextDocumentItem{URI: uri, LanguageID: "shellscript", Version: 1, Text: text}}
	report := fileReport{path: path}
	for _, c := range []*client{ours, theirs} {
		if err := c.conn.Notify("textDocument/didOpen", open); err != nil {
			return fileReport{}, err
		}
	}
	oursDiags, _ := ours.waitDiagnostics(uri)
	theirDiags, theirsOK := theirs.waitDiagnostics(uri)
	report.ours, report.theirs, report.theirsOK = diagnosticKeys(oursDiags), diagnosticKeys(theirDiags), theirsOK
	for _, p := range probes(text, d) {
		report.probes++
		params := lsp.CompletionParams{TextDocument: lsp.TextDocumentIdentifier{URI: uri}, Position: p.position}
		for _, side := range []struct {
			c      *client
			hit    *int
			missed *[]string
		}{{ours, &report.oursHit, &report.oursMissed}, {theirs, &report.theirsHit, &report.theirsMiss}} {
			result, err := side.c.request("textDocument/completion", params)
			if err != nil {
				return fileReport{}, err
			}
			labels, err := completionLabels(result)
			if err != nil {
				return fileReport{}, err
			}
			if slices.Contains(labels, p.want) {
				*side.hit++
			} else {
				*side.missed = append(*side.missed, fmt.Sprintf("%s@%d:%d", p.want, p.position.Line+1, p.position.Character+1))
			}
		}
	}
	for _, c := range []*client{ours, theirs} {
		if err := c.conn.Notify("textDocument/didClose", lsp.DidCloseTextDocumentParams{TextDocument: lsp.TextDocumentIdentifier{URI: uri}}); err != nil {
			return fileReport{}, err
		}
	}
	return report, nil
}

func render(reports []fileReport, corpus string) string {
	var sb strings.Builder
	sb.WriteString("# Cross-check against bash-language-server\n\n")
	sb.WriteString("Generated by `just crosscheck` (`cmd/crosscheck`). Both servers see the same corpus file;\n")
	sb.WriteString("bash-language-server lints through the shellcheck on PATH, shell-lsp with its built-in linter.\n")
	sb.WriteString("Diagnostics are compared as `line:code`; completion recall asks each server to complete\n")
	sb.WriteString("the first two characters of every command name and assigned variable reference and checks that the\n")
	sb.WriteString("word actually written there is offered.\n\n")
	sb.WriteString("| File | Diagnostics (ours / bashls / shared) | Completion recall (ours / bashls) |\n| --- | --- | --- |\n")
	totalProbes, oursHits, theirsHits := 0, 0, 0
	for _, r := range reports {
		rel, _ := filepath.Rel(corpus, r.path)
		shared := 0
		for _, k := range r.ours {
			if slices.Contains(r.theirs, k) {
				shared++
			}
		}
		theirs := strconv.Itoa(len(r.theirs))
		if !r.theirsOK {
			theirs = "none published"
		}
		fmt.Fprintf(&sb, "| `%s` | %d / %s / %d | %d/%d / %d/%d |\n", rel, len(r.ours), theirs, shared, r.oursHit, r.probes, r.theirsHit, r.probes)
		totalProbes += r.probes
		oursHits += r.oursHit
		theirsHits += r.theirsHit
	}
	fmt.Fprintf(&sb, "\nTotal completion recall: ours %d/%d, bash-language-server %d/%d.\n", oursHits, totalProbes, theirsHits, totalProbes)
	sb.WriteString("\n## Differences\n")
	for _, r := range reports {
		rel, _ := filepath.Rel(corpus, r.path)
		onlyOurs := difference(r.ours, r.theirs)
		onlyTheirs := difference(r.theirs, r.ours)
		if len(onlyOurs)+len(onlyTheirs)+len(r.oursMissed)+len(r.theirsMiss) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n### `%s`\n\n", rel)
		line := func(label string, items []string) {
			if len(items) > 0 {
				fmt.Fprintf(&sb, "- %s: %s\n", label, "`"+strings.Join(items, "`, `")+"`")
			}
		}
		line("diagnostics only from shell-lsp", onlyOurs)
		line("diagnostics only from bash-language-server", onlyTheirs)
		line("completions shell-lsp missed", r.oursMissed)
		line("completions bash-language-server missed", r.theirsMiss)
	}
	return sb.String()
}

func difference(a, b []string) []string {
	var out []string
	for _, k := range a {
		if !slices.Contains(b, k) {
			out = append(out, k)
		}
	}
	return out
}

func run() error {
	oursPath := flag.String("ours", "", "path to the shell-lsp binary")
	bashls := flag.String("bashls", "bash-language-server start", "command line of bash-language-server")
	corpus := flag.String("corpus", "internal/parser/testdata/corpus", "corpus directory with one subdirectory per dialect")
	out := flag.String("out", "docs/crosscheck.md", "report path")
	flag.Parse()
	if *oursPath == "" {
		return errors.New("-ours is required")
	}
	root, err := filepath.Abs(*corpus)
	if err != nil {
		return err
	}
	var files []string
	for _, d := range []string{"bash", "sh", "dash"} {
		matches, err := filepath.Glob(filepath.Join(root, d, "*"))
		if err != nil {
			return err
		}
		for _, m := range matches {
			if filepath.Ext(m) != ".golden" {
				files = append(files, m)
			}
		}
	}
	slices.SortFunc(files, cmp.Compare)
	ctx := context.Background()
	ours, err := start(ctx, "shell-lsp", []string{*oursPath}, root)
	if err != nil {
		return err
	}
	defer ours.stop()
	theirs, err := start(ctx, "bash-language-server", strings.Fields(*bashls), root)
	if err != nil {
		return err
	}
	defer theirs.stop()
	var reports []fileReport
	for _, f := range files {
		r, err := check(ours, theirs, f)
		if err != nil {
			return fmt.Errorf("checking %s: %w", f, err)
		}
		reports = append(reports, r)
	}
	tmp := *out + ".tmp"
	if err := os.WriteFile(tmp, []byte(render(reports, root)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, *out)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "crosscheck:", err)
		os.Exit(1)
	}
}
