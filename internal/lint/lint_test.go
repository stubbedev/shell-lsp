package lint

import (
	"errors"
	"strconv"
	"testing"
)

func TestParseCode(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]Code{"SC2034": 2034, "sc1090": 1090, " 2154 ": 2154} {
		if got, err := ParseCode(input); err != nil || got != want {
			t.Errorf("ParseCode(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
	for input, cause := range map[string]error{"nope": strconv.ErrSyntax, "SC99999": strconv.ErrRange, "SC0": nil} {
		_, err := ParseCode(input)
		if err == nil {
			t.Errorf("ParseCode(%q) succeeded, want an error", input)
			continue
		}
		if cause != nil && !errors.Is(err, cause) {
			t.Errorf("ParseCode(%q) = %v, want it to wrap %v", input, err, cause)
		}
	}
}
