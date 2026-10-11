package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var markdownCell = strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`, "|", `\|`)

func renderChecks() string {
	var sb strings.Builder
	sb.WriteString("# Checks\n\n")
	sb.WriteString("Every check the linter runs, generated from the lint catalog by\n")
	sb.WriteString("`go test ./internal/lint -run TestChecksDoc -update`. `SC` codes reuse ShellCheck's numbering;\n")
	sb.WriteString("`SL` codes are shell-lsp's own and are described in [rules.md](rules.md). Optional checks run only\n")
	sb.WriteString("when switched on by name with `enable=`, `--enable` or the `lint.optional` setting.\n\n")
	sb.WriteString("| Code | Severity | Optional | Summary |\n| --- | --- | --- | --- |\n")
	for _, info := range Catalog() {
		optional := ""
		if info.Optional != "" {
			optional = "`" + info.Optional + "`"
		}
		fmt.Fprintf(&sb, "| [%s](%s) | %s | %s | %s |\n", info.Code, info.Reference(), info.Severity, optional, markdownCell.Replace(info.Summary))
	}
	return sb.String()
}

func TestChecksDoc(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "docs", "checks.md")
	want := renderChecks()
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (regenerate with -update): %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s is out of date; run go test ./internal/lint -run TestChecksDoc -update", path)
	}
}
