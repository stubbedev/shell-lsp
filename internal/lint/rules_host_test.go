package lint

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestHostCodeRanges(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		code             Code
		host, just, make bool
		text             string
	}{
		{CodeJustSyntax, true, true, false, "SL5001"},
		{CodeJustNeedsSetting, true, true, false, "SL5023"},
		{CodeMakeSyntax, true, false, true, "SL6001"},
		{CodeMakeUnusedVariable, true, false, true, "SL6014"},
		{CodeNixUndefinedVariable, true, false, false, "SL8002"},
		{CodeNixEmptyBinding, true, false, false, "SL8013"},
		{codeDownloadedScript, false, false, false, "SL1005"},
		{2086, false, false, false, "SC2086"},
	} {
		if tt.code.Host() != tt.host || tt.code.Just() != tt.just || tt.code.Make() != tt.make || tt.code.String() != tt.text {
			t.Errorf("%s: Host %v Just %v Make %v", tt.code, tt.code.Host(), tt.code.Just(), tt.code.Make())
		}
	}
}

func TestStatixLintsAreCheckedByUniqueCodes(t *testing.T) {
	t.Parallel()

	seen := map[Code]string{}
	ids := map[int]string{}
	for _, l := range StatixLints() {
		if other, repeated := seen[l.Code]; repeated {
			t.Errorf("%s and %s share %s", l.Name, other, l.Code)
		}
		if other, repeated := ids[l.ID]; repeated {
			t.Errorf("%s and %s share W%02d", l.Name, other, l.ID)
		}
		seen[l.Code], ids[l.ID] = l.Name, l.Name
		if _, known := catalog[l.Code]; !known || !l.Code.Nix() {
			t.Errorf("%s maps to %s, which is no Nix check", l.Name, l.Code)
		}
		if !StatixCodes().Has(l.Code) {
			t.Errorf("StatixCodes lacks %s", l.Code)
		}
	}
	if len(seen) != 17 {
		t.Errorf("%d statix lints are mapped, and statix list names 17", len(seen))
	}
	for _, code := range []Code{CodeNixUnusedLet, CodeNixUnusedArgument, CodeNixUnusedFormal, CodeNixUnusedUnderscore, CodeNixUsedUnderscore} {
		if !DeadnixCodes().Has(code) {
			t.Errorf("DeadnixCodes lacks %s", code)
		}
	}
}

func TestHostFinding(t *testing.T) {
	t.Parallel()

	f := HostFinding(CodeJustUnusedVariable, parser.Span{Start: 1, End: 2}, "x")
	if f.Severity != SeverityInfo || f.Tag != TagUnnecessary || f.Span.Start != 1 || f.Message != "x" {
		t.Errorf("HostFinding = %+v", f)
	}
	defer func() {
		if recover() == nil {
			t.Error("HostFinding accepted a shell check")
		}
	}()
	HostFinding(2086, parser.Span{}, "x")
}

func TestProfilesKeepHostFindings(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		profile  Profile
		code     Code
		expected bool
	}{
		{ProfileRecommended, CodeNixUnusedLet, true},
		{ProfileStrict, CodeNixUnusedLet, true},
		{ProfileMinimal, CodeNixUnusedLet, false},
		{ProfileMinimal, CodeNixUselessParens, false},
		{ProfileMinimal, CodeNixUndefinedVariable, true},
		{ProfileMinimal, CodeNixDuplicateAttribute, true},
		{ProfileMinimal, CodeNixOrIdentifier, true},
		{ProfileMinimal, CodeNixSyntax, true},
	} {
		if got := tt.profile.KeepsHost(HostFinding(tt.code, parser.Span{}, "x")); got != tt.expected {
			t.Errorf("%s keeps %s: %v", tt.profile, tt.code, got)
		}
	}
}
