package main

import (
	"slices"
	"strings"
	"testing"
)

const benchOutput = `goos: linux
goarch: amd64
pkg: github.com/stubbedev/shell-lsp/internal/server
cpu: Some CPU
BenchmarkKeystroke/nix-12         	      10	 170000000 ns/op
BenchmarkKeystroke/nix-shell-typing-12         	      50	  20500000 ns/op
BenchmarkKeystroke/nix-12         	      10	 160000000 ns/op
BenchmarkKeystroke/nix-shell-typing-12         	      50	  21500000 ns/op
BenchmarkKeystroke/nix-12         	      10	 190000000 ns/op
BenchmarkKeystroke/nix-shell-typing-12         	      50	  20000000 ns/op
PASS
ok  	github.com/stubbedev/shell-lsp/internal/server	12.3s
`

func TestParseKeystrokesKeepsTheMedianOfEachCase(t *testing.T) {
	t.Parallel()

	got, err := parseKeystrokes(benchOutput, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []keystroke{{Name: "nix", MedianNanos: 170000000}, {Name: "nix-shell-typing", MedianNanos: 20500000}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseKeystrokesKeepsANameWithoutAProcessorSuffix(t *testing.T) {
	t.Parallel()

	got, err := parseKeystrokes("BenchmarkKeystroke/nix-shell 5 1000 ns/op\n", 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []keystroke{{Name: "nix-shell", MedianNanos: 1000}}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseKeystrokesRejectsMissingRuns(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, out string }{
		{"no results", "PASS\n"},
		{"fewer runs than asked", "BenchmarkKeystroke/nix-12 10 1000 ns/op\n"},
	} {
		if _, err := parseKeystrokes(tt.out, 2); err == nil {
			t.Errorf("%s: parsed without an error", tt.name)
		}
	}
}

func TestParseToolVersionReadsShellcheckAndNixfmt(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ program, out, want string }{
		{shellcheck, "ShellCheck - shell script analysis tool\nversion: 0.11.0\nlicense: GNU General Public License, version 3\n", "0.11.0"},
		{nixfmt, "nixfmt 1.5.0\n", "1.5.0"},
	} {
		got, err := parseToolVersion(tt.program, tt.out)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.program, got, tt.want)
		}
	}
	if _, err := parseToolVersion(nixfmt, "something else\n"); err == nil {
		t.Error("an output without a version parsed")
	}
}

func TestFormattersRoundToThreeSignificantDigits(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		micros float64
		want   string
	}{
		{180_000, "180 ms"},
		{20_700_000, "20.7 s"},
		{14_000, "14.0 ms"},
		{1_234_567, "1.23 s"},
		{750, "750 µs"},
	} {
		if got := formatMicros(tt.micros); got != tt.want {
			t.Errorf("formatMicros(%v) = %q, want %q", tt.micros, got, tt.want)
		}
	}
	for n, want := range map[int]string{0: "0", 999: "999", 1187: "1,187", 44765: "44,765", 1234567: "1,234,567"} {
		if got := formatCount(n); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestReplaceSectionKeepsWhatSurroundsIt(t *testing.T) {
	t.Parallel()

	doc := "head\n" + sectionBegin + "old\n" + sectionEnd + "tail\n"
	got, err := replaceSection(doc, "new\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "head\n" + sectionBegin + "new\n" + sectionEnd + "tail\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	for _, broken := range []string{"head\n", sectionBegin + "old\n", sectionEnd + sectionBegin} {
		if _, err := replaceSection(broken, "new\n"); err == nil {
			t.Errorf("%q: replaced without its markers", broken)
		}
	}
}

func TestDecodeReportRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	if _, err := decodeReport([]byte(`{"version": "1", "extra": 2}`)); err == nil {
		t.Error("a report with an unknown field decoded")
	}
	if _, err := decodeReport([]byte(`{} {}`)); err == nil {
		t.Error("two reports decoded as one")
	}
}

func TestRenderTableNamesEveryKeystrokeTheReadmeShows(t *testing.T) {
	t.Parallel()

	_, err := renderTable(report{Keystrokes: []keystroke{{Name: "nix", MedianNanos: 1}}})
	if err == nil || !strings.Contains(err.Error(), "nix-shell") {
		t.Errorf("a report without nix-shell rendered: %v", err)
	}
}
