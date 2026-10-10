package fileuri

import "testing"

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/home/u/a.sh", "/tmp/with space/é.sh", "/x/100%.sh"} {
		uri := FromPath(path)
		got, err := ToPath(uri)
		if err != nil {
			t.Fatalf("ToPath(%q) error = %v", uri, err)
		}
		if got != path {
			t.Errorf("round trip %q -> %q -> %q", path, uri, got)
		}
	}
}

func TestToPathDecodesAndRejects(t *testing.T) {
	t.Parallel()

	if got, err := ToPath("file:///tmp/a%20b.sh"); err != nil || got != "/tmp/a b.sh" {
		t.Errorf("ToPath(escaped) = %q, %v", got, err)
	}
	if got, err := ToPath("file:///tmp/./x//y/../a.sh"); err != nil || got != "/tmp/x/a.sh" {
		t.Errorf("ToPath(uncanonical) = %q, %v; want the cleaned path", got, err)
	}
	for _, uri := range []string{"untitled:Untitled-1", "file://remote/x.sh", "%zz"} {
		if _, err := ToPath(uri); err == nil {
			t.Errorf("ToPath(%q) succeeded, want error", uri)
		}
	}
}
