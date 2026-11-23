package gitrepo

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func at(k key, value string, line int) setting {
	return setting{key: k, value: value, line: line}
}

func TestParseConfigReadsTheSettingsThatDecideExcludes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want config
	}{
		{"plain", "[core]\n\texcludesFile = ~/ignore\n", config{at(excludesFile, "~/ignore", 2)}},
		{"case-insensitive section and key", "[Core]\nEXCLUDESFILE=a\n", config{at(excludesFile, "a", 2)}},
		{"key on the header line", "[core] excludesfile = a\n", config{at(excludesFile, "a", 1)}},
		{"other sections", "[user]\n\texcludesFile = no\n[core \"x\"]\n\texcludesFile = no\n[core.x]\n\texcludesFile = no\n[core \"\"]\n\texcludesFile = no\n", nil},
		{"other keys", "[core]\n\tautocrlf\n\teditor = vim\n\texcludes-file = no\n", nil},
		{"comments", "# [core]\n; excludesFile = no\n[core] ; comment\n\texcludesFile = a # comment\n", config{at(excludesFile, "a", 4)}},
		{"quotes keep spaces and comment characters", "[core]\n\texcludesFile = \"  a # b ; c  \"\n", config{at(excludesFile, "  a # b ; c  ", 2)}},
		{"unquoted inner spaces kept, outer dropped", "[core]\n\texcludesFile =   a  b\t \n", config{at(excludesFile, "a  b", 2)}},
		{"escapes and a continued line", "[core]\n\texcludesFile = a\\\\b\\\"c\\t\\\n  d\n", config{at(excludesFile, "a\\b\"c\t  d", 2)}},
		{"empty value", "[core]\n\texcludesFile =\n", config{at(excludesFile, "", 2)}},
		{"CRLF, BOM and no final newline", "\xef\xbb\xbf[core]\r\n\texcludesFile = a\r\n\texcludesFile = b", config{at(excludesFile, "a", 2), at(excludesFile, "b", 3)}},
		{"includes", "[include]\n\tpath = a\n[includeIf \"gitdir:~/W\\\"x/\"]\n\tpath = b\n[includeIf]\n\tpath = no\n[IncludeIf \"\"]\n\tpath = c\n", config{
			at(include, "a", 2),
			{key: conditionalInclude, condition: "gitdir:~/W\"x/", value: "b", line: 4},
			{key: conditionalInclude, value: "c", line: 8},
		}},
		{"remote URLs", "[remote \"Origin\"]\n\turl = u1\n\tpushurl = no\n[remote.o]\n\tURL = u2\n[remote]\n\turl = no\n", config{at(remoteURL, "u1", 2), at(remoteURL, "u2", 5)}},
		{"repository extensions", "[extensions]\n\tworktreeConfig\n\trefStorage = reftable\n", config{at(worktreeConfig, "true", 2), at(refStorage, "reftable", 3)}},
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

func TestParseConfigRejectsValuelessSettingsThatNeedAValue(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"[remote \"o\"]\n\turl\n", "[extensions]\n\trefStorage\n", "[includeIf \"gitdir:x\"]\n\tpath\n"} {
		if _, err := parseConfig("config", []byte(text)); !errors.Is(err, errNoValue) {
			t.Errorf("parseConfig(%q) = %v, want %v", text, err, errNoValue)
		}
	}
}

func TestCanonicalKeyFollowsGitKeyRules(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ in, want string }{
		{"Core.ExcludesFile", "core.excludesfile"},
		{"includeIf.gitdir:~/Work/.Path", "includeif.gitdir:~/Work/.path"},
		{"Remote.My.Origin.URL", "remote.My.Origin.url"},
		{"a-1.b-2", "a-1.b-2"},
	} {
		if got, err := canonicalKey(tt.in); err != nil || got != tt.want {
			t.Errorf("canonicalKey(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, tt := range []struct {
		in   string
		want error
	}{
		{"nodot", errNoSection},
		{".name", errNoSection},
		{"core.", errNoName},
		{"core.1x", errBadKey},
		{"co_re.x", errBadKey},
		{"core.ex_cludes", errBadKey},
		{"remote.a\nb.url", errBadKey},
	} {
		if _, err := canonicalKey(tt.in); !errors.Is(err, tt.want) {
			t.Errorf("canonicalKey(%q) = %v, want %v", tt.in, err, tt.want)
		}
	}
}

func TestParseBoolFollowsGitBooleans(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{
		"true": true, "YES": true, "On": true, "1": true, "2": true, "-1": true, "1k": true, "0x10": true,
		"false": false, "No": false, "OFF": false, "": false, "0": false, "0k": false,
	} {
		if got, err := parseBool(value); err != nil || got != want {
			t.Errorf("parseBool(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"garbage", "1x", "k", "99999999999999999999", "9999999999999999g", "3g", "2147483648"} {
		if got, err := parseBool(value); !errors.Is(err, errBadBoolean) {
			t.Errorf("parseBool(%q) = %v, %v; want %v", value, got, err, errBadBoolean)
		}
	}
}

func TestPathnameFollowsGitPathValues(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "home")
	other := filepath.Join(t.TempDir(), "other")
	prefix := filepath.Join(t.TempDir(), "prefix")
	base := filepath.Join(t.TempDir(), "repo")
	absolute := filepath.Join(t.TempDir(), "abs", "ignore")
	e := expander{
		home:   home,
		prefix: func() (string, error) { return prefix, nil },
		userHome: func(name string) (string, error) {
			if name == "other" {
				return other, nil
			}
			return "", errNotExpanded
		},
	}
	tests := []struct {
		value string
		want  pathValue
	}{
		{"~/x/ignore", pathValue{path: filepath.Join(home, "x", "ignore")}},
		{"~", pathValue{path: home}},
		{"~other/x", pathValue{path: filepath.Join(other, "x")}},
		{"~other", pathValue{path: other}},
		{"%(prefix)/etc/ignore", pathValue{path: filepath.Join(prefix, "etc", "ignore")}},
		{"rel/ignore", pathValue{path: filepath.Join(base, "rel", "ignore")}},
		{absolute, pathValue{path: absolute}},
		{":(optional)~/x", pathValue{path: filepath.Join(home, "x"), optional: true}},
		{":(optional)", pathValue{optional: true}},
		{"", pathValue{}},
	}
	for _, tt := range tests {
		got, err := e.pathname(tt.value, base)
		if err != nil || got != tt.want {
			t.Errorf("pathname(%q) = %+v, %v; want %+v", tt.value, got, err, tt.want)
		}
	}
	for _, value := range []string{"~nobody/x", "~nobody"} {
		if got, err := e.pathname(value, base); !errors.Is(err, errNotExpanded) {
			t.Errorf("pathname(%q) = %+v, %v; want %v", value, got, err, errNotExpanded)
		}
	}
	if got, err := e.pathname("rel", ""); !errors.Is(err, errRelativeInclude) {
		t.Errorf("pathname(rel) without a base = %+v, %v; want %v", got, err, errRelativeInclude)
	}
	for _, value := range []string{"~/x", "~other/x", "%(prefix)/x"} {
		if got, err := (expander{}).pathname(value, base); err == nil {
			t.Errorf("pathname(%q) without a home, a user lookup or a prefix = %+v, want an error", value, got)
		}
	}
}

func TestParseIntFollowsGitIntegers(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]int64{
		"0": 0, "1": 1, "+1": 1, "-1": -1, " 7": 7, "010": 8, "0x1f": 31, "0X1F": 31, "0b11": 3, "1k": 1 << 10, "2M": 2 << 20, "1g": 1 << 30,
		"2147483647": 1<<31 - 1, "-2147483648": -1 << 31, "-2g": -1 << 31, "+0x1k": 1 << 10,
	} {
		if got, err := parseInt(value); err != nil || got != want {
			t.Errorf("parseInt(%q) = %d, %v; want %d", value, got, err, want)
		}
	}
	for _, value := range []string{"", "x", "k", "08", "0x", " 0x", "1_0", "0o7", "1 k", "1kb", "true", "2147483648", "2g", "-2147483649", "99999999999999999999"} {
		if got, err := parseInt(value); !errors.Is(err, errBadNumber) {
			t.Errorf("parseInt(%q) = %d, %v; want %v", value, got, err, errBadNumber)
		}
	}
}
