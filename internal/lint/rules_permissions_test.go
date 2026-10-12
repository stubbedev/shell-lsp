package lint

import "testing"

func TestSensitivePathsAreDescribedForWhatTheyAre(t *testing.T) {
	t.Parallel()

	for shape, want := range map[string]string{
		"/":                 "the root directory",
		"/etc":              "a system directory",
		"/usr":              "a system directory",
		"/etc/sudoers":      "part of a system directory",
		"/usr/local/bin":    "part of a system directory",
		"/home/ann":         "a home directory",
		"~":                 "a home directory",
		"~/.bashrc":         "a shell startup file",
		"~/.ssh":            "a directory of private keys",
		"~/.ssh/config":     "part of a directory of private keys",
		"~/.ssh/id_rsa":     "an SSH private key",
		"x/authorized_keys": "the list of keys that may log in over SSH",
	} {
		if got, ok := sensitivePath(shape); !ok || got != want {
			t.Errorf("sensitivePath(%q) = %q, %t; want %q", shape, got, ok, want)
		}
	}
	if got, ok := sensitivePath("build/out"); ok {
		t.Errorf("sensitivePath(build/out) = %q, want no sensitive path", got)
	}
}
