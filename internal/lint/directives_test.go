package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestDirectivePlacement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		script string
		want   []string
	}{
		{"after an opening brace", "#!/bin/sh\n:\n{ # shellcheck disable=SC2086\n  echo $1; }\n", nil},
		{"after then", "#!/bin/sh\n:\nif true; then # shellcheck disable=SC2086\n  echo $1\nfi\n", nil},
		{"after a semicolon applies to the next command", "#!/bin/sh\n:\necho $2; # shellcheck disable=SC2086\necho $1\n", []string{"SC2086 3:6"}},
		{"after a command's words covers its own line", "#!/bin/sh\n:\necho $1 # shellcheck disable=SC2086\necho $2\n", []string{"SC1126 3:9", "SC2086 4:6"}},
		{"after a backslash continuation", "#!/bin/sh\n:\necho $1 \\\n  # shellcheck disable=SC2086\necho $2\n", []string{"SC1126 4:3", "SC2086 5:6"}},
		{"a run before a closer is reported once", "#!/bin/sh\nif true; then\n  :\n  # shellcheck disable=SC2086\n  # note\n  # shellcheck disable=SC2034\nfi\n", []string{"SC1123 4:3"}},
		{"before a closer has no effect", "#!/bin/sh\nfor f in a b; do\n  echo \"$f\"\n  # shellcheck disable=SC2086\ndone\necho $1\n", []string{"SC1123 4:3", "SC2086 6:6"}},
		{"after a case terminator", "#!/bin/sh\ncase $1 in\n  a) echo $2 ;; # shellcheck disable=SC2086\n  b) echo $3 ;;\nesac\n", []string{"SC1124 3:17", "SC2086 3:11", "SC2086 4:11"}},
		{"empty value at the end of the file", "#!/bin/sh\n# shellcheck disable=", nil},
		{"inside a case branch body", "#!/bin/sh\ncase $1 in\n  a)\n    # shellcheck disable=SC2086\n    echo $2 ;;\nesac\n", nil},
		{"between pipeline stages covers the whole list", "#!/bin/sh\n:\ncat $1 |\n  # shellcheck disable=SC2086\n  grep $2 && echo $3\necho $4\n", []string{"SC1126 4:3", "SC2086 6:6"}},
		{"between and-or operands covers the whole list", "#!/bin/sh\n:\ntest -n $1 &&\n  # shellcheck disable=SC2086\n  grep -c x $2 | sort\necho $3\n", []string{"SC1126 4:3", "SC2086 6:6"}},
		{"between pipeline stages stops at the enclosing block", "#!/bin/sh\nif true; then\n  cat $1 |\n    # shellcheck disable=SC2086\n    grep $2\n  echo $3\nfi\n", []string{"SC1126 4:5", "SC2086 6:8"}},
		{"before a group covers the here-document bodies of its commands", "#!/bin/bash\n:\n# shellcheck disable=SC2154\n{ cat <<A; cat <<B; }\n$x\nA\n$y\nB\necho \"$z\"\n", []string{"SC2154 9:7"}},
	} {
		tree := parser.Parse(tc.script, dialect.Posix)
		var got []string
		for _, f := range Check(tree, nil, Options{}) {
			got = append(got, f.Code.String()+" "+position(tc.script, f.Span.Start))
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s:\n%s\ngot  %v\nwant %v", tc.name, tc.script, got, tc.want)
		}
	}
}

func TestRunReportsWhatEachDisabledCodeSilenced(t *testing.T) {
	t.Parallel()

	script := "#!/bin/bash\nx=$1\n# shellcheck disable=SC2086,SC2116\necho $x $x\n"
	report := Run(parser.Parse(script, dialect.Bash), nil, Options{})
	if len(report.Findings) != 0 {
		t.Errorf("findings = %+v", report.Findings)
	}
	if len(report.Disabled) != 2 {
		t.Fatalf("disabled = %+v", report.Disabled)
	}
	used, unused := report.Disabled[0], report.Disabled[1]
	if used.Text != "SC2086" || script[used.Span.Start:used.Span.End] != "SC2086" || len(used.Suppressed) != 2 {
		t.Errorf("SC2086 = %+v", used)
	}
	for _, i := range used.Suppressed {
		if report.Suppressed[i].Code != 2086 {
			t.Errorf("SC2086 points at a %s finding", report.Suppressed[i].Code)
		}
	}
	if unused.Text != "SC2116" || len(unused.Suppressed) != 0 {
		t.Errorf("SC2116 = %+v", unused)
	}
}

func TestUnusedDirectivesSilencedInTheirOwnDirective(t *testing.T) {
	t.Parallel()

	script := "#!/bin/bash\n# shellcheck enable=unused-directives\nx=$1\n# shellcheck disable=SL2009,SC2086\necho \"$x\"\n"
	if got := Check(parser.Parse(script, dialect.Bash), nil, Options{}); len(got) != 0 {
		t.Errorf("a directive that also disables SL2009 still got %+v", got)
	}
	script = "#!/bin/bash\n# shellcheck enable=unused-directives disable=SL2009\nx=$1\n# shellcheck disable=SC2086\necho \"$x\"\n"
	if got := Check(parser.Parse(script, dialect.Bash), nil, Options{}); len(got) != 0 {
		t.Errorf("a file-wide disable=SL2009 still got %+v", got)
	}
}

func TestUnknownDirectiveKeyNamesEveryKey(t *testing.T) {
	t.Parallel()

	got := findingsWith(t, "# shellcheck severity=error\necho hi\n", 1107)
	if len(got) != 1 {
		t.Fatalf("SC1107 findings = %+v, want one", got)
	}
	_, listed, _ := strings.Cut(strings.TrimSuffix(got[0].Message, "."), "known keys are ")
	if names := strings.FieldsFunc(strings.ReplaceAll(listed, " and ", ", "), func(r rune) bool { return r == ',' || r == ' ' }); !slices.Equal(names, directive.KeyNames()) {
		t.Errorf("SC1107 message %q lists %q, want every directive key", got[0].Message, names)
	}
}

func TestUnknownShellDirectiveNamesEveryDialect(t *testing.T) {
	t.Parallel()

	got := findingsWith(t, "# shellcheck shell=fish\necho hi\n", 1103)
	if len(got) != 1 {
		t.Fatalf("SC1103 findings = %+v, want one", got)
	}
	for d := range dialect.All.Members() {
		if !strings.Contains(got[0].Message, d.String()) {
			t.Errorf("SC1103 message %q does not name %s", got[0].Message, d)
		}
	}
}
