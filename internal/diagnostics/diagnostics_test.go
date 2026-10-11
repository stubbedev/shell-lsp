package diagnostics

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/bats"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
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

	text := "if true; then\n  echo x\necho ${name@Q}\n"
	doc := document(t, text)
	got := Syntax(parser.Parse(text, dialect.Ksh), doc, docstore.EncodingUTF16)
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

func TestLintMapsFindings(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\necho 😀 `date` $1\n"
	doc := document(t, text)
	findings := lint.Check(parser.Parse(text, dialect.Bash), nil, lint.Options{})
	got := Lint(findings, doc, docstore.EncodingUTF16, true)
	byCode := map[string]lsp.Diagnostic{}
	for _, d := range got {
		byCode[d.Code] = d
	}
	backticks, ok := byCode["SC2006"]
	if !ok || backticks.Severity != lsp.SeverityHint || !slices.Equal(backticks.Tags, []lsp.DiagnosticTag{lsp.TagDeprecated}) {
		t.Fatalf("SC2006 = %+v in %+v", backticks, got)
	}
	if want := (lsp.Range{Start: lsp.Position{Line: 1, Character: 8}, End: lsp.Position{Line: 1, Character: 14}}); backticks.Range != want {
		t.Errorf("SC2006 range = %+v, want %+v (UTF-16 after a surrogate pair)", backticks.Range, want)
	}
	unquoted, ok := byCode["SC2086"]
	if !ok || unquoted.Severity != lsp.SeverityInformation || unquoted.Source != Source {
		t.Errorf("SC2086 = %+v", unquoted)
	}
	if unquoted.CodeDescription == nil || unquoted.CodeDescription.Href != "https://github.com/stubbedev/shell-lsp/blob/main/docs/checks.md#sc2086" {
		t.Errorf("SC2086 codeDescription = %+v, want its docs/checks.md row", unquoted.CodeDescription)
	}
	for _, d := range Lint(findings, doc, docstore.EncodingUTF16, false) {
		if d.CodeDescription != nil {
			t.Errorf("%s has a codeDescription although the client cannot show one", d.Code)
		}
	}
}

func TestBatsDiagnosticsCoverTheFailingLine(t *testing.T) {
	t.Parallel()

	doc := document(t, "#!/usr/bin/env bats\n@test \"x\" {\n\t[ 1 -eq 2 ]\n}\n")
	got := Bats([]bats.Failure{{Test: "x", Line: 3, Message: "test failed: x"}, {Test: "gone", Line: 40, Message: "test failed: gone"}}, doc, docstore.EncodingUTF16)
	want := []lsp.Diagnostic{
		{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 1}, End: lsp.Position{Line: 2, Character: 12}}, Severity: lsp.SeverityError, Source: BatsSource, Message: "test failed: x"},
		{Range: lsp.Range{Start: lsp.Position{Line: 4}, End: lsp.Position{Line: 4}}, Severity: lsp.SeverityError, Source: BatsSource, Message: "test failed: gone"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diagnostics = %+v\nwant         %+v", got, want)
	}
}
