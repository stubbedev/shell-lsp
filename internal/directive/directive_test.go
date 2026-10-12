package directive

import (
	"reflect"
	"slices"
	"strings"
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
			{Name: "disable", Value: "SC2086,SC2034", KeyOffset: 13, ValueOffset: 21, ValueEnd: 34},
			{Name: "source", Value: "lib.sh", KeyOffset: 35, ValueOffset: 42, ValueEnd: 48},
		}}},
		{"#shellcheck shell=dash", true, Directive{Pairs: []Pair{{Name: "shell", Value: "dash", KeyOffset: 12, ValueOffset: 18, ValueEnd: 22}}}},
		{"  # shellcheck disable=2086 # quoting is intended", true, Directive{Pairs: []Pair{{Name: "disable", Value: "2086", KeyOffset: 15, ValueOffset: 23, ValueEnd: 27}}}},
		{`# shellcheck source="./shared lib.sh" shell='sh'`, true, Directive{Pairs: []Pair{
			{Name: "source", Value: "./shared lib.sh", KeyOffset: 13, ValueOffset: 20, ValueEnd: 37},
			{Name: "shell", Value: "sh", KeyOffset: 38, ValueOffset: 44, ValueEnd: 48},
		}}},
		{`# shellcheck shell='busybox sh' disable="SC1,SC2"`, true, Directive{Pairs: []Pair{
			{Name: "shell", Value: "busybox sh", KeyOffset: 13, ValueOffset: 19, ValueEnd: 31},
			{Name: "disable", Value: "SC1,SC2", KeyOffset: 32, ValueOffset: 40, ValueEnd: 49},
		}}},
		{"# shellcheck source=a'#'b #comment", true, Directive{Pairs: []Pair{{Name: "source", Value: "a#b", KeyOffset: 13, ValueOffset: 20, ValueEnd: 25}}}},
		{"# shellcheck disable=SC2086 42", true, Directive{
			Pairs:   []Pair{{Name: "disable", Value: "SC2086", KeyOffset: 13, ValueOffset: 21, ValueEnd: 27}},
			Problem: &Problem{Kind: LeftoverText, Offset: 28},
		}},
		{"# shellcheck please ignore this", false, Directive{}},
		{"# shellcheck 42", false, Directive{}},
		{"# shellcheck disable SC2086", true, Directive{Problem: &Problem{Kind: MissingEquals, Offset: 13}}},
		{"# shellcheck disable=SC1 please", true, Directive{
			Pairs:   []Pair{{Name: "disable", Value: "SC1", KeyOffset: 13, ValueOffset: 21, ValueEnd: 24}},
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

func TestParseSettings(t *testing.T) {
	t.Parallel()

	got, ok := ParseSettings("disable=SC2034,SC2086 enable=all # team default")
	want := Directive{Pairs: []Pair{
		{Name: "disable", Value: "SC2034,SC2086", KeyOffset: 0, ValueOffset: 8, ValueEnd: 21},
		{Name: "enable", Value: "all", KeyOffset: 22, ValueOffset: 29, ValueEnd: 32},
	}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSettings = %+v, %v; want %+v", got, ok, want)
	}
	if d, ok := ParseSettings("disable"); !ok || d.Problem == nil || d.Problem.Kind != MissingEquals {
		t.Errorf("a key without a value = %+v, %v; want a missing-equals problem", d, ok)
	}
	if _, ok := ParseSettings("not a setting"); ok {
		t.Error("free text parsed as a setting")
	}
}

func TestKnown(t *testing.T) {
	t.Parallel()

	d, _ := Parse("# shellcheck severity=error disable=SC2086")
	if d.Pairs[0].Known() || !d.Pairs[1].Known() {
		t.Errorf("Known() = %t, %t; want false, true", d.Pairs[0].Known(), d.Pairs[1].Known())
	}
	if key, ok := d.Pairs[1].Key(); !ok || key != Disable || !d.Pairs[1].Is(Disable) || d.Pairs[0].Is(Disable) {
		t.Errorf("Key() = %v, %t; want disable", key, ok)
	}
	if pair, ok := d.Pair(Disable); !ok || pair.Value != "SC2086" {
		t.Errorf("Pair(disable) = %+v, %t", pair, ok)
	}
	if _, ok := d.Pair(Shell); ok {
		t.Error("Pair(shell) found a pair that is not there")
	}
}

func TestKeySpecs(t *testing.T) {
	t.Parallel()

	for key := range Keys() {
		if named, ok := KeyNamed(key.String()); !ok || named != key || key.Doc() == "" {
			t.Errorf("key %s does not round-trip through its name or lacks a doc", key)
		}
		if key.List() != (key == Disable || key == Enable) {
			t.Errorf("%s.List() = %t", key, key.List())
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("the zero key has a name")
		}
	}()
	_ = Key(0).String()
}

func TestCursorAt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		comment string
		ok      bool
		want    Cursor
	}{
		{"# shellcheck |", true, Cursor{Slot: SlotKey, Start: 13, End: 13}},
		{"# shellcheck dis|", true, Cursor{Slot: SlotKey, Start: 13, End: 16}},
		{"# shellcheck di|sable=SC1", true, Cursor{Slot: SlotKey, Start: 13, End: 20}},
		{"# shellcheck disable=|", true, Cursor{Slot: SlotValue, Key: Disable, Start: 21, End: 21}},
		{"# shellcheck disable=SC20|", true, Cursor{Slot: SlotValue, Key: Disable, Start: 21, End: 25}},
		{"# shellcheck disable=SC1,SC2|,SC3", true, Cursor{Slot: SlotValue, Key: Disable, Start: 25, End: 28, Others: []string{"SC1", "SC3"}}},
		{"# shellcheck disable=SC1,|", true, Cursor{Slot: SlotValue, Key: Disable, Start: 25, End: 25, Others: []string{"SC1"}}},
		{"# shellcheck shell=ba|sh", true, Cursor{Slot: SlotValue, Key: Shell, Start: 19, End: 23}},
		{"# shellcheck shell=bash |", true, Cursor{Slot: SlotKey, Start: 24, End: 24}},
		{"# shellcheck shell=bash | disable=SC1", true, Cursor{Slot: SlotKey, Start: 24, End: 24}},
		{"# shellcheck source=\"a b|\"", false, Cursor{}},
		{"# shellcheck disable=SC1 # why|", false, Cursor{}},
		{"# shellcheck disable SC|", false, Cursor{}},
		{"# shellcheck|", false, Cursor{}},
		{"# shellcheck severity=er|", false, Cursor{}},
		{"# not a directive|", false, Cursor{}},
	} {
		at := strings.IndexByte(tc.comment, '|')
		comment := tc.comment[:at] + tc.comment[at+1:]
		got, ok := CursorAt(comment, at)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("CursorAt(%q) = %+v, %t; want %+v, %t", tc.comment, got, ok, tc.want, tc.ok)
		}
	}
}

func TestItems(t *testing.T) {
	t.Parallel()

	d, _ := Parse("# shellcheck disable=SC1,SC2000-SC2099,all")
	want := []Item{{Text: "SC1", Offset: 21, End: 24}, {Text: "SC2000-SC2099", Offset: 25, End: 38}, {Text: "all", Offset: 39, End: 42}}
	if got := Items(d.Pairs[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("Items = %+v, want %+v", got, want)
	}
	quoted, _ := Parse(`# shellcheck disable="SC1,SC2"`)
	if got := Items(quoted.Pairs[0]); got != nil {
		t.Errorf("Items of a quoted value = %+v, want none: offsets inside quotes are not the value's", got)
	}
}
