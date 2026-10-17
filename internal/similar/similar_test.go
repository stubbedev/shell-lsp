package similar

import (
	"slices"
	"testing"
)

func TestDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, candidate string
		distance        int
		close           bool
	}{
		{"LOGLEVEL", "LOG_LEVEL", 1, true},
		{"RETRY_COUNT", "Retry_Count", 1, true},
		{"DATABSE_URI", "DATABASE_URL", 2, true},
		{"ABC", "ABD", 0, false},
		{"HOST", "PORT", 0, false},
	}
	for _, tc := range tests {
		distance, ok := Distance(tc.name, tc.candidate)
		if distance != tc.distance || ok != tc.close {
			t.Errorf("Distance(%q, %q) = %d, %t; want %d, %t", tc.name, tc.candidate, distance, ok, tc.distance, tc.close)
		}
	}
}

func TestClosest(t *testing.T) {
	t.Parallel()

	if got, ok := Closest("LOGLEVEL", slices.Values([]string{"HOST", "LOG_LEVEL", "LOG_LEVELS"})); !ok || got != "LOG_LEVEL" {
		t.Errorf("Closest = %q, %v", got, ok)
	}
	if _, ok := Closest("HOST", slices.Values([]string{"PORT"})); ok {
		t.Error("Closest found a name too far away")
	}
}
