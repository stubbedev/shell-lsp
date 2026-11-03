package main

import (
	"errors"
	"testing"
)

func TestCompareTakesOptionsForTheServerUnderTest(t *testing.T) {
	t.Parallel()

	o, dir, err := parseCompare([]string{"-ours-init", `{"nix":{"evaluate":true}}`, "-ours-settle", "2m", "tree"})
	if err != nil || dir != "tree" || o.oursInit == "" || o.oursSettle.Minutes() != 2 {
		t.Errorf("parseCompare = %+v, %q, %v", o, dir, err)
	}
	if _, _, err := parseCompare([]string{"-ours-init", "{", "tree"}); err == nil || errors.Is(err, errUsage) {
		t.Errorf("broken JSON = %v, want an error that names it", err)
	}
}
