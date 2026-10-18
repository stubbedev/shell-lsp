package scan

import (
	"slices"
	"testing"
)

func TestForwardVisitsEveryStep(t *testing.T) {
	t.Parallel()

	var visited []int
	Forward(2, 9, func(at int) int {
		visited = append(visited, at)
		return at + at%3 + 1
	})
	if want := []int{2, 5, 8}; !slices.Equal(visited, want) {
		t.Errorf("visited %v, want %v", visited, want)
	}
}

func TestForwardRefusesAStepThatStays(t *testing.T) {
	t.Parallel()

	for _, step := range []func(int) int{func(at int) int { return at }, func(at int) int { return at - 1 }} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("a step that does not advance was accepted")
				}
			}()
			Forward(0, 10, step)
		}()
	}
}

func TestContinuedCountsTrailingBackslashes(t *testing.T) {
	t.Parallel()

	for line, want := range map[string]bool{
		``:           false,
		`echo`:       false,
		`echo \`:     true,
		`echo \\`:    false,
		`echo \\\`:   true,
		`\`:          true,
		`echo \ x`:   false,
		`echo \\\\\`: true,
	} {
		if got := Continued(line); got != want {
			t.Errorf("Continued(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestWhileStopsAtTheFirstByteOutsideTheClass(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		s    string
		want int
	}{
		{"", 0},
		{"x1", 0},
		{"12x3", 2},
		{"123", 3},
	} {
		if got := While(tt.s, Digit); got != tt.want {
			t.Errorf("While(%q, Digit) = %d, want %d", tt.s, got, tt.want)
		}
	}
	if !All("", Digit) || !All("09", Digit) || All("0x", Digit) {
		t.Error("All disagrees with While")
	}
	if !All("09afAF", HexDigit) || HexDigit('g') || HexDigit('G') || HexDigit('@') {
		t.Error("HexDigit admits the wrong bytes")
	}
	if !All("azAZ09", Alnum) || Alnum('@') || Alnum('[') || Alnum('`') || Alnum('{') || Letter('5') || !All("az09", LowerOrDigit) || LowerOrDigit('A') {
		t.Error("the letter classes admit the wrong bytes")
	}
	if got := TrimBlanks(" \t a b "); got != "a b " {
		t.Errorf("TrimBlanks = %q, want the leading spaces and tabs gone", got)
	}
}
