package lint

import "testing"

func TestEscapedNumberTruncatesToAByte(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in          string
		base, width int
		code        byte
		consumed    int
	}{
		{"101", 8, 3, 'A', 3},
		{"777", 8, 3, 0xff, 3},
		{"4142", 16, 2, 'A', 2},
		{"9", 8, 3, 0, 0},
		{"7x", 8, 3, 7, 1},
	} {
		code, consumed := escapedNumber(tc.in, tc.base, tc.width)
		if code != tc.code || consumed != tc.consumed {
			t.Errorf("escapedNumber(%q, %d, %d) = %d, %d; want %d, %d", tc.in, tc.base, tc.width, code, consumed, tc.code, tc.consumed)
		}
	}
}
