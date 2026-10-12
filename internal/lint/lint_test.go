package lint

import (
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
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

func TestOptionalChecksNameEveryOptIn(t *testing.T) {
	t.Parallel()

	checks := OptionalChecks()
	names := make([]string, len(checks))
	for i, check := range checks {
		names[i] = check.Name
		if (len(check.Codes) == 0) == (check.Summary == "") {
			t.Errorf("%s needs either the codes it turns on or a summary of what it changes: %+v", check.Name, check)
		}
	}
	if !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Errorf("OptionalChecks() = %v, want sorted unique names", names)
	}
	for _, want := range []string{uppercaseOptional, "require-variable-braces", "arithmetic-injection"} {
		if !IsOptionalCheck(want) {
			t.Errorf("OptionalChecks() lacks %s", want)
		}
	}
	for code, m := range catalog {
		if m.optional != "" && !IsOptionalCheck(m.optional) {
			t.Errorf("%s is gated by %s, which OptionalChecks() lacks", code, m.optional)
		}
	}
}

func TestNoExtendedAnalysisDefaultsTheDirectiveOff(t *testing.T) {
	t.Parallel()

	codes := func(script string, opts Options) []Code {
		var out []Code
		for _, f := range Check(parser.Parse(script, dialect.Posix), nil, opts) {
			out = append(out, f.Code)
		}
		return out
	}
	unreachable := "#!/bin/sh\nexit 0; echo never\n"
	if got := codes(unreachable, Options{}); !slices.Contains(got, 2317) {
		t.Fatalf("extended analysis is off by default: %v", got)
	}
	if got := codes(unreachable, Options{NoExtendedAnalysis: true}); slices.Contains(got, 2317) {
		t.Errorf("NoExtendedAnalysis kept data-flow findings: %v", got)
	}
	if got := codes("#!/bin/sh\n# shellcheck extended-analysis=true\nexit 0; echo never\n", Options{NoExtendedAnalysis: true}); !slices.Contains(got, 2317) {
		t.Errorf("a file-wide directive did not override NoExtendedAnalysis: %v", got)
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

func TestUnclosedSubstitutionInAnArray(t *testing.T) {
	t.Parallel()

	closed := "words=($(compgen -W \"a b\" -- \"$cur\"))\n"
	for _, d := range []dialect.Dialect{dialect.Bash, dialect.Zsh} {
		fixed := false
		for _, f := range Check(parser.Parse(closed+"words=($(compgen -W \"a b -- \"$cur\"))\n", d), nil, Options{}) {
			switch {
			case f.Code != 2207:
			case f.Span.Start < len(closed):
				fixed = len(f.Fixes) > 0
			case len(f.Fixes) > 0:
				t.Errorf("%s: the unclosed substitution got the fix %+v", d, f.Fixes)
			}
		}
		if !fixed {
			t.Errorf("%s: the closed substitution lost its fix", d)
		}
	}
}

func TestLetWithAnEmptyQuotedExpression(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"let \"", "let ''\n", "let \"\"\n"} {
		for _, d := range []dialect.Dialect{dialect.Bash, dialect.Posix, dialect.Zsh} {
			Check(parser.Parse(src, d), nil, Options{})
		}
	}
}
