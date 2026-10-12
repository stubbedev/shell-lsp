package shellcheckrc

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestDisableRangeExcludesItsEnd(t *testing.T) {
	t.Parallel()

	rc, err := Parse("/p/.shellcheckrc", "disable=SC2000-SC2086\n")
	if err != nil {
		t.Fatal(err)
	}
	if !rc.Disabled.Has(2085) || rc.Disabled.Has(2086) {
		t.Errorf("disable=SC2000-SC2086: Has(2085) = %t, Has(2086) = %t; want true, false as in shellcheck", rc.Disabled.Has(2085), rc.Disabled.Has(2086))
	}
}

func TestParseNamesTheFileAndLine(t *testing.T) {
	t.Parallel()

	rc, err := Parse("/p/.shellcheckrc", "# comment\ndisable=SC2086\nenable=require-variable-braces\nshell=bash\nsource-path=lib\n")
	if err != nil {
		t.Fatal(err)
	}
	if !rc.Disabled.Has(2086) || !slices.Equal(rc.Enabled, []string{"require-variable-braces"}) || rc.Shell == nil || !slices.Equal(rc.SourcePaths, []string{filepath.Join("/p", "lib")}) {
		t.Errorf("Parse = %+v", rc)
	}
	_, err = Parse("/p/.shellcheckrc", "disable=SC2086\nbogus=1\n")
	if err == nil || !strings.HasPrefix(err.Error(), "/p/.shellcheckrc:2:") {
		t.Fatalf("error = %v, want one naming the file and line 2", err)
	}
	_, listed, _ := strings.Cut(err.Error(), "known keys are ")
	want := slices.DeleteFunc(directive.KeyNames(), func(name string) bool { return name == directive.Source.String() })
	if names := strings.FieldsFunc(strings.ReplaceAll(listed, " and ", ", "), func(r rune) bool { return r == ',' || r == ' ' }); !slices.Equal(names, want) {
		t.Errorf("error %q lists %q, want every key but source", err, names)
	}
}

type fakeDisk map[string]string

func (d fakeDisk) read(path string) ([]byte, error) {
	text, ok := d[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(text), nil
}

func TestNearestWalksUpThenFallsBack(t *testing.T) {
	t.Parallel()

	root := filepath.FromSlash("/w")
	disk := fakeDisk{
		filepath.Join(root, "a", "shellcheckrc"):     "disable=SC2034\n",
		filepath.Join("/home", "u", ".shellcheckrc"): "disable=SC2086\n",
	}
	now := time.Unix(0, 0)
	r := NewResolver(disk.read, func() time.Time { return now }, time.Minute, Fallback(filepath.Join("/home", "u"), ""))
	rc, found, err := r.Nearest(filepath.Join(root, "a", "b"))
	if err != nil || !found || rc.Path != filepath.Join(root, "a", "shellcheckrc") {
		t.Fatalf("Nearest = %+v, %t, %v", rc, found, err)
	}
	rc, found, err = r.Nearest(filepath.Join(root, "c"))
	if err != nil || !found || !rc.Disabled.Has(2086) {
		t.Fatalf("Nearest outside a = %+v, %t, %v, want the home fallback", rc, found, err)
	}

	disk[filepath.Join(root, "c", FileName)] = "disable=SC2154\n"
	if rc, _, _ := r.Nearest(filepath.Join(root, "c")); rc.Disabled.Has(2154) {
		t.Error("a new file was read before the recheck interval passed")
	}
	r.Invalidate(filepath.Join(root, "c", FileName))
	if rc, _, _ := r.Nearest(filepath.Join(root, "c")); !rc.Disabled.Has(2154) {
		t.Error("Invalidate did not make the new file visible")
	}
	disk[filepath.Join(root, "c", FileName)] = "disable=SC2155\n"
	now = now.Add(time.Minute)
	if rc, _, _ := r.Nearest(filepath.Join(root, "c")); !rc.Disabled.Has(2155) {
		t.Error("the file was not reread after the recheck interval")
	}
}

func TestNearestReportsABrokenFile(t *testing.T) {
	t.Parallel()

	disk := fakeDisk{filepath.Join("/w", FileName): "disable=nope\n"}
	r := NewResolver(disk.read, time.Now, time.Minute, nil)
	if _, _, err := r.Nearest(filepath.Join("/w", "x")); err == nil {
		t.Error("a malformed .shellcheckrc was not reported")
	}
}

func TestDisableEdit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		text, code, want string
		ok               bool
	}{
		{"", "SC2086", "disable=SC2086\n", true},
		{"shell=bash", "SC2086", "shell=bash\ndisable=SC2086\n", true},
		{"# keep\ndisable=SC2034 # old\nshell=bash\n", "SC2086", "# keep\ndisable=SC2034,SC2086 # old\nshell=bash\n", true},
		{"shell=bash disable=SC2034\n", "SL1001", "shell=bash disable=SC2034,SL1001\n", true},
		{"disable=SC2000-SC2100\n", "SC2086", "", false},
		{"disable=SC2000-SC2086\n", "SC2086", "disable=SC2000-SC2086,SC2086\n", true},
		{"disable=all\n", "SC2086", "", false},
		{"disable=SC2086\n", "SC2086", "", false},
		{"disable='SC2034'\n", "SC2086", "disable='SC2034'\ndisable=SC2086\n", true},
	} {
		code, err := lint.ParseCode(tc.code)
		if err != nil {
			t.Fatal(err)
		}
		edit, ok := DisableEdit(tc.text, code)
		if ok != tc.ok {
			t.Errorf("DisableEdit(%q, %s) ok = %t, want %t", tc.text, tc.code, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		got := textedit.Apply(tc.text, []textedit.Edit{edit})
		if got != tc.want {
			t.Errorf("DisableEdit(%q, %s) = %q, want %q", tc.text, tc.code, got, tc.want)
		}
		if _, again := DisableEdit(got, code); again {
			t.Errorf("DisableEdit is not idempotent on %q", got)
		}
	}
}

func TestStatementDisableEdit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		text, want string
		ok         bool
	}{
		{"f() {\n\techo $1\n}\n", "f() {\n\t# shellcheck disable=SC2086\n\techo $1\n}\n", true},
		{"f() {\n\t#shellcheck disable=SC2034\n\techo $1\n}\n", "f() {\n\t#shellcheck disable=SC2034,SC2086\n\techo $1\n}\n", true},
		{"f() {\n\t# shellcheck disable=SC2034\n\t# why: it is fine\n\techo $1\n}\n", "f() {\n\t# shellcheck disable=SC2034,SC2086\n\t# why: it is fine\n\techo $1\n}\n", true},
		{"f() {\n\t# shellcheck disable=SC2000-SC2100\n\techo $1\n}\n", "", false},
		{"f() {\n\t# shellcheck disable=all\n\techo $1\n}\n", "", false},
		{"f() {\n\t# shellcheck disable=SC2086\n\techo $1\n}\n", "", false},
		{"x=1\n\n\techo $1\n}\n", "x=1\n\n\t# shellcheck disable=SC2086\n\techo $1\n}\n", true},
	} {
		edit, ok := StatementDisableEdit(tc.text, strings.Index(tc.text, "\techo"), "\t", 2086)
		if ok != tc.ok {
			t.Errorf("StatementDisableEdit(%q) ok = %t, want %t", tc.text, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		got := textedit.Apply(tc.text, []textedit.Edit{edit})
		if got != tc.want {
			t.Errorf("StatementDisableEdit(%q) = %q, want %q", tc.text, got, tc.want)
		}
		if _, again := StatementDisableEdit(got, strings.Index(got, "\techo"), "\t", 2086); again {
			t.Errorf("StatementDisableEdit is not idempotent on %q", got)
		}
	}
}
