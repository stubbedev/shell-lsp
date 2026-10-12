package cli

import (
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

func TestCheckUsageNamesEveryOptionAndValue(t *testing.T) {
	t.Parallel()

	for _, o := range checkOptionTable {
		if !strings.Contains(checkUsage, "--"+o.long) {
			t.Errorf("check usage does not mention --%s", o.long)
		}
		if o.short != 0 && !strings.Contains(checkUsage, "  -"+string(o.short)) {
			t.Errorf("check usage does not mention -%c", o.short)
		}
	}
	for _, values := range [][]string{colorNames[:], formatNames(), lint.ProfileNames(), dialect.Names(), lint.SeverityNames()} {
		if !strings.Contains(checkUsage, "("+strings.Join(values, ", ")) {
			t.Errorf("check usage does not list %v", values)
		}
	}
}

func TestFmtUsageNamesEveryFlagAndLanguage(t *testing.T) {
	t.Parallel()

	var o fmtOptions
	posix := false
	fs, _ := fmtFlags(&o, &posix, io.Discard)
	fs.VisitAll(func(f *flag.Flag) {
		if !strings.Contains(fmtUsage, "-"+f.Name+" ") && !strings.Contains(fmtUsage, "-"+f.Name+",") && !strings.Contains(fmtUsage, "-"+f.Name+"[") {
			t.Errorf("fmt usage does not mention -%s", f.Name)
		}
	})
	for _, name := range fmtLanguages() {
		if _, err := languageNamed(name); err != nil {
			t.Errorf("fmt lists the language %s but rejects it: %v", name, err)
		}
		if !strings.Contains(fmtUsage, name) {
			t.Errorf("fmt usage does not list the language %s", name)
		}
	}
}
