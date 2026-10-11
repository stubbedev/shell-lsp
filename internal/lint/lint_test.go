package lint

import (
	"errors"
	"strconv"
	"testing"
)

func TestCodesRoundTripThroughText(t *testing.T) {
	t.Parallel()

	for _, code := range []Code{1, 2034, nativeBase - 1, nativeBase + 1, nativeBase + 1001} {
		if got, err := ParseCode(code.String()); err != nil || got != code {
			t.Errorf("ParseCode(%s) = %v, %v", code, got, err)
		}
	}
	if (nativeBase+1001).String() != "SL1001" || Code(2034).String() != "SC2034" {
		t.Errorf("codes render as %s and %s", nativeBase+1001, Code(2034))
	}
}

func TestParseCode(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]Code{"SC2034": 2034, "sc1090": 1090, " 2154 ": 2154, "SL1001": nativeBase + 1001, "sl7": nativeBase + 7} {
		if got, err := ParseCode(input); err != nil || got != want {
			t.Errorf("ParseCode(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
	for input, cause := range map[string]error{"nope": strconv.ErrSyntax, "SC99999": strconv.ErrRange, "SC0": nil, "SC10000": nil, "SL0": nil, "SL12000": nil} {
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
