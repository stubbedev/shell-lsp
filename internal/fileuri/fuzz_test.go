package fileuri

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

func wellFormed(flavour platform.Flavour, path string) bool {
	if flavour == platform.POSIX || platform.HasDrive(path) {
		return flavour.IsAbs(path)
	}
	rest, unc := strings.CutPrefix(strings.ReplaceAll(path, `\`, "/"), "//")
	host, share, _ := strings.Cut(rest, "/")
	share, _, _ = strings.Cut(share, "/")
	return unc && host != "" && share != ""
}

func FuzzRoundTrip(f *testing.F) {
	for _, seed := range []string{"/home/u/a.sh", "/x/100%.sh", `C:\Users\a.sh`, `\\server\share\a.sh`, "C:", `\\`, "//", "/a/../../b", "c:a", `\\?\C:\x`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, path string) {
		if !utf8.ValidString(path) {
			return
		}
		for _, flavour := range []platform.Flavour{platform.POSIX, platform.Windows} {
			uri := fromPath(flavour, path)
			if len(uri) > 3*len(path)+len("file:////") {
				t.Fatalf("%s: URI %q of %q is out of proportion to the path", flavour, uri, path)
			}
			got, err := toPath(flavour, uri)
			if err == nil && wellFormed(flavour, path) && got != flavour.Clean(path) {
				t.Fatalf("%s: %q -> %q -> %q, want %q", flavour, path, uri, got, flavour.Clean(path))
			}
			if k := key(flavour, uri); err == nil && wellFormed(flavour, got) && key(flavour, k) != k {
				t.Fatalf("%s: key(%q) = %q is not stable: %q", flavour, uri, k, key(flavour, k))
			}
		}
	})
}

func FuzzToPath(f *testing.F) {
	for _, seed := range []string{"file:///a", "file://host/share/x", "file:///C:/x", "file:///c%3A/x", "http://x", "file://", "%zz"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, uri string) {
		for _, flavour := range []platform.Flavour{platform.POSIX, platform.Windows} {
			path, err := toPath(flavour, uri)
			if err != nil {
				continue
			}
			if len(path) > len(uri)+len(`\\`) {
				t.Fatalf("%s: path %q of %q is longer than the URI", flavour, path, uri)
			}
			if flavour.Clean(path) != path {
				t.Fatalf("%s: path %q of %q is not clean", flavour, path, uri)
			}
		}
	})
}
