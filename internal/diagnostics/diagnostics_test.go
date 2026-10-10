package diagnostics

import (
	"slices"
	"testing"

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

	text := "if true; then\n  echo x\necho ${name^^}\n"
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
	got := Lint(lint.Check(parser.Parse(text, dialect.Bash), nil, lint.Options{}), doc, docstore.EncodingUTF16)
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
	if !ok || unquoted.Severity != lsp.SeverityInformation || unquoted.Source != Source || unquoted.CodeDescription == nil {
		t.Errorf("SC2086 = %+v", unquoted)
	}
}
