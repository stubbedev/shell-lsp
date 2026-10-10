package lint

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestStdinConsumerFixes(t *testing.T) {
	t.Parallel()

	loop := func(body string) string { return "while read -r f; do\n  " + body + "\ndone < list\n" }
	tests := map[string]string{
		`ssh "$f" uptime`:           `ssh -n "$f" uptime`,
		`ffmpeg -i "$f" out.mp4`:    `ffmpeg -nostdin -i "$f" out.mp4`,
		`mplayer "$f" > /dev/null`:  `mplayer -noconsolecontrols "$f" > /dev/null`,
		`HandBrakeCLI -i "$f" -o x`: `HandBrakeCLI -i "$f" -o x < /dev/null`,
	}
	for body, want := range tests {
		src := loop(body)
		var got []string
		for _, f := range Check(parser.Parse(src, dialect.Bash), nil, Options{}) {
			if f.Code == 2095 && len(f.Fix) > 0 {
				got = append(got, textedit.Apply(src, f.Fix))
			}
		}
		if len(got) != 1 || got[0] != loop(want) {
			t.Errorf("fixes for %q = %q, want [%q]", body, got, loop(want))
		}
	}
}

func TestStdinConsumerSeverities(t *testing.T) {
	t.Parallel()

	src := "while read -r host; do\n  ssh \"$host\" uptime\ndone < hosts\n"
	got := map[Severity]int{}
	for _, f := range Check(parser.Parse(src, dialect.Bash), nil, Options{}) {
		if f.Code == 2095 {
			got[f.Severity]++
		}
	}
	if got[SeverityInfo] != 1 || got[SeverityWarning] != 1 || len(got) != 2 {
		t.Errorf("SC2095 severities = %v, want one info on the loop and one warning on ssh", got)
	}
}
