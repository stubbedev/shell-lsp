package shelldocs

import "strings"

type Slot interface{ slot() }

type (
	OperandSlot   struct{}
	FinishedSlot  struct{}
	FlagSlot      struct{}
	VariableSlot  struct{}
	CommandSlot   struct{}
	DirectorySlot struct{}
	FormatSlot    struct{}
	OperatorSlot  struct{ Vocabularies []Vocabulary }
	OptionSlot    struct{ Vocabulary Vocabulary }
	ValueSlot     struct{ Vocabulary Vocabulary }
	SignalSlot    struct {
		Dashed       bool
		Vocabularies []Vocabulary
	}
)

func (OperandSlot) slot()   {}
func (FinishedSlot) slot()  {}
func (FlagSlot) slot()      {}
func (VariableSlot) slot()  {}
func (CommandSlot) slot()   {}
func (DirectorySlot) slot() {}
func (FormatSlot) slot()    {}
func (OperatorSlot) slot()  {}
func (OptionSlot) slot()    {}
func (ValueSlot) slot()     {}
func (SignalSlot) slot()    {}

var commandTakers = map[string]bool{
	"type": true, "which": true, "whence": true, "where": true, "man": true, "run-help": true, "noglob": true,
}

func ArgumentSlot(command string, before []string, word string) Slot {
	last := ""
	if n := len(before); n > 0 {
		last = before[n-1]
	}
	dashed := strings.HasPrefix(word, "-")
	if command == "[[" || command == "[" || command == "test" {
		if slot, ok := testSlot(command, before, dashed); ok {
			return slot
		}
	}
	switch {
	case command == "set" && (last == "-o" || last == "+o"):
		return OptionSlot{Vocabulary: SetOptions}
	case command == "kill" && last == "-s":
		return SignalSlot{Vocabularies: []Vocabulary{Signals}}
	case command == "kill" && dashed:
		return SignalSlot{Dashed: true, Vocabularies: []Vocabulary{Signals}}
	case command == "trap" && len(before) > 0 && !dashed:
		return SignalSlot{Vocabularies: []Vocabulary{TrapConditions, Signals}}
	case command == "printf" && formatArgument(before):
		return FormatSlot{}
	case command == "emulate" && !dashed:
		return ValueSlot{Vocabulary: EmulateModes}
	case dashed:
		return FlagSlot{}
	case command == "shopt" && namesSetOptions(before):
		return OptionSlot{Vocabulary: SetOptions}
	case command == "shopt":
		return OptionSlot{Vocabulary: ShoptOptions}
	case command == "setopt" || command == "unsetopt":
		return OptionSlot{Vocabulary: SetoptOptions}
	case command == "unset" || command == "export" || command == "readonly":
		return VariableSlot{}
	case commandTakers[command] && !strings.Contains(word, "/"):
		return CommandSlot{}
	case command == "cd" || command == "pushd":
		return DirectorySlot{}
	}
	return OperandSlot{}
}

func NormalizeSignal(word string) (string, bool) {
	name, spelledSIG := strings.CutPrefix(strings.ToUpper(word), "SIG")
	if name == "0" {
		name = "EXIT"
	}
	return name, spelledSIG
}

func namesSetOptions(before []string) bool {
	for _, w := range before {
		if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.ContainsRune(w[1:], 'o') {
			return true
		}
	}
	return false
}

func formatArgument(before []string) bool {
	for len(before) > 0 {
		switch before[0] {
		case "--":
			return len(before) == 1
		case "-v":
			if len(before) < 2 {
				return false
			}
			before = before[2:]
		default:
			return false
		}
	}
	return true
}

type testState uint8

const (
	testStart testState = iota
	testAfterUnary
	testAfterOperand
	testAfterBinary
	testDone
)

func testSlot(command string, before []string, dashed bool) (Slot, bool) {
	state, last := testPosition(command, before)
	switch state {
	case testStart:
		if !dashed {
			return nil, false
		}
		return OperatorSlot{Vocabularies: []Vocabulary{TestUnaryOperators}}, true
	case testAfterOperand:
		if command == "[[" {
			return OperatorSlot{Vocabularies: []Vocabulary{TestBinaryOperators, CondOperators}}, true
		}
		return OperatorSlot{Vocabularies: []Vocabulary{TestBinaryOperators}}, true
	case testAfterUnary:
		switch {
		case last == "-v" || last == "-R":
			return VariableSlot{}, true
		case last == "-o" && command == "[[":
			return OptionSlot{Vocabulary: SetOptions}, true
		}
		return nil, false
	case testAfterBinary:
		return nil, false
	case testDone:
		return FinishedSlot{}, true
	}
	panic("shelldocs: unknown test expression state")
}

func testPosition(command string, before []string) (testState, string) {
	connector := func(w string) bool {
		if command == "[[" {
			return w == "&&" || w == "||"
		}
		return w == "-a" || w == "-o"
	}
	state, last := testStart, ""
	for _, w := range before {
		last = w
		switch {
		case w == "]]" || w == "]" || w == ")":
			state = testDone
			continue
		case state == testStart && (w == "!" || w == "("):
			continue
		}
		switch state {
		case testStart:
			state = testAfterOperand
			if known(TestUnaryOperators, w) {
				state = testAfterUnary
			}
		case testAfterOperand:
			switch {
			case connector(w):
				state = testStart
			case isTestBinary(w):
				state = testAfterBinary
			default:
				state = testDone
			}
		case testAfterUnary, testAfterBinary:
			state = testDone
		case testDone:
			if connector(w) {
				state = testStart
			}
		}
	}
	return state, last
}

func isTestBinary(w string) bool {
	return known(TestBinaryOperators, w) || (known(CondOperators, w) && w != "&&" && w != "||")
}
