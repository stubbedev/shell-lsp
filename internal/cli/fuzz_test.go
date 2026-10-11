package cli

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
)

func FuzzParseCheckArgs(f *testing.F) {
	for _, seed := range []string{"-s\x00bash\x00a.sh", "--sev=warning\x00-e\x00SC2034,SC2086", "-f\x00json1\x00--\x00-x", "--norc\x00-x", "-W\x0099999999999999999999", "--shell", "-", "--enable=all\x00--include=1000"} {
		f.Add(seed)
	}
	statuses := []int{checkStatusFindings, checkStatusFiles, checkStatusSyntax, checkStatusOptions}
	f.Fuzz(func(t *testing.T, joined string) {
		args := strings.Split(joined, "\x00")
		opts, err := parseCheckArgs(args)
		if err != nil {
			if !slices.Contains(statuses, checkStatusOf(err)) {
				t.Fatalf("parseCheckArgs(%q) error %v has status %d", args, err, checkStatusOf(err))
			}
			return
		}
		for _, operand := range opts.operands {
			if !slices.Contains(args, operand) {
				t.Fatalf("parseCheckArgs(%q) invented operand %q", args, operand)
			}
		}
	})
}

func FuzzParseFmtArgs(f *testing.F) {
	for _, seed := range []string{"-w\x00a.sh", "-i\x004\x00-ln\x00mksh", "--filename\x00x", "-l=", "-i\x00-1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, joined string) {
		_, _ = parseFmtArgs(strings.Split(joined, "\x00"), io.Discard)
	})
}

func FuzzParseRC(f *testing.F) {
	for _, seed := range []string{"disable=SC2034\n# c\nshell=bash\n", "source-path=SCRIPTDIR:lib\nexternal-sources=true", "enable=all extended-analysis=false", "disable=SC9999999999999999999", "x='", "disable=1000-2000"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if _, err := shellcheckrc.Parse("/home/u/.shellcheckrc", text); err != nil && !strings.HasPrefix(err.Error(), "/home/u/.shellcheckrc:") {
			t.Fatalf("Parse error %q does not name the file", err)
		}
	})
}
