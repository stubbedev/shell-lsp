package main

import (
	"strings"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	t.Parallel()

	tail := " S 1 77 77 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 0 1000 42 18446744073709551615"
	tests := []struct {
		name, text string
		want       procStat
		wantErr    bool
	}{
		{"plain", "77 (nixd)" + tail, procStat{group: 77, rssPages: 42}, false},
		{"a command name with spaces and brackets", "77 (a) b (c)" + tail, procStat{group: 77, rssPages: 42}, false},
		{"no command name", "77 nixd" + tail, procStat{}, true},
		{"cut short", "77 (nixd) S 1 77", procStat{}, true},
		{"bad group", "77 (nixd)" + strings.Replace(tail, " 77 77 ", " x 77 ", 1), procStat{}, true},
	}
	for _, tt := range tests {
		got, err := parseProcStat(tt.text)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("%s: parseProcStat = %+v, %v; want %+v, error %v", tt.name, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestParsePeakRSS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, text string
		want       int64
		wantErr    bool
	}{
		{"present", "Name:\tnixd\nVmHWM:\t    2048 kB\nVmRSS:\t 1 kB\n", 2048 * kibibyte, false},
		{"a zombie has none", "Name:\tnixd\nState:\tZ\n", 0, false},
		{"gone", "", 0, false},
		{"another unit", "VmHWM:\t 2 MB\n", 0, true},
		{"not a number", "VmHWM:\t x kB\n", 0, true},
	}
	for _, tt := range tests {
		got, err := parsePeakRSS(tt.text)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("%s: parsePeakRSS = %d, %v; want %d, error %v", tt.name, got, err, tt.want, tt.wantErr)
		}
	}
}
