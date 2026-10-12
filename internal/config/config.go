package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const Section = "shellLsp"

type Settings struct {
	Dialect *dialect.Dialect
	Lint    Lint
	Format  Format
}

type Format struct {
	Enabled bool
	Style   format.Style
}

type Lint struct {
	Enabled  bool
	Profile  lint.Profile
	Exclude  []lint.Code
	Optional []string
}

func Default() Settings {
	return Settings{
		Lint:   Lint{Enabled: true},
		Format: Format{Enabled: true},
	}
}

type rawSettings struct {
	Dialect *string    `json:"dialect"`
	Lint    *rawLint   `json:"lint"`
	Format  *rawFormat `json:"format"`
}

type rawFormat struct {
	Enable           *bool `json:"enable"`
	Indent           *uint `json:"indent"`
	BinaryNextLine   *bool `json:"binaryNextLine"`
	SwitchCaseIndent *bool `json:"switchCaseIndent"`
	SpaceRedirects   *bool `json:"spaceRedirects"`
	FunctionNextLine *bool `json:"functionNextLine"`
	Simplify         *bool `json:"simplify"`
	Minify           *bool `json:"minify"`
}

type rawLint struct {
	Enable   *bool    `json:"enable"`
	Profile  *string  `json:"profile"`
	Exclude  []string `json:"exclude"`
	Optional []string `json:"optional"`
}

func Parse(raw json.RawMessage) (Settings, error) {
	settings := Default()
	if IsAbsent(raw) {
		return settings, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed rawSettings
	if err := decoder.Decode(&parsed); err != nil {
		return Settings{}, fmt.Errorf("parsing %s settings: %w", Section, err)
	}
	if parsed.Dialect != nil {
		d, ok := dialect.FromName(*parsed.Dialect)
		if !ok {
			return Settings{}, fmt.Errorf("%s.dialect: unknown dialect %q", Section, *parsed.Dialect)
		}
		settings.Dialect = &d
	}
	if parsed.Format != nil {
		settings.Format = parseFormat(*parsed.Format, settings.Format)
	}
	if parsed.Lint == nil {
		return settings, nil
	}
	l, err := parseLint(*parsed.Lint, settings.Lint)
	if err != nil {
		return Settings{}, err
	}
	settings.Lint = l
	return settings, nil
}

func parseFormat(raw rawFormat, f Format) Format {
	if raw.Enable != nil {
		f.Enabled = *raw.Enable
	}
	f.Style = format.Style{
		Indent:           raw.Indent,
		BinaryNextLine:   raw.BinaryNextLine,
		SwitchCaseIndent: raw.SwitchCaseIndent,
		SpaceRedirects:   raw.SpaceRedirects,
		FunctionNextLine: raw.FunctionNextLine,
		Simplify:         raw.Simplify,
		Minify:           raw.Minify,
	}.Over(f.Style)
	return f
}

func ParseSection(raw json.RawMessage) (Settings, bool, error) {
	if IsAbsent(raw) {
		return Settings{}, false, nil
	}
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sections); err != nil {
		return Settings{}, false, fmt.Errorf("parsing configuration settings: %w", err)
	}
	section, ok := sections[Section]
	if !ok {
		return Settings{}, false, nil
	}
	settings, err := Parse(section)
	return settings, true, err
}

func parseLint(raw rawLint, l Lint) (Lint, error) {
	if raw.Enable != nil {
		l.Enabled = *raw.Enable
	}
	if raw.Profile != nil {
		profile, ok := lint.ProfileNamed(*raw.Profile)
		if !ok {
			return Lint{}, fmt.Errorf("%s.lint.profile: unknown profile %q; use %s", Section, *raw.Profile, strings.Join(lint.ProfileNames(), ", "))
		}
		l.Profile = profile
	}
	for i, name := range raw.Optional {
		if !lint.EnablesOptional(name) {
			return Lint{}, fmt.Errorf("%s.lint.optional[%d]: %q is not an optional check; use all or a name such as require-variable-braces", Section, i, name)
		}
	}
	l.Optional = raw.Optional
	l.Exclude = make([]lint.Code, 0, len(raw.Exclude))
	for i, code := range raw.Exclude {
		c, err := lint.ParseCode(code)
		if err != nil {
			return Lint{}, fmt.Errorf("%s.lint.exclude[%d]: %w", Section, i, err)
		}
		l.Exclude = append(l.Exclude, c)
	}
	return l, nil
}

func IsAbsent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
