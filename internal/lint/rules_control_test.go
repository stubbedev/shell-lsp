package lint

import (
	"strings"
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
			if f.Code == 2095 && len(f.Fixes) > 0 {
				got = append(got, textedit.Apply(src, fixEdits(f)))
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

func TestZshLoopFormsAreLoops(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"repeat 3 { break }\n",
		"repeat 3 do continue; done\n",
		"for f (a b) { break }\n",
		"foreach f (a b)\n\tbreak\nend\n",
		"while (( 1 )) { break }\n",
	} {
		for _, f := range Check(parser.Parse(src, dialect.Zsh), nil, Options{}) {
			if f.Code == 2105 {
				t.Errorf("%q: break or continue reported outside a loop: %s", src, f.Message)
			}
		}
	}
}

func TestZshAnonymousFunctionsRunInPlace(t *testing.T) {
	t.Parallel()

	src := "() {\n  return 0\n  print never\n} arg\nprint after\n"
	var unreachable []int
	for _, f := range Check(parser.Parse(src, dialect.Zsh), nil, Options{}) {
		if f.Code == 2317 {
			unreachable = append(unreachable, f.Span.Start)
		}
	}
	if want := strings.Index(src, "print never"); len(unreachable) != 1 || unreachable[0] != want {
		t.Errorf("unreachable commands at %v, want only the one after return at %d", unreachable, want)
	}
}

func TestZshAlwaysListRunsAfterTheTryListEnds(t *testing.T) {
	t.Parallel()

	src := "f() {\n  { return 1 } always { print cleanup }\n  print after\n}\nf\n"
	var unreachable []string
	for _, f := range Check(parser.Parse(src, dialect.Zsh), nil, Options{}) {
		if f.Code == 2317 {
			unreachable = append(unreachable, src[f.Span.Start:f.Span.End])
		}
	}
	if len(unreachable) != 1 || unreachable[0] != "print after" {
		t.Errorf("unreachable commands = %q, want only the one after the always list", unreachable)
	}
}
