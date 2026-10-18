package editorconfig

import (
	"errors"
	"io/fs"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/filecache"
)

func TestPatterns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		glob, path string
		want       bool
	}{
		{"*", "/p/a.sh", true},
		{"*", "/p/sub/a.sh", true},
		{"*.sh", "/p/deep/er/a.sh", true},
		{"*.sh", "/p/a.bash", false},
		{"*.{sh,bash}", "/p/x/a.bash", true},
		{"*.{sh,bash}", "/p/x/a.zsh", false},
		{"lib/*.sh", "/p/lib/a.sh", true},
		{"lib/*.sh", "/p/x/lib/a.sh", false},
		{"lib/*.sh", "/p/lib/x/a.sh", false},
		{"/lib/**.sh", "/p/lib/x/y/a.sh", true},
		{"lib/**/a.sh", "/p/lib/a.sh", true},
		{"lib/**/a.sh", "/p/lib/x/y/a.sh", true},
		{"a?.sh", "/p/ab.sh", true},
		{"a?.sh", "/p/a/b.sh", false},
		{"[ab].sh", "/p/b.sh", true},
		{"[!ab].sh", "/p/b.sh", false},
		{"[!ab].sh", "/p/c.sh", true},
		{"file{1..3}.sh", "/p/file2.sh", true},
		{"file{1..3}.sh", "/p/file4.sh", false},
		{"file{3..1}.sh", "/p/file1.sh", true},
		{"{a,{b,c}}.sh", "/p/c.sh", true},
		{"{single}.sh", "/p/{single}.sh", true},
		{"[unclosed.sh", "/p/[unclosed.sh", true},
		{`\*.sh`, "/p/*.sh", true},
		{`\*.sh`, "/p/a.sh", false},
	} {
		p, err := compilePattern(tc.glob, "/p")
		if err != nil {
			t.Fatalf("compilePattern(%q) error = %v", tc.glob, err)
		}
		if got := p.matches(tc.path); got != tc.want {
			t.Errorf("%q matches %q = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
}

func TestNumericBrace(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		body string
		want numericRange
		ok   bool
	}{
		{"1..3", numericRange{low: 1, high: 3}, true},
		{"3..1", numericRange{low: 1, high: 3}, true},
		{"-2..+5", numericRange{low: -2, high: 5}, true},
		{"007..8", numericRange{low: 7, high: 8}, true},
		{"1...3", numericRange{}, false},
		{"1.2..3", numericRange{}, false},
		{"a..3", numericRange{}, false},
		{"1..", numericRange{}, false},
		{"..3", numericRange{}, false},
		{"+..3", numericRange{}, false},
		{"--1..3", numericRange{}, false},
		{"1..3x", numericRange{}, false},
		{" 1..3", numericRange{}, false},
		{"99999999999999999999..1", numericRange{}, false},
		{"1,2", numericRange{}, false},
	} {
		if got, ok := numericBrace(tt.body); got != tt.want || ok != tt.ok {
			t.Errorf("numericBrace(%q) = %+v, %t, want %+v, %t", tt.body, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseRejectsMalformedLines(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]byte("[*]\nindent_size 4\n"), "/p"); err == nil {
		t.Error("a line without = was accepted")
	}
	if _, err := Parse([]byte("[[z-a]]x\nk=v\n"), "/p"); err == nil {
		t.Error("an invalid character range was accepted")
	}
}

type disk map[string]string

func (d disk) read(reads *int) filecache.ReadFile {
	return func(path string) ([]byte, error) {
		*reads++
		text, ok := d[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(text), nil
	}
}

func TestResolverMergesNearestLast(t *testing.T) {
	t.Parallel()

	files := disk{
		filepath.Join("/", FileName):                  "root = true\n[*]\nindent_size = 8\nshell_variant = bash\n",
		filepath.Join("/repo", FileName):              "root = true\n[*]\nindent_style = space\nindent_size = 2\n[*.sh]\nswitch_case_indent = true\n[vendor/**]\nignore = true\n",
		filepath.Join("/repo/scripts", FileName):      "# comment\n; also a comment\n[*.sh]\nindent_size = 4\nswitch_case_indent = unset\n[[bash]]\nbinary_next_line = true\n",
		filepath.Join("/repo/scripts/sub", FileName):  "",
		filepath.Join("/repo/vendor/lib", "x.sh"):     "",
		filepath.Join("/elsewhere/deep", "unrelated"): "",
	}
	reads := 0
	r := NewResolver(files.read(&reads), time.Now, time.Second)
	got, err := r.Properties("/repo/scripts/sub/run.sh", []string{"shell", "bash"})
	if err != nil {
		t.Fatal(err)
	}
	want := Properties{"indent_style": "space", "indent_size": "4", "binary_next_line": "true"}
	if !maps.Equal(got, want) {
		t.Errorf("properties = %v, want %v", got, want)
	}
	if got, _ := r.Properties("/repo/scripts/run.sh", []string{"shell", "zsh"}); got["binary_next_line"] != "" {
		t.Errorf("a [[bash]] section applied to a zsh script: %v", got)
	}
	if got, _ := r.Properties("/repo/vendor/lib/x.sh", nil); got["ignore"] != "true" {
		t.Errorf("vendor ignore = %v", got)
	}
	if got, _ := r.Properties("/elsewhere/deep/unrelated", nil); !maps.Equal(got, Properties{"indent_size": "8", "shell_variant": "bash"}) {
		t.Errorf("properties outside the repo = %v, want the filesystem root's", got)
	}
}

func TestResolverCachesUntilRecheckOrInvalidate(t *testing.T) {
	t.Parallel()

	path := filepath.Join("/repo", FileName)
	files := disk{path: "root = true\n[*]\nindent_size = 2\n"}
	reads := 0
	now := time.Unix(1000, 0)
	r := NewResolver(files.read(&reads), func() time.Time { return now }, time.Second)
	if _, err := r.Properties("/repo/a.sh", nil); err != nil {
		t.Fatal(err)
	}
	files[path] = "root = true\n[*]\nindent_size = 6\n"
	if got, _ := r.Properties("/repo/b.sh", nil); got["indent_size"] != "2" || reads != 1 {
		t.Errorf("within the recheck window: %v after %d reads, want the cached file read once", got, reads)
	}
	r.Invalidate(path)
	if got, _ := r.Properties("/repo/b.sh", nil); got["indent_size"] != "6" {
		t.Errorf("after Invalidate = %v", got)
	}
	files[path] = "root = true\n[*]\nindent_size = 3\n"
	now = now.Add(time.Second)
	if got, _ := r.Properties("/repo/b.sh", nil); got["indent_size"] != "3" {
		t.Errorf("after the recheck window = %v", got)
	}
}

func TestResolverReadsABrokenFileOncePerRecheck(t *testing.T) {
	t.Parallel()

	path := filepath.Join("/repo", FileName)
	files := disk{path: "root = true\n[*]\nindent_size 2\n"}
	reads := 0
	r := NewResolver(files.read(&reads), time.Now, time.Hour)
	for range 3 {
		if _, err := r.Properties("/repo/a.sh", nil); err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("error = %v, want one naming %s", err, path)
		}
	}
	if reads != 1 {
		t.Errorf("a broken %s was read %d times within the recheck window", FileName, reads)
	}
}

func TestResolverReportsUnreadableFiles(t *testing.T) {
	t.Parallel()

	r := NewResolver(func(string) ([]byte, error) { return nil, fs.ErrPermission }, time.Now, time.Second)
	if _, err := r.Properties("/repo/a.sh", nil); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("error = %v, want the permission failure", err)
	}
}
