package config

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

func TestParseDefaults(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "null", "{}"} {
		got, err := Parse(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", raw, err)
		}
		if got.Dialect != nil || !got.Lint.Enabled || got.Lint.Profile != lint.ProfileRecommended || len(got.Lint.Exclude) != 0 {
			t.Errorf("Parse(%q) = %+v, want defaults", raw, got)
		}
	}
}

func TestParseFull(t *testing.T) {
	t.Parallel()

	got, err := Parse(json.RawMessage(`{"dialect":"zsh","lint":{"enable":false,"profile":"minimal","exclude":["SC2034","sc1090","2154"],"optional":["require-variable-braces"]}}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Dialect == nil || *got.Dialect != dialect.Zsh {
		t.Errorf("Dialect = %v, want zsh", got.Dialect)
	}
	if got.Lint.Enabled || got.Lint.Profile != lint.ProfileMinimal {
		t.Errorf("Lint = %+v, want disabled with the minimal profile", got.Lint)
	}
	if !slices.Equal(got.Lint.Exclude, []lint.Code{2034, 1090, 2154}) {
		t.Errorf("Exclude = %v", got.Lint.Exclude)
	}
	if !slices.Equal(got.Lint.Optional, []string{"require-variable-braces"}) {
		t.Errorf("Optional = %v", got.Lint.Optional)
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		`{"dialect":"fish"}`:                                     "unknown dialect",
		`{"lint":{"exclude":["nope"]}}`:                          "exclude[0]",
		`{"lint":{"profile":"loud"}}`:                            "lint.profile",
		`{"lint":{"optional":["all","require-varible-braces"]}}`: "optional[1]",
		`{"shellcheck":{"enable":false}}`:                        "unknown field",
		`{"lint":{"path":"/opt/linter"}}`:                        "unknown field",
		`{"unknownKey":1}`:                                       "unknown field",
		`[1]`:                                                    "parsing",
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

func TestParseFormat(t *testing.T) {
	t.Parallel()

	got, err := Parse(json.RawMessage(`{"format": {"indent": 2, "switchCaseIndent": true, "enable": false}}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if style := got.Format.Style; got.Format.Enabled || style.Indent == nil || *style.Indent != 2 || style.SwitchCaseIndent == nil || !*style.SwitchCaseIndent || style.Simplify != nil {
		t.Errorf("Format = %+v", got.Format)
	}
	if def := Default().Format; !def.Enabled || def.Style != (format.Style{}) {
		t.Errorf("default Format = %+v", def)
	}
	if _, err := Parse(json.RawMessage(`{"format": {"indent": -1}}`)); err == nil {
		t.Error("negative indent accepted")
	}
}
