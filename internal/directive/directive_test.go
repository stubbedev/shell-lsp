package directive

import (
	"reflect"
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
		{"# shellcheck disable=SC2086,SC2034 source=lib.sh", true, Directive{Pairs: []Pair{
			{Key: "disable", Value: "SC2086,SC2034", KeyOffset: 13, ValueOffset: 21, ValueEnd: 34},
			{Key: "source", Value: "lib.sh", KeyOffset: 35, ValueOffset: 42, ValueEnd: 48},
		}}},
		{"#shellcheck shell=dash", true, Directive{Pairs: []Pair{{Key: "shell", Value: "dash", KeyOffset: 12, ValueOffset: 18, ValueEnd: 22}}}},
		{"  # shellcheck disable=2086 # quoting is intended", true, Directive{Pairs: []Pair{{Key: "disable", Value: "2086", KeyOffset: 15, ValueOffset: 23, ValueEnd: 27}}}},
		{`# shellcheck source="./shared lib.sh" shell='sh'`, true, Directive{Pairs: []Pair{
			{Key: "source", Value: "./shared lib.sh", KeyOffset: 13, ValueOffset: 20, ValueEnd: 37},
			{Key: "shell", Value: "sh", KeyOffset: 38, ValueOffset: 44, ValueEnd: 48},
		}}},
		{`# shellcheck shell='busybox sh' disable="SC1,SC2"`, true, Directive{Pairs: []Pair{
			{Key: "shell", Value: "busybox sh", KeyOffset: 13, ValueOffset: 19, ValueEnd: 31},
			{Key: "disable", Value: "SC1,SC2", KeyOffset: 32, ValueOffset: 40, ValueEnd: 49},
		}}},
		{"# shellcheck source=a'#'b #comment", true, Directive{Pairs: []Pair{{Key: "source", Value: "a#b", KeyOffset: 13, ValueOffset: 20, ValueEnd: 25}}}},
		{"# shellcheck disable=SC2086 42", true, Directive{
			Pairs:   []Pair{{Key: "disable", Value: "SC2086", KeyOffset: 13, ValueOffset: 21, ValueEnd: 27}},
			Problem: &Problem{Kind: LeftoverText, Offset: 28},
		}},
		{"# shellcheck please ignore this", false, Directive{}},
		{"# shellcheck 42", false, Directive{}},
		{"# shellcheck disable SC2086", true, Directive{Problem: &Problem{Kind: MissingEquals, Offset: 13}}},
		{"# shellcheck disable=SC1 please", true, Directive{
			Pairs:   []Pair{{Key: "disable", Value: "SC1", KeyOffset: 13, ValueOffset: 21, ValueEnd: 24}},
			Problem: &Problem{Kind: MissingEquals, Offset: 25},
		}},
		{`# shellcheck source="unterminated`, true, Directive{Problem: &Problem{Kind: MalformedQuote, Offset: 20}}},
		{"# shellcheck", false, Directive{}},
		{"# shellcheckx disable=SC2086", false, Directive{}},
		{"# ShellCheck disable=SC2086", false, Directive{}},
		{"# not a directive", false, Directive{}},
		{"echo # shellcheck disable=SC2086", false, Directive{}},
	} {
		got, ok := Parse(tc.comment)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Parse(%q) = %+v, %t; want %+v, %t", tc.comment, got, ok, tc.want, tc.ok)
		}
	}
	d, _ := Parse("# shellcheck disable=SC1 disable=SC2 shell=sh")
	if values := d.Values(Disable); !slices.Equal(values, []string{"SC1", "SC2"}) {
		t.Errorf("Values = %v", values)
	}
	if shell, ok := d.Value(Shell); !ok || shell != "sh" {
		t.Errorf("Value(shell) = %q, %t", shell, ok)
	}
}

func TestKnown(t *testing.T) {
	t.Parallel()

	d, _ := Parse("# shellcheck severity=error disable=SC2086")
	if d.Pairs[0].Known() || !d.Pairs[1].Known() {
		t.Errorf("Known() = %t, %t; want false, true", d.Pairs[0].Known(), d.Pairs[1].Known())
	}
}
