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

	for _, code := range []Code{1, 2034, nativeBase - 1, nativeBase + 1, nativeBase + 1001, hadolintBase - 1, hadolintBase + 1, hadolintBase + 3008, familyEnd - 1} {
		if got, err := ParseCode(code.String()); err != nil || got != code {
			t.Errorf("ParseCode(%s) = %v, %v", code, got, err)
		}
	}
	if (nativeBase+1001).String() != "SL1001" || Code(2034).String() != "SC2034" || (hadolintBase+3008).String() != "DL3008" {
		t.Errorf("codes render as %s, %s and %s", nativeBase+1001, Code(2034), hadolintBase+3008)
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
	for c := notOptional + 1; c < optionalCheckCount; c++ {
		if found, ok := optionalNamed(c.String()); !ok || found != c {
			t.Errorf("optional check %d has the name %q, which does not name it back", c, c)
		}
	}
	for _, name := range []string{"check-unassigned-uppercase", "require-variable-braces", "arithmetic-injection", "all"} {
		if !EnablesOptional(name) {
			t.Errorf("EnablesOptional(%q) = false", name)
		}
	}
	if EnablesOptional("") || EnablesOptional("require-braces") {
		t.Error("EnablesOptional accepted a name that is not an optional check")
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

	for input, want := range map[string]Code{"SC2034": 2034, "sc1090": 1090, " 2154 ": 2154, "SL1001": nativeBase + 1001, "sl7": nativeBase + 7, "DL3008": hadolintBase + 3008, "dl1001": hadolintBase + 1001} {
		if got, err := ParseCode(input); err != nil || got != want {
			t.Errorf("ParseCode(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
	for input, cause := range map[string]error{"nope": strconv.ErrSyntax, "SC99999": strconv.ErrRange, "SC0": nil, "SC10000": nil, "SL0": nil, "SL12000": nil, "DL0": nil, "DL10000": nil} {
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

	closed := "words=($(grep -e \"a b\" -- \"$cur\"))\n"
	for _, d := range []dialect.Dialect{dialect.Bash, dialect.Zsh} {
		fixed := false
		for _, f := range Check(parser.Parse(closed+"words=($(grep -e \"a b -- \"$cur\"))\n", d), nil, Options{}) {
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

func TestShadowedPatternsNameTheOtherLine(t *testing.T) {
	t.Parallel()

	tree := parser.Parse("#!/bin/sh\ncase $1 in\n  *) echo a ;;\n  b) echo b ;;\nesac\n", dialect.Posix)
	want := map[Code]string{
		2221: "This pattern also matches everything the pattern on line 4 matches, so that one is never reached.",
		2222: "This pattern never matches because the pattern on line 3 catches everything it would.",
	}
	for _, f := range Check(tree, nil, Options{}) {
		message, ok := want[f.Code]
		if !ok {
			continue
		}
		if f.Message != message {
			t.Errorf("%s says %q, want %q", f.Code, f.Message, message)
		}
		delete(want, f.Code)
	}
	for code := range want {
		t.Errorf("%s is not reported", code)
	}
}

func TestTrapHandlersCallTheirFunctions(t *testing.T) {
	t.Parallel()

	src := "#!/bin/sh\ncleanup() {\n  echo bye\n}\nstop() {\n  echo stop\n}\nunused() {\n  echo never\n}\ntrap 'cleanup' EXIT\ntrap -- \"echo done; stop\" INT TERM\nsleep 1\nexit 0\n"
	var reported []int
	for _, f := range Check(parser.Parse(src, dialect.Posix), nil, Options{}) {
		if f.Code == 2329 {
			reported = append(reported, parser.Parse(src, dialect.Posix).Line(f.Span.Start))
		}
	}
	if !slices.Equal(reported, []int{8}) {
		t.Errorf("SC2329 on lines %v, want only the function no trap runs, on line 8", reported)
	}
}
