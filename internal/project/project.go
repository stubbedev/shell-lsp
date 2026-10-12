package project

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/format"
)

type Settings struct {
	Style   format.Style
	Dialect *dialect.Dialect
	Ignore  bool
}

type Resolution struct {
	dialect.Detection
	Settings Settings
}

func Resolve(resolver *editorconfig.Resolver, path string, forced *dialect.Dialect, in dialect.Input) (Resolution, error) {
	r := Resolution{Detection: dialect.Resolve(forced, in)}
	if path == "" {
		return r, nil
	}
	props, err := resolver.Properties(path, []string{"shell", r.Dialect.String()})
	if err != nil {
		return r, err
	}
	settings, err := FromEditorConfig(props)
	if err != nil {
		return r, fmt.Errorf("%s settings for %s: %w", editorconfig.FileName, path, err)
	}
	r.Settings = settings
	if forced == nil && settings.Dialect != nil {
		r.Detection = dialect.Detection{Shell: dialect.ShellOf(*settings.Dialect, in), Source: dialect.SourceEditorConfig, Evidence: props["shell_variant"]}
	}
	return r, nil
}

func FromEditorConfig(props editorconfig.Properties) (Settings, error) {
	var s Settings
	for _, flag := range []struct {
		key string
		dst **bool
	}{
		{"binary_next_line", &s.Style.BinaryNextLine},
		{"switch_case_indent", &s.Style.SwitchCaseIndent},
		{"space_redirects", &s.Style.SpaceRedirects},
		{"keep_padding", &s.Style.KeepPadding},
		{"function_next_line", &s.Style.FunctionNextLine},
		{"simplify", &s.Style.Simplify},
		{"minify", &s.Style.Minify},
	} {
		value, ok := props[flag.key]
		if !ok {
			continue
		}
		b, err := parseFlag(flag.key, value)
		if err != nil {
			return Settings{}, err
		}
		*flag.dst = &b
	}
	if value, ok := props["ignore"]; ok {
		ignore, err := parseFlag("ignore", value)
		if err != nil {
			return Settings{}, err
		}
		s.Ignore = ignore
	}
	indent, err := parseIndent(props["indent_style"], props["indent_size"])
	if err != nil {
		return Settings{}, err
	}
	s.Style.Indent = indent
	if value, ok := props["shell_variant"]; ok {
		d, known := dialect.FromName(value)
		if !known {
			return Settings{}, fmt.Errorf("shell_variant = %s: want one of %s", value, strings.Join(dialect.Names(), ", "))
		}
		s.Dialect = &d
	}
	return s, nil
}

func parseFlag(key, value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%s = %s: want true or false", key, value)
}

func parseIndent(style, size string) (*uint, error) {
	tabs := uint(0)
	switch strings.ToLower(style) {
	case "tab":
		return &tabs, nil
	case "", "space":
	default:
		return nil, fmt.Errorf("indent_style = %s: want tab or space", style)
	}
	switch {
	case size == "":
		return nil, nil
	case strings.EqualFold(size, "tab"):
		return &tabs, nil
	}
	n, err := strconv.ParseUint(size, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("indent_size = %s: want a number of spaces or tab", size)
	}
	spaces := uint(n)
	return &spaces, nil
}
