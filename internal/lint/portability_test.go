package lint

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestPortabilitySeverityFollowsDialect(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		dialect  dialect.Dialect
		severity Severity
		shell    string
	}{
		{dialect: dialect.Posix, severity: SeverityWarning, shell: "POSIX sh"},
		{dialect: dialect.Dash, severity: SeverityError, shell: "dash"},
		{dialect: dialect.Busybox, severity: SeverityError, shell: "busybox sh"},
	} {
		findings := Check(parser.Parse("items=(a b)\n", tc.dialect), nil, Options{Exclude: []Code{2034}})
		if len(findings) != 1 || findings[0].Code != 3030 {
			t.Fatalf("%s: findings = %+v, want one SC3030", tc.dialect, findings)
		}
		if f := findings[0]; f.Severity != tc.severity || !strings.Contains(f.Message, tc.shell) {
			t.Errorf("%s: severity %d message %q, want severity %d naming %s", tc.dialect, f.Severity, f.Message, tc.severity, tc.shell)
		}
	}
	for _, d := range []dialect.Dialect{dialect.Bash, dialect.Ksh, dialect.Zsh} {
		for _, f := range Check(parser.Parse("items=(a b)\n", d), nil, Options{}) {
			if f.Code == 3030 {
				t.Errorf("%s: unexpected %+v", d, f)
			}
		}
	}
}
