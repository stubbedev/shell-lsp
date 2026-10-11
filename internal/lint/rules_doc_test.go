package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeRulesAreDocumented(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "rules.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range Codes() {
		if code.Native() && !strings.Contains(string(doc), "\n## "+code.String()+"\n") {
			t.Errorf("docs/rules.md has no section for %s", code)
		}
	}
}
