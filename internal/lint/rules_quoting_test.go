package lint

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestQuoteSafeVariables(t *testing.T) {
	t.Parallel()

	src := "region=eu-west-1\necho $region\ndirty=$(date)\necho $dirty\n"
	got := findingsWith(t, src, 2248, "quote-safe-variables")
	if len(got) != 1 {
		t.Fatalf("SC2248 findings = %+v, want one for the clean variable", got)
	}
	if fixed, want := textedit.Apply(src, got[0].Fix), "region=eu-west-1\necho \"$region\"\ndirty=$(date)\necho $dirty\n"; fixed != want {
		t.Errorf("fixed = %q, want %q", fixed, want)
	}
	if off := findingsWith(t, src, 2248); len(off) != 0 {
		t.Errorf("SC2248 reported without being enabled: %+v", off)
	}
	bats := "#!/usr/bin/env bats\n@test \"ok\" {\n  run true\n  [ $status -eq 0 ]\n}\n"
	if got := findingsWith(t, bats, 2248, "quote-safe-variables"); len(got) != 1 {
		t.Errorf("SC2248 on the Bats status = %+v, want one finding", got)
	}
}
