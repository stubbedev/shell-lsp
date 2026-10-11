package platform

import (
	"path/filepath"
	"testing"
)

func TestRelativeWithin(t *testing.T) {
	t.Parallel()

	dir := filepath.FromSlash("/proj")
	tests := []struct {
		path string
		want string
		ok   bool
	}{
		{"/proj", ".", true},
		{"/proj/lib/a.sh", "lib/a.sh", true},
		{"/proj/..x.sh", "..x.sh", true},
		{"/other/a.sh", "", false},
		{"/a.sh", "", false},
	}
	for _, tc := range tests {
		got, ok := RelativeWithin(dir, filepath.FromSlash(tc.path))
		if got != filepath.FromSlash(tc.want) || ok != tc.ok {
			t.Errorf("RelativeWithin(%q, %q) = %q, %v; want %q, %v", dir, tc.path, got, ok, tc.want, tc.ok)
		}
	}
}
