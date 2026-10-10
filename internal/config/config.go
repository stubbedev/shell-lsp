package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

const Section = "shellLsp"

type Settings struct {
	Dialect    *dialect.Dialect
	Shellcheck Shellcheck
}

type Shellcheck struct {
	Enabled   bool
	Path      string
	Arguments []string
	Exclude   []int
}

func Default() Settings {
	return Settings{Shellcheck: Shellcheck{Enabled: true, Path: "shellcheck"}}
}

type rawSettings struct {
	Dialect    *string        `json:"dialect"`
	Shellcheck *rawShellcheck `json:"shellcheck"`
}

type rawShellcheck struct {
	Enable    *bool    `json:"enable"`
	Path      *string  `json:"path"`
	Arguments []string `json:"arguments"`
	Exclude   []string `json:"exclude"`
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
	if parsed.Shellcheck == nil {
		return settings, nil
	}
	sc, err := parseShellcheck(*parsed.Shellcheck, settings.Shellcheck)
	if err != nil {
		return Settings{}, err
	}
	settings.Shellcheck = sc
	return settings, nil
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

func parseShellcheck(raw rawShellcheck, sc Shellcheck) (Shellcheck, error) {
	if raw.Enable != nil {
		sc.Enabled = *raw.Enable
	}
	if raw.Path != nil {
		if strings.TrimSpace(*raw.Path) == "" {
			return Shellcheck{}, fmt.Errorf("%s.shellcheck.path: must not be empty", Section)
		}
		sc.Path = *raw.Path
	}
	for i, arg := range raw.Arguments {
		if reserved := reservedFlag(arg); reserved != "" {
			return Shellcheck{}, fmt.Errorf("%s.shellcheck.arguments[%d]: %q is set by the server (%s)", Section, i, arg, reserved)
		}
	}
	sc.Arguments = raw.Arguments
	sc.Exclude = make([]int, 0, len(raw.Exclude))
	for i, code := range raw.Exclude {
		n, err := parseCode(code)
		if err != nil {
			return Shellcheck{}, fmt.Errorf("%s.shellcheck.exclude[%d]: %w", Section, i, err)
		}
		sc.Exclude = append(sc.Exclude, n)
	}
	return sc, nil
}

func reservedFlag(arg string) string {
	name, _, _ := strings.Cut(arg, "=")
	switch {
	case name == "--format" || strings.HasPrefix(name, "-f"):
		return "output is always json1"
	case name == "--shell" || strings.HasPrefix(name, "-s"):
		return fmt.Sprintf("the dialect is detected per file; set %s.dialect to override it", Section)
	case name == "--exclude" || strings.HasPrefix(name, "-e"):
		return fmt.Sprintf("use %s.shellcheck.exclude", Section)
	case arg == "-":
		return "the document is passed on stdin"
	}
	return ""
}

func parseCode(code string) (int, error) {
	digits := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(code)), "SC")
	n, err := strconv.Atoi(digits)
	if err != nil || n <= 0 {
		return 0, errors.New("expected a shellcheck code like SC2034, got " + strconv.Quote(code))
	}
	return n, nil
}

func isAbsent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
