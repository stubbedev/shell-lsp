package directive

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		comment string
		ok      bool
		want    Directive
	}{
		{"# shellcheck disable=SC2086,SC2034 source=lib.sh", true, Directive{{"disable", "SC2086,SC2034"}, {"source", "lib.sh"}}},
		{"#shellcheck shell=dash", true, Directive{{"shell", "dash"}}},
		{"  # shellcheck disable=2086 # quoting is intended", true, Directive{{"disable", "2086"}}},
		{"# shellcheckx disable=SC2086", false, nil},
		{"# not a directive", false, nil},
		{"echo # shellcheck disable=SC2086", false, nil},
		{"# shellcheck shell='busybox sh' disable=\"SC1,SC2\"", true, Directive{{"shell", "busybox sh"}, {"disable", "SC1,SC2"}}},
		{"# shellcheck source=a'#'b #comment", true, Directive{{"source", "a#b"}}},
	} {
		got, ok := Parse(tc.comment)
		if ok != tc.ok || !slices.Equal(got, tc.want) {
			t.Errorf("Parse(%q) = %v, %t; want %v, %t", tc.comment, got, ok, tc.want, tc.ok)
		}
	}
	d, _ := Parse("# shellcheck disable=SC1 disable=SC2 shell=sh")
	if values := d.Values("disable"); !slices.Equal(values, []string{"SC1", "SC2"}) {
		t.Errorf("Values = %v", values)
	}
	if shell, ok := d.Value("shell"); !ok || shell != "sh" {
		t.Errorf("Value(shell) = %q, %t", shell, ok)
	}
}
