package platform

import (
	"io/fs"
	"slices"
	"testing"
)

func TestClean(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flavour Flavour
		in      string
		want    string
	}{
		{POSIX, "/a/./b/../c", "/a/c"},
		{POSIX, "", "."},
		{Windows, `c:\a\.\b\..\c`, `C:\a\c`},
		{Windows, "C:/a//b/", `C:\a\b`},
		{Windows, `.\A:`, `.\A:`},
		{Windows, `x\..\c:b`, `.\c:b`},
		{Windows, `C:\..\..`, `C:\`},
		{Windows, `C:`, `C:.`},
		{Windows, `\\host\share\a\..\..\b`, `\\host\share\b`},
		{Windows, `a\..\..\b`, `..\b`},
		{Windows, `\rooted\x`, `\rooted\x`},
	}
	for _, tc := range tests {
		if got := tc.flavour.Clean(tc.in); got != tc.want {
			t.Errorf("%s.Clean(%q) = %q, want %q", tc.flavour, tc.in, got, tc.want)
		}
	}
}

func TestWithin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flavour    Flavour
		path, root string
		want       bool
	}{
		{POSIX, "/home/u/a.sh", "/home/u", true},
		{POSIX, "/home/u", "/home/u", true},
		{POSIX, "/home/user/a.sh", "/home/u", false},
		{POSIX, "/home/U/a.sh", "/home/u", false},
		{POSIX, "/a.sh", "/", true},
		{POSIX, "/a/b/", "/a/b", true},
		{POSIX, "/a/b/../c/d.sh", "/a/b", false},
		{POSIX, "/a/./b/c.sh", "/a/b/", true},
		{POSIX, "/a", "/a/b", false},
		{POSIX, "rel/x.sh", "/a", false},
		{Windows, `C:\Users\U\a.sh`, `c:\users\u`, true},
		{Windows, `C:\Users\Uma\a.sh`, `C:\Users\U`, false},
		{Windows, `C:\a.sh`, `C:\`, true},
		{Windows, `D:\a.sh`, `C:\`, false},
		{Windows, `\\host\share\x\a.sh`, `\\HOST\share`, true},
	}
	for _, tc := range tests {
		if got := tc.flavour.Within(tc.path, tc.root); got != tc.want {
			t.Errorf("%s.Within(%q, %q) = %v, want %v", tc.flavour, tc.path, tc.root, got, tc.want)
		}
	}
}

func TestIsAbs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flavour Flavour
		in      string
		want    bool
	}{
		{POSIX, "/a", true},
		{POSIX, "a/b", false},
		{POSIX, `C:\a`, false},
		{Windows, `C:\a`, true},
		{Windows, "c:/a", true},
		{Windows, `C:a`, false},
		{Windows, `\\host\share`, true},
		{Windows, `\a`, false},
		{Windows, "a", false},
	}
	for _, tc := range tests {
		if got := tc.flavour.IsAbs(tc.in); got != tc.want {
			t.Errorf("%s.IsAbs(%q) = %v, want %v", tc.flavour, tc.in, got, tc.want)
		}
	}
}

func TestJoin(t *testing.T) {
	t.Parallel()

	if got := POSIX.Join("/proj", "test/../a.bats"); got != "/proj/a.bats" {
		t.Errorf("POSIX.Join = %q", got)
	}
	if got := Windows.Join(`C:\proj`, "test/a.bats"); got != `C:\proj\test\a.bats` {
		t.Errorf("Windows.Join = %q", got)
	}
}

func TestFromMSYS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flavour Flavour
		in      string
		want    string
	}{
		{Windows, "/c/Users/u/a.bats", `C:\Users\u\a.bats`},
		{Windows, "/d", `D:\`},
		{Windows, "/usr/bin/bats", "/usr/bin/bats"},
		{Windows, "test/a.bats", "test/a.bats"},
		{POSIX, "/c/Users/u/a.bats", "/c/Users/u/a.bats"},
	}
	for _, tc := range tests {
		if got := tc.flavour.FromMSYS(tc.in); got != tc.want {
			t.Errorf("%s.FromMSYS(%q) = %q, want %q", tc.flavour, tc.in, got, tc.want)
		}
	}
}

func TestExecutable(t *testing.T) {
	t.Parallel()

	pathExt := Windows.PathExt(".COM;.EXE;;.Bat")
	tests := []struct {
		flavour Flavour
		name    string
		mode    fs.FileMode
		want    string
		ok      bool
	}{
		{POSIX, "ls", 0o755, "ls", true},
		{POSIX, "notes", 0o644, "", false},
		{POSIX, "dir", fs.ModeDir | 0o755, "", false},
		{Windows, "git.exe", 0o644, "git", true},
		{Windows, "build.BAT", 0o644, "build", true},
		{Windows, "build.cmd", 0o644, "", false},
		{Windows, "readme.txt", 0o644, "", false},
		{Windows, "bats", 0o755, "", false},
		{Windows, "tools.exe", fs.ModeDir, "", false},
	}
	for _, tc := range tests {
		got, ok := tc.flavour.Executable(tc.name, tc.mode, pathExt)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s.Executable(%q, %v) = %q, %v; want %q, %v", tc.flavour, tc.name, tc.mode, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMayExecute(t *testing.T) {
	t.Parallel()

	if POSIX.MayExecute(0o644) || !POSIX.MayExecute(0o700) || POSIX.MayExecute(fs.ModeDir|0o755) {
		t.Error("POSIX.MayExecute does not follow the execute bits of regular files")
	}
	if !Windows.MayExecute(0o644) || Windows.MayExecute(fs.ModeDir|0o755) {
		t.Error("Windows.MayExecute does not accept every regular file")
	}
}

func TestPathExt(t *testing.T) {
	t.Parallel()

	if got := Windows.PathExt(""); !slices.Equal(got, []string{".COM", ".EXE", ".BAT", ".CMD"}) {
		t.Errorf("Windows.PathExt(\"\") = %q, want the default list", got)
	}
	if got := POSIX.PathExt(".EXE"); got != nil {
		t.Errorf("POSIX.PathExt = %q, want nil", got)
	}
}
