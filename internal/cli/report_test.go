package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var update = flag.Bool("update", false, "rewrite the golden report files")

func spanOf(t *testing.T, text, needle string) parser.Span {
	t.Helper()
	i := strings.Index(text, needle)
	if i < 0 {
		t.Fatalf("%q is not in %q", needle, text)
	}
	return parser.Span{Start: i, End: i + len(needle)}
}

func sampleReports(t *testing.T) []fileReport {
	t.Helper()
	a := "#!/bin/bash\necho $1 \"(it's)\"\nfoo=bar\n\tcd /tmp\nif true; then\n  :\nfi\n"
	b := "echo é `date`\n"
	dollar := spanOf(t, a, "$1")
	backticks := spanOf(t, b, "`date`")
	native, _ := lint.ParseCode("SL1001")
	return []fileReport{
		{name: "a.sh", doc: docstore.NewDocument("", "", 0, a), findings: []lint.Finding{
			{Code: 2086, Severity: lint.SeverityInfo, Span: dollar, Message: "Quote $1 & friends <now>.", Fix: []textedit.Edit{
				{Start: dollar.Start, End: dollar.Start, NewText: `"`}, {Start: dollar.End, End: dollar.End, NewText: `"`},
			}},
			{Code: native, Severity: lint.SeverityError, Span: dollar, Message: "Untrusted data reaches eval."},
			{Code: 2034, Severity: lint.SeverityWarning, Span: spanOf(t, a, "foo"), Message: "foo appears unused. Verify use (or export if used externally)."},
			{Code: 2164, Severity: lint.SeverityWarning, Span: spanOf(t, a, "cd /tmp"), Message: "Use 'cd ... || exit' in case cd fails.", Fix: []textedit.Edit{
				{Start: spanOf(t, a, "cd /tmp").End, End: spanOf(t, a, "cd /tmp").End, NewText: " || exit"},
			}},
			{Code: 1072, Severity: lint.SeverityError, Span: spanOf(t, a, "if true; then\n  :\nfi"), Message: "A clause spanning lines."},
		}},
		{name: "b.sh", doc: docstore.NewDocument("", "", 0, b), findings: []lint.Finding{
			{Code: 2006, Severity: lint.SeverityStyle, Span: backticks, Message: "Use $(...) notation instead of legacy backticks `...`.", Fix: []textedit.Edit{
				{Start: backticks.Start, End: backticks.End, NewText: "$(date)"},
			}},
		}},
		{name: "clean.sh", doc: docstore.NewDocument("", "", 0, "echo hi\n")},
	}
}

func TestReportFormatsMatchGoldenFiles(t *testing.T) {
	t.Parallel()

	reports := sampleReports(t)
	for _, f := range outputFormats {
		for _, color := range []bool{false, true} {
			if color && f.name != "tty" {
				continue
			}
			name := f.name
			if color {
				name += "-color"
			}
			var out bytes.Buffer
			f.write(&out, reports, reportOptions{color: color, wikiLinks: defaultWikiLinks, version: "1.2.3"})
			golden := filepath.Join("testdata", "report", name+".golden")
			if *update {
				if err := os.WriteFile(golden, out.Bytes(), 0o600); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%s: %v; run go test ./internal/cli -run Golden -update", name, err)
			}
			if out.String() != string(want) {
				t.Errorf("%s output differs from %s:\n%s", name, golden, out.String())
			}
		}
	}
}

func TestJSONReportsParseAndKeepNativeCodesDistinct(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	writeJSON1(&out, sampleReports(t), reportOptions{})
	var parsed struct {
		Comments []jsonComment `json:"comments"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	codes := map[int]bool{}
	for _, c := range parsed.Comments {
		codes[c.Code] = true
	}
	if !codes[2086] || !codes[11001] || codes[1001] {
		t.Errorf("codes = %v; SL1001 must stay apart from SC1001", codes)
	}
	var empty bytes.Buffer
	writeJSON(&empty, nil, reportOptions{})
	if empty.String() != "[]\n" {
		t.Errorf("an empty json report = %q, want []", empty.String())
	}
}

func TestSARIFReportIsValidJSONWithRulesForEveryResult(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	writeSARIF(&out, sampleReports(t), reportOptions{version: "1.2.3"})
	var log sarifLog
	if err := json.Unmarshal(out.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	run := log.Runs[0]
	for _, r := range run.Results {
		if r.RuleIndex >= len(run.Tool.Driver.Rules) || run.Tool.Driver.Rules[r.RuleIndex].ID != r.RuleID {
			t.Errorf("result %s points at rule %d of %v", r.RuleID, r.RuleIndex, run.Tool.Driver.Rules)
		}
	}
	if log.Version != sarifVersion || run.Tool.Driver.Version != "1.2.3" || len(run.Results) != 6 {
		t.Errorf("sarif log = %+v", log)
	}
}

func TestWikiLinksPreferSevereCodesAndShortenMessages(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	writeWikiLinks(&out, sampleReports(t), 2)
	want := "For more information:\n" +
		"  https://www.shellcheck.net/wiki/SC1072 -- A clause spanning lines.\n" +
		"  https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1001 -- Untrusted data reaches eval.\n"
	if out.String() != want {
		t.Errorf("wiki links =\n%s\nwant\n%s", out.String(), want)
	}
	out.Reset()
	writeWikiLinks(&out, sampleReports(t), 0)
	if out.Len() != 0 {
		t.Errorf("-W 0 still printed links: %q", out.String())
	}
}

func TestXMLAttributeEscapesLikeShellCheck(t *testing.T) {
	t.Parallel()

	if got := xmlAttribute("we-ird_name.sh: 'a' <b> & ./x é"); got != "we&#45;ird&#95;name.sh&#58; &#39;a&#39; &#60;b&#62; &#38; ./x é" {
		t.Errorf("xmlAttribute = %q", got)
	}
}

func TestCaretWidth(t *testing.T) {
	t.Parallel()

	text := "ab\ncdef é\n"
	for span, want := range map[parser.Span]string{
		{Start: 0, End: 1}: "^--", {Start: 0, End: 2}: "^--", {Start: 3, End: 6}: "^-^",
		{Start: 3, End: len(text) - 1}: "^----^", {Start: 0, End: 5}: "^--",
	} {
		if got := caret(text, span); got != want {
			t.Errorf("caret(%+v) = %q, want %q", span, got, want)
		}
	}
	if visualWidth("\tab\t") != 16 || visualWidth("é") != 1 {
		t.Errorf("visualWidth expands tabs to the next multiple of %d and counts runes", tabStop)
	}
}
