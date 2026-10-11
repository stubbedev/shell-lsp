package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiagnosticTagsEncodeAsIntegerArray(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(Diagnostic{Tags: []DiagnosticTag{TagUnnecessary, TagDeprecated}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"tags":[1,2]`) {
		t.Fatalf("tags not encoded as an integer array: %s", raw)
	}
}
