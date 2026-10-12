package lint

import (
	"testing"
)

func TestCodeSetRangesExcludeTheirEnd(t *testing.T) {
	t.Parallel()

	set, err := ParseCodeSet("SC2086, SC2000-SC2010,SL1001,SC2050-SC2050")
	if err != nil {
		t.Fatal(err)
	}
	for code, want := range map[Code]bool{2086: true, 2000: true, 2009: true, 2010: false, 2050: false, 11001: true, 2034: false} {
		if set.Has(code) != want {
			t.Errorf("Has(%v) = %t, want %t", code, !want, want)
		}
	}
	all, err := ParseCodeSet("all")
	if err != nil || !all.Has(2034) || !all.Has(11001) {
		t.Errorf("all = %+v, %v", all, err)
	}
	for _, bad := range []string{"SC20x", "SC2000-", "SC2000-SCx"} {
		if _, err := ParseCodeSet(bad); err == nil {
			t.Errorf("ParseCodeSet(%q) accepted a malformed code", bad)
		}
	}
}

func TestDisabledRangeKeepsItsEnd(t *testing.T) {
	t.Parallel()

	src := "#!/bin/sh\n# shellcheck disable=SC2000-SC2086\necho $1\n"
	if got := findingsWith(t, src, 2086); len(got) != 1 {
		t.Errorf("disable=SC2000-SC2086 left %d SC2086 findings, want 1: shellcheck excludes a range's end", len(got))
	}
}
