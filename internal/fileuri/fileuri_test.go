package fileuri

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	cases := map[platform.Flavour][]string{
		platform.POSIX:   {"/home/u/a.sh", "/tmp/with space/é.sh", "/x/100%.sh"},
		platform.Windows: {`C:\Users\u\a.sh`, `D:\with space\é.sh`, `C:\x\100%.sh`, `\\server\share\dir\a.sh`, `C:\`},
	}
	for f, paths := range cases {
		for _, path := range paths {
			uri := fromPath(f, path)
			got, err := toPath(f, uri)
			if err != nil {
				t.Fatalf("%s: toPath(%q) error = %v", f, uri, err)
			}
			if got != path {
				t.Errorf("%s: round trip %q -> %q -> %q", f, path, uri, got)
			}
		}
	}
}

func TestToPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flavour platform.Flavour
		uri     string
		want    string
	}{
		{platform.POSIX, "file:///tmp/a%20b.sh", "/tmp/a b.sh"},
		{platform.POSIX, "file:///tmp/./x//y/../a.sh", "/tmp/x/a.sh"},
		{platform.POSIX, "file://localhost/tmp/a.sh", "/tmp/a.sh"},
		{platform.Windows, "file:///C:/Users/u/a.sh", `C:\Users\u\a.sh`},
		{platform.Windows, "file:///c%3A/Users/u/a%20b.sh", `C:\Users\u\a b.sh`},
		{platform.Windows, "file:///c:/x/./y/../a.sh", `C:\x\a.sh`},
		{platform.Windows, "file:///C:/a/../../b.sh", `C:\b.sh`},
		{platform.Windows, "file://server/share/dir/a.sh", `\\server\share\dir\a.sh`},
		{platform.Windows, "file://server/share/../a.sh", `\\server\share\a.sh`},
		{platform.Windows, "file:///C:/x%5Cy.sh", `C:\x\y.sh`},
	}
	for _, tc := range tests {
		got, err := toPath(tc.flavour, tc.uri)
		if err != nil || got != tc.want {
			t.Errorf("%s: toPath(%q) = %q, %v; want %q", tc.flavour, tc.uri, got, err, tc.want)
		}
	}
}

func TestToPathRejects(t *testing.T) {
	t.Parallel()

	for _, f := range []platform.Flavour{platform.POSIX, platform.Windows} {
		for _, uri := range []string{"untitled:Untitled-1", "%zz"} {
			if _, err := toPath(f, uri); err == nil {
				t.Errorf("%s: toPath(%q) succeeded, want error", f, uri)
			}
		}
	}
	if _, err := toPath(platform.POSIX, "file://remote/x.sh"); err == nil {
		t.Error("posix: toPath of a remote host succeeded, want error")
	}
}

func TestFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flavour platform.Flavour
		path    string
		want    string
	}{
		{platform.POSIX, "/tmp/a b.sh", "file:///tmp/a%20b.sh"},
		{platform.Windows, `C:\Users\u\a b.sh`, "file:///C:/Users/u/a%20b.sh"},
		{platform.Windows, `\\server\share\a.sh`, "file://server/share/a.sh"},
	}
	for _, tc := range tests {
		if got := fromPath(tc.flavour, tc.path); got != tc.want {
			t.Errorf("%s: fromPath(%q) = %q, want %q", tc.flavour, tc.path, got, tc.want)
		}
	}
}

func TestKeyIdentifiesTheSameFile(t *testing.T) {
	t.Parallel()

	same := []struct {
		flavour platform.Flavour
		a, b    string
	}{
		{platform.POSIX, "file:///home/u/a+b@c.sh", "file:///home/u/a%2Bb%40c.sh"},
		{platform.POSIX, "file:///home/u/./a.sh", "file:///home/u/a.sh"},
		{platform.Windows, "file:///c%3A/Users/U/A.sh", "file:///C:/users/u/a.sh"},
		{platform.Windows, "file://Server/Share/a.sh", "file://server/share/A.SH"},
	}
	for _, tc := range same {
		if ka, kb := key(tc.flavour, tc.a), key(tc.flavour, tc.b); ka != kb {
			t.Errorf("%s: key(%q) = %q, key(%q) = %q; want equal", tc.flavour, tc.a, ka, tc.b, kb)
		}
	}
	if key(platform.POSIX, "file:///home/u/A.sh") == key(platform.POSIX, "file:///home/u/a.sh") {
		t.Error("posix keys fold case, want case-sensitive")
	}
	if got := key(platform.POSIX, "untitled:Untitled-1"); got != "untitled:Untitled-1" {
		t.Errorf("key(untitled) = %q, want it unchanged", got)
	}
}
