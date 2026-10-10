package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestElevatedRedirectionSeverity(t *testing.T) {
	t.Parallel()

	for script, want := range map[string][]Severity{
		"sudo wc -l < /etc/shadow\n":          {SeverityInfo},
		"sudo echo 1 > /proc/sys/vm/swappy\n": {SeverityWarning},
		"doas echo line >> /etc/hosts\n":      {SeverityWarning},
		"run0 ls &> /var/log/listing\n":       {SeverityWarning},
		"sudo sort < /etc/hosts > /etc/out\n": {SeverityInfo, SeverityWarning},
	} {
		var got []Severity
		for _, f := range Check(parser.Parse(script, dialect.Bash), nil, Options{}) {
			if f.Code == 2024 {
				got = append(got, f.Severity)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q: SC2024 severities %v, want %v", script, got, want)
		}
	}
}
