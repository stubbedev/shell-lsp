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
		{codeDownloadedScript, false, false, false, "SL1005"},
		{2086, false, false, false, "SC2086"},
	} {
		if tt.code.Host() != tt.host || tt.code.Just() != tt.just || tt.code.Make() != tt.make || tt.code.String() != tt.text {
			t.Errorf("%s: Host %v Just %v Make %v", tt.code, tt.code.Host(), tt.code.Just(), tt.code.Make())
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
