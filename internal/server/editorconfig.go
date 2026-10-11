package server

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/format"
)

type projectFormat struct {
	style   format.Style
	dialect *dialect.Dialect
	ignore  bool
}

func (s *Server) resolveDialect(path string, forced *dialect.Dialect, in dialect.Input) (dialect.Dialect, projectFormat, error) {
	detected := dialect.Detect(forced, in)
	if path == "" {
		return detected, projectFormat{}, nil
	}
	props, err := s.editorconfig.Properties(path, []string{"shell", detected.String()})
	if err != nil {
		return detected, projectFormat{}, err
	}
	project, err := readProjectFormat(props)
	if err != nil {
		return detected, projectFormat{}, fmt.Errorf("%s settings for %s: %w", editorconfig.FileName, path, err)
	}
	if forced == nil && project.dialect != nil {
		return *project.dialect, project, nil
	}
	return detected, project, nil
}

func readProjectFormat(props editorconfig.Properties) (projectFormat, error) {
	var p projectFormat
	for _, flag := range []struct {
		key string
		dst **bool
	}{
		{"binary_next_line", &p.style.BinaryNextLine},
		{"switch_case_indent", &p.style.SwitchCaseIndent},
		{"space_redirects", &p.style.SpaceRedirects},
		{"keep_padding", &p.style.KeepPadding},
		{"function_next_line", &p.style.FunctionNextLine},
		{"simplify", &p.style.Simplify},
		{"minify", &p.style.Minify},
	} {
		value, ok := props[flag.key]
		if !ok {
			continue
		}
		b, err := parseFlag(flag.key, value)
		if err != nil {
			return projectFormat{}, err
		}
		*flag.dst = &b
	}
	if value, ok := props["ignore"]; ok {
		ignore, err := parseFlag("ignore", value)
		if err != nil {
			return projectFormat{}, err
		}
		p.ignore = ignore
	}
	indent, err := parseIndent(props["indent_style"], props["indent_size"])
	if err != nil {
		return projectFormat{}, err
	}
	p.style.Indent = indent
	if value, ok := props["shell_variant"]; ok {
		d, known := dialect.FromName(value)
		if !known {
			return projectFormat{}, fmt.Errorf("shell_variant = %s: want bash, posix, mksh, bats or zsh", value)
		}
		p.dialect = &d
	}
	return p, nil
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
