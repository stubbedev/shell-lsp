package shelldocs

import (
	"reflect"
	"strings"
	"testing"
)

func TestArgumentSlot(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		line string
		want Slot
	}{
		{"[ -f", OperatorSlot{Vocabularies: []Vocabulary{TestUnaryOperators}}},
		{"[ a =", OperatorSlot{Vocabularies: []Vocabulary{TestBinaryOperators}}},
		{"[[ a ==", OperatorSlot{Vocabularies: []Vocabulary{TestBinaryOperators, CondOperators}}},
		{"[[ -v name", VariableSlot{}},
		{"[[ -o pipefail", OptionSlot{Vocabulary: SetOptions}},
		{"[ -f x ] y", FinishedSlot{}},
		{"set -o pipefail", OptionSlot{Vocabulary: SetOptions}},
		{"shopt -s nullglob", OptionSlot{Vocabulary: ShoptOptions}},
		{"shopt -so pipefail", OptionSlot{Vocabulary: SetOptions}},
		{"setopt extended_glob", OptionSlot{Vocabulary: SetoptOptions}},
		{"emulate zsh", ValueSlot{Vocabulary: EmulateModes}},
		{"kill -s TERM", SignalSlot{Vocabularies: []Vocabulary{Signals}}},
		{"kill -TERM", SignalSlot{Dashed: true, Vocabularies: []Vocabulary{Signals}}},
		{"trap cleanup EXIT", SignalSlot{Vocabularies: []Vocabulary{TrapConditions, Signals}}},
		{"printf %s", FormatSlot{}},
		{"printf -v out %s", FormatSlot{}},
		{"printf %s x", OperandSlot{}},
		{"grep -E", FlagSlot{}},
		{"unset name", VariableSlot{}},
		{"type git", CommandSlot{}},
		{"type ./git", OperandSlot{}},
		{"cd dir", DirectorySlot{}},
		{"git status", OperandSlot{}},
	} {
		words := strings.Fields(tc.line)
		n := len(words)
		if got := ArgumentSlot(words[0], words[1:n-1], words[n-1]); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ArgumentSlot(%q) = %#v, want %#v", tc.line, got, tc.want)
		}
	}
}

func TestNormalizeSignal(t *testing.T) {
	t.Parallel()

	for word, want := range map[string]struct {
		name string
		sig  bool
	}{
		"term": {"TERM", false}, "SIGterm": {"TERM", true}, "0": {"EXIT", false}, "INT": {"INT", false},
	} {
		if name, sig := NormalizeSignal(word); name != want.name || sig != want.sig {
			t.Errorf("NormalizeSignal(%q) = %q, %t; want %q, %t", word, name, sig, want.name, want.sig)
		}
	}
}
