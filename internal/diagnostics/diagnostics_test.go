package diagnostics

import (
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func document(t *testing.T, text string) *docstore.Document {
	t.Helper()

	store := docstore.NewStore()
	store.Open(lsp.TextDocumentItem{URI: "file:///t.sh", Version: 1, Text: text})
	doc, ok := store.View().Document("file:///t.sh")
	if !ok {
		t.Fatal("document not stored")
	}
	return doc
}

func TestSyntaxDiagnostics(t *testing.T) {
	t.Parallel()

	text := "if true; then\n  echo x\narr=(1)\n"
	doc := document(t, text)
	got := Syntax(parser.Parse(text, dialect.Posix), doc, docstore.EncodingUTF16)
	if len(got) != 2 {
		t.Fatalf("diagnostics = %+v, want 2", got)
	}
	codes := []string{got[0].Code, got[1].Code}
	if !slices.Contains(codes, CodeSyntax) || !slices.Contains(codes, CodeDialect) {
		t.Errorf("codes = %v, want syntax and dialect", codes)
	}
	for _, d := range got {
		if d.Severity != lsp.SeverityError || d.Source != Source {
			t.Errorf("diagnostic = %+v", d)
		}
	}
}

func TestShellcheckArgs(t *testing.T) {
	t.Parallel()

	settings := config.Shellcheck{Arguments: []string{"-x"}, Exclude: []int{2034, 1090}}
	got, err := ShellcheckArgs(settings, dialect.Posix)
	if err != nil {
		t.Fatalf("ShellcheckArgs() error = %v", err)
	}
	want := []string{"--format=json1", "--shell=sh", "--exclude=SC2034,SC1090", "-x", "-"}
	if !slices.Equal(got, want) {
		t.Errorf("ShellcheckArgs() = %v, want %v", got, want)
	}
	if _, err := ShellcheckArgs(settings, dialect.Zsh); !errors.Is(err, ErrDialectUnsupported) {
		t.Errorf("zsh error = %v, want ErrDialectUnsupported", err)
	}
}

func TestParseShellcheckMapsCodePointsToEncoding(t *testing.T) {
	t.Parallel()

	text := "echo \"é😀\" $y\n\techo `date` $unused\n"
	output := `{"comments":[
		{"file":"-","line":1,"endLine":1,"column":11,"endColumn":13,"level":"warning","code":2154,"message":"y is referenced but not assigned.","fix":null},
		{"file":"-","line":1,"endLine":1,"column":11,"endColumn":13,"level":"info","code":2086,"message":"Double quote.","fix":{"replacements":[
			{"line":1,"endLine":1,"column":11,"endColumn":11,"insertionPoint":"afterEnd","precedence":7,"replacement":"\""},
			{"line":1,"endLine":1,"column":13,"endColumn":13,"insertionPoint":"beforeStart","precedence":7,"replacement":"\""}]}},
		{"file":"-","line":2,"endLine":2,"column":7,"endColumn":13,"level":"style","code":2006,"message":"Use $(...).","fix":null}]}`
	doc := document(t, text)
	got, err := ParseShellcheck([]byte(output), doc, docstore.EncodingUTF16)
	if err != nil {
		t.Fatalf("ParseShellcheck() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("diagnostics = %+v", got)
	}
	want := lsp.Range{Start: lsp.Position{Line: 0, Character: 11}, End: lsp.Position{Line: 0, Character: 13}}
	if got[0].Range != want {
		t.Errorf("utf-16 range = %+v, want %+v (emoji is two code units)", got[0].Range, want)
	}
	if got[0].Severity != lsp.SeverityWarning || got[0].Code != "SC2154" || got[0].Source != ShellcheckSource {
		t.Errorf("diagnostic = %+v", got[0])
	}
	if got[0].CodeDescription == nil || got[0].CodeDescription.Href != "https://www.shellcheck.net/wiki/SC2154" {
		t.Errorf("codeDescription = %+v", got[0].CodeDescription)
	}
	var fix FixData
	if err := json.Unmarshal(got[1].Data, &fix); err != nil {
		t.Fatalf("decoding fix data: %v", err)
	}
	if len(fix.Edits) != 2 || fix.Edits[0].NewText != `"` || fix.Edits[1].Range.Start.Character != 13 {
		t.Errorf("fix edits = %+v", fix.Edits)
	}
	if got[2].Severity != lsp.SeverityHint || !slices.Equal(got[2].Tags, []lsp.DiagnosticTag{lsp.TagDeprecated}) {
		t.Errorf("style diagnostic = %+v", got[2])
	}
	if got[2].Range.Start != (lsp.Position{Line: 1, Character: 6}) {
		t.Errorf("tab counted as one column: range = %+v", got[2].Range)
	}
}

func TestParseShellcheckRejectsGarbage(t *testing.T) {
	t.Parallel()

	doc := document(t, "x\n")
	for _, output := range []string{"not json", `{"comments":[{"level":"loud","code":1}]}`} {
		if _, err := ParseShellcheck([]byte(output), doc, docstore.EncodingUTF16); err == nil {
			t.Errorf("ParseShellcheck(%q) succeeded, want error", output)
		}
	}
}

func TestRunShellcheck(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not installed")
	}
	bin, err := ResolveBinary("shellcheck")
	if err != nil {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
	text := "#!/bin/sh\necho $1\n"
	args, err := ShellcheckArgs(config.Default().Shellcheck, dialect.Posix)
	if err != nil {
		t.Fatalf("ShellcheckArgs() error = %v", err)
	}
	output, err := RunShellcheck(t.Context(), bin, args, t.TempDir(), text)
	if err != nil {
		t.Fatalf("RunShellcheck() error = %v", err)
	}
	got, err := ParseShellcheck(output, document(t, text), docstore.EncodingUTF16)
	if err != nil {
		t.Fatalf("ParseShellcheck() error = %v", err)
	}
	if len(got) != 1 || got[0].Code != "SC2086" {
		t.Errorf("diagnostics = %+v, want SC2086", got)
	}
	if _, err := RunShellcheck(t.Context(), bin, []string{"--format=json1", "--shell=nope", "-"}, "", text); err == nil {
		t.Error("RunShellcheck with invalid shell succeeded, want error")
	}
}

func TestResolveBinaryMissing(t *testing.T) {
	t.Parallel()

	if _, err := ResolveBinary("shellcheck-definitely-not-installed"); err == nil {
		t.Error("ResolveBinary(missing) succeeded")
	}
}
