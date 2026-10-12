package lint

import "testing"

func TestArithmeticNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in    string
		value int64
		ok    bool
	}{
		{in: "42", value: 42, ok: true},
		{in: "0", value: 0, ok: true},
		{in: "017", value: 15, ok: true},
		{in: "08", ok: false},
		{in: "0xff", value: 255, ok: true},
		{in: "0XdFfF", value: 0xdfff, ok: true},
		{in: "0x", ok: false},
		{in: "2#1010", value: 10, ok: true},
		{in: "36#Zz", value: 35*36 + 35, ok: true},
		{in: "64#A", value: 36, ok: true},
		{in: "64#@_", value: 62*64 + 63, ok: true},
		{in: "8#9", ok: false},
		{in: "65#1", ok: false},
		{in: "1#0", ok: false},
		{in: "9223372036854775807", value: 9223372036854775807, ok: true},
		{in: "9223372036854775808", ok: false},
		{in: "cp", ok: false},
		{in: "", ok: false},
	}
	for _, tc := range tests {
		value, ok := arithmeticNumber(tc.in)
		if value != tc.value || ok != tc.ok {
			t.Errorf("arithmeticNumber(%q) = %d, %t; want %d, %t", tc.in, value, ok, tc.value, tc.ok)
		}
	}
}
