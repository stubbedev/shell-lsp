package shelloptions

import "testing"

func TestSetCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flags []string
		want  string
	}{
		{flags: []string{"-euo", "pipefail", "-c"}, want: "set -eu -o pipefail\n"},
		{flags: []string{"-e", "-o", "pipefail", "-c"}, want: "set -e -o pipefail\n"},
		{flags: []string{"-ec"}, want: "set -e\n"},
		{flags: []string{"-c"}, want: ""},
		{flags: nil, want: ""},
		{flags: []string{"--login", "-e", "-c"}, want: "set -e\n"},
		{flags: []string{"-o"}, want: ""},
		{flags: []string{"-c", "script", "-e"}, want: ""},
		{flags: []string{"-e", "--", "-u"}, want: "set -e\n"},
	}
	for _, tt := range tests {
		if got := SetCommand(tt.flags); got != tt.want {
			t.Errorf("SetCommand(%q) = %q, want %q", tt.flags, got, tt.want)
		}
	}
}
