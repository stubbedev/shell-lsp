package lang

import "testing"

type testKind uint8

const (
	kAssign testKind = iota + 1
	kEqual
	kNot
	kNotEqual
	kParen
)

func TestOperatorsTakeTheLongestMatch(t *testing.T) {
	t.Parallel()

	ops := NewOperators(map[string]testKind{"=": kAssign, "==": kEqual, "!": kNot, "!=": kNotEqual}, map[string]testKind{"(": kParen})
	for _, tt := range []struct {
		src      string
		kind     testKind
		end      int
		semantic bool
	}{
		{"==x", kEqual, 2, true},
		{"=x", kAssign, 1, true},
		{"!=", kNotEqual, 2, true},
		{"!x", kNot, 1, true},
		{"((", kParen, 1, false},
	} {
		var r Result
		l, ok := ops.Scan(&r, tt.src, 0)
		if !ok || l.Kind != tt.kind || l.Start != 0 || l.End != tt.end || l.Sem.ok != tt.semantic || (len(r.Tokens) == 1) != tt.semantic {
			t.Errorf("Scan(%q) = %+v, %v with tokens %v", tt.src, l, ok, r.Tokens)
		}
	}
	var r Result
	if l, ok := ops.Scan(&r, "x", 0); ok {
		t.Errorf("Scan(x) = %+v", l)
	}
}

func TestOperatorsRefuseAnAmbiguousTable(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("an operator that is also punctuation was accepted")
		}
	}()
	NewOperators(map[string]testKind{"(": kParen}, map[string]testKind{"(": kParen})
}

func TestReclassifyRewritesOnlyEmittedTokens(t *testing.T) {
	t.Parallel()

	var r Result
	e := r.Emit(0, 2, Variable, 0)
	r.Reclassify(e, Parameter, Declaration)
	r.Reclassify(Emitted{}, Keyword, 0)
	if len(r.Tokens) != 1 || r.Tokens[0].Kind != Parameter || r.Tokens[0].Modifiers != Declaration {
		t.Errorf("tokens = %+v", r.Tokens)
	}
}

func TestScanners(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		scan func(string, int) int
		src  string
		end  int
	}{
		{"integer", ScanNumber, "42+", 2},
		{"fraction", ScanNumber, "4.5x", 3},
		{"leading dot", ScanNumber, ".5", 2},
		{"exponent", ScanNumber, "1e-3)", 4},
		{"not an exponent", ScanNumber, "1e+", 1},
		{"name", ScanName, "a_1-b", 3},
		{"qualified name", ScanQualifiedName, "lib::f::g(", 9},
		{"trailing colons", ScanQualifiedName, "lib::", 3},
		{"colons before a digit", ScanQualifiedName, "lib::1", 3},
	} {
		if got := tt.scan(tt.src, 0); got != tt.end {
			t.Errorf("%s: scanning %q ends at %d, want %d", tt.name, tt.src, got, tt.end)
		}
	}
	for src, want := range map[string]bool{"1": true, ".5": true, ".": false, ".x": false, "x": false} {
		if got := StartsNumber(src, 0); got != want {
			t.Errorf("StartsNumber(%q) = %v, want %v", src, got, want)
		}
	}
}
