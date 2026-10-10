package config

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestParseDefaults(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "null", "{}"} {
		got, err := Parse(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", raw, err)
		}
		if got.Dialect != nil || !got.Shellcheck.Enabled || got.Shellcheck.Path != "shellcheck" {
			t.Errorf("Parse(%q) = %+v, want defaults", raw, got)
		}
	}
}

func TestParseFull(t *testing.T) {
	t.Parallel()

	got, err := Parse(json.RawMessage(`{"dialect":"zsh","shellcheck":{"enable":false,"path":"/opt/sc","arguments":["--severity=style","-x"],"exclude":["SC2034","sc1090","2154"]}}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Dialect == nil || *got.Dialect != dialect.Zsh {
		t.Errorf("Dialect = %v, want zsh", got.Dialect)
	}
	if got.Shellcheck.Enabled || got.Shellcheck.Path != "/opt/sc" {
		t.Errorf("Shellcheck = %+v", got.Shellcheck)
	}
	if !slices.Equal(got.Shellcheck.Exclude, []int{2034, 1090, 2154}) {
		t.Errorf("Exclude = %v", got.Shellcheck.Exclude)
	}
	if !slices.Equal(got.Shellcheck.Arguments, []string{"--severity=style", "-x"}) {
		t.Errorf("Arguments = %v", got.Shellcheck.Arguments)
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		`{"dialect":"fish"}`:                            "unknown dialect",
		`{"shellcheck":{"path":" "}}`:                   "must not be empty",
		`{"shellcheck":{"exclude":["nope"]}}`:           "exclude[0]",
		`{"shellcheck":{"arguments":["--format=gcc"]}}`: "json1",
		`{"shellcheck":{"arguments":["-s","bash"]}}`:    "dialect",
		`{"unknownKey":1}`:                              "unknown field",
		`[1]`:                                           "parsing",
	}
	for raw, want := range tests {
		_, err := Parse(json.RawMessage(raw))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%s) error = %v, want mention of %q", raw, err, want)
		}
	}
}

func TestParseSection(t *testing.T) {
	t.Parallel()

	if _, found, err := ParseSection(json.RawMessage(`{"other":{}}`)); found || err != nil {
		t.Errorf("missing section: found=%t err=%v", found, err)
	}
	if _, found, err := ParseSection(json.RawMessage(`null`)); found || err != nil {
		t.Errorf("null settings: found=%t err=%v", found, err)
	}
	got, found, err := ParseSection(json.RawMessage(`{"shellLsp":{"dialect":"ksh"}}`))
	if !found || err != nil || got.Dialect == nil || *got.Dialect != dialect.Ksh {
		t.Errorf("ParseSection = %+v, %t, %v", got, found, err)
	}
}
