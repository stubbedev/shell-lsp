package platform

import (
	"strings"
	"testing"
)

func FuzzPaths(f *testing.F) {
	for _, seed := range []string{"/a/./b/../c", "", `c:\a\..\..`, `\\host\share\x`, `\\?\C:\x`, `\\.\pipe\x`, "C:", "NUL.txt", "/c/Users", `C:\a\CON .x`} {
		f.Add(seed, "child")
	}
	f.Fuzz(func(t *testing.T, p, child string) {
		for _, flavour := range []Flavour{POSIX, Windows} {
			clean := flavour.Clean(p)
			if flavour.Clean(clean) != clean {
				t.Fatalf("%s: Clean(%q) = %q is not stable: %q", flavour, p, clean, flavour.Clean(clean))
			}
			if len(clean) > len(p)+len(`\\.`) {
				t.Fatalf("%s: Clean(%q) = %q grew", flavour, p, clean)
			}
			if !flavour.Within(p, p) {
				t.Fatalf("%s: %q is not within itself", flavour, p)
			}
			rooted := flavour == POSIX && flavour.IsAbs(p) || flavour == Windows && HasDrive(p) && flavour.IsAbs(p)
			if rooted && child != "" && !strings.ContainsAny(child, `/\:.`) && !flavour.Within(flavour.Join(p, child), p) {
				t.Fatalf("%s: %q joined to %q is not within it", flavour, child, p)
			}
			flavour.IsDevicePath(p)
			flavour.FromMSYS(p)
		}
	})
}
