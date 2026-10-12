package host

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestOf(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		uri, languageID string
		want            Language
		ok              bool
	}{
		{"file:///w/Makefile", "makefile", Make, true},
		{"file:///w/rules", "make", Make, true},
		{"file:///w/Makefile", "shellscript", 0, false},
		{"file:///w/Makefile", "", Make, true},
		{"file:///w/GNUmakefile", "", Make, true},
		{"file:///w/build/common.mk", "", Make, true},
		{`C:\w\rules.mak`, "", Make, true},
		{"file:///w/Makefile.sh", "", 0, false},
		{"file:///w/run.sh", "", 0, false},
		{"file:///w/justfile", "just", Just, true},
		{"file:///w/Justfile", "", Just, true},
		{"file:///w/.justfile", "", Just, true},
		{"file:///w/tasks.just", "", Just, true},
	} {
		got, ok := Of(tt.uri, tt.languageID)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Of(%q, %q) = %v, %v, want %v, %v", tt.uri, tt.languageID, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseKeepsEachRecipeLineToItself(t *testing.T) {
	t.Parallel()

	text := "a:\n\techo \"unclosed\nb:\n\tif true; then echo x\nc:\n\techo $$(date\nd:\n\techo == x\n"
	script := Make.Project(docstore.NewDocument("file:///w/Makefile", "makefile", 1, text))
	tree := script.Parse(nil)
	type located struct {
		line, character uint32
		message         string
	}
	var got []located
	for _, e := range tree.Errors {
		at := script.Doc.OffsetToPosition(e.Span.Start, docstore.EncodingUTF16)
		got = append(got, located{at.Line, at.Character, e.Message})
	}
	want := []located{
		{1, 6, "missing `\"` to match `\"`"},
		{3, 1, "missing `fi` to match `if`"},
		{5, 1, "missing `)` to match `(`"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("errors\n got %+v\nwant %+v", got, want)
	}
	if at := script.Doc.OffsetToPosition(len(script.Doc.Text()), docstore.EncodingUTF16); at != (lsp.Position{Line: 7, Character: 10}) {
		t.Errorf("the end of the projection is at %+v", at)
	}
}
