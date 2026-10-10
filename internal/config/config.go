package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const Section = "shellLsp"

type Settings struct {
	Dialect *dialect.Dialect
	Lint    Lint
	Format  Format
}

type Format struct {
	Enabled          bool
	Indent           *uint
	BinaryNextLine   bool
	SwitchCaseIndent bool
	SpaceRedirects   bool
	FunctionNextLine bool
	Simplify         bool
	Minify           bool
}

type Lint struct {
	Enabled bool
	Exclude []lint.Code
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
	Enable  *bool    `json:"enable"`
	Exclude []string `json:"exclude"`
}

func Parse(raw json.RawMessage) (Settings, error) {
	settings := Default()
	if isAbsent(raw) {
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
	set := func(dst *bool, src *bool) {
		if src != nil {
			*dst = *src
		}
	}
	set(&f.Enabled, raw.Enable)
	set(&f.BinaryNextLine, raw.BinaryNextLine)
	set(&f.SwitchCaseIndent, raw.SwitchCaseIndent)
	set(&f.SpaceRedirects, raw.SpaceRedirects)
	set(&f.FunctionNextLine, raw.FunctionNextLine)
	set(&f.Simplify, raw.Simplify)
	set(&f.Minify, raw.Minify)
	if raw.Indent != nil {
		f.Indent = raw.Indent
	}
	return f
}

func ParseSection(raw json.RawMessage) (Settings, bool, error) {
	if isAbsent(raw) {
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

func isAbsent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
