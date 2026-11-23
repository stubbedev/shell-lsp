package gitrepo

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseConfigReadsTheSettingsThatDecideExcludes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want config
	}{
		{"plain", "[core]\n\texcludesFile = ~/ignore\n", config{{excludesFile, "~/ignore", 2}}},
		{"case-insensitive section and key", "[Core]\nEXCLUDESFILE=a\n", config{{excludesFile, "a", 2}}},
		{"key on the header line", "[core] excludesfile = a\n", config{{excludesFile, "a", 1}}},
		{"other sections", "[user]\n\texcludesFile = no\n[core \"x\"]\n\texcludesFile = no\n[core.x]\n\texcludesFile = no\n[core \"\"]\n\texcludesFile = no\n", nil},
		{"other keys", "[core]\n\tautocrlf\n\teditor = vim\n\texcludes-file = no\n", nil},
		{"comments", "# [core]\n; excludesFile = no\n[core] ; comment\n\texcludesFile = a # comment\n", config{{excludesFile, "a", 4}}},
		{"quotes keep spaces and comment characters", "[core]\n\texcludesFile = \"  a # b ; c  \"\n", config{{excludesFile, "  a # b ; c  ", 2}}},
		{"unquoted inner spaces kept, outer dropped", "[core]\n\texcludesFile =   a  b\t \n", config{{excludesFile, "a  b", 2}}},
		{"escapes and a continued line", "[core]\n\texcludesFile = a\\\\b\\\"c\\t\\\n  d\n", config{{excludesFile, "a\\b\"c\t  d", 2}}},
		{"empty value", "[core]\n\texcludesFile =\n", config{{excludesFile, "", 2}}},
		{"CRLF, BOM and no final newline", "\xef\xbb\xbf[core]\r\n\texcludesFile = a\r\n\texcludesFile = b", config{{excludesFile, "a", 2}, {excludesFile, "b", 3}}},
		{"includes", "[include]\n\tpath = a\n[includeIf \"gitdir:~/w/\"]\n\tpath = b\n[includeIf]\n\tpath = no\n", config{{include, "a", 2}, {conditionalInclude, "b", 4}}},
	}
	for _, tt := range tests {
		got, err := parseConfig("config", []byte(tt.text))
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(setting{})); diff != "" {
			t.Errorf("%s (-want +got):\n%s", tt.name, diff)
		}
	}
}

func TestParseConfigRejectsWhatGitRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want error
	}{
		{"[core]\n\texcludesFile\n", errNoValue},
		{"[include]\n\tpath\n", errNoValue},
		{"[core]\n\texcludesFile = \"a\n", errOpenQuote},
		{"[core]\n\texcludesFile = a\\q\n", errBadEscape},
		{"[core]\n\t1key = a\n", errBadVariable},
		{"[core]\n\texcludes_file = a\n", errBadVariable},
		{"[core]\n\tkey # comment\n", errBadVariable},
		{"[]\n", errBadSection},
		{"[core ]\n", errBadSection},
		{"[core\n", errBadSection},
		{"[core \"x\n", errBadSection},
	}
	for _, tt := range tests {
		if _, err := parseConfig("config", []byte(tt.text)); !errors.Is(err, tt.want) {
			t.Errorf("parseConfig(%q) = %v, want %v", tt.text, err, tt.want)
		}
	}
}

func TestExpandPathFollowsGitPathValues(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "home")
	base := filepath.Join(t.TempDir(), "repo")
	absolute := filepath.Join(t.TempDir(), "abs", "ignore")
	tests := []struct {
		value string
		want  pathValue
	}{
		{"~/x/ignore", pathValue{path: filepath.Join(home, "x", "ignore")}},
		{"~", pathValue{path: home}},
		{"rel/ignore", pathValue{path: filepath.Join(base, "rel", "ignore")}},
		{absolute, pathValue{path: absolute}},
		{":(optional)~/x", pathValue{path: filepath.Join(home, "x"), optional: true}},
	}
	for _, tt := range tests {
		got, err := expandPath(tt.value, home, base)
		if err != nil || got != tt.want {
			t.Errorf("expandPath(%q) = %+v, %v; want %+v", tt.value, got, err, tt.want)
		}
	}
	for _, value := range []string{"~other/x", "%(prefix)/etc/ignore"} {
		if got, err := expandPath(value, home, base); err == nil {
			t.Errorf("expandPath(%q) = %+v, want an error", value, got)
		}
	}
	if got, err := expandPath("~/x", "", base); err == nil {
		t.Errorf("expandPath(~/x) without a home = %+v, want an error", got)
	}
}
