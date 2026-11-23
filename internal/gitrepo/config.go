package gitrepo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type directive uint8

const (
	excludesFile directive = iota + 1
	include
	conditionalInclude
)

type setting struct {
	directive directive
	value     string
	line      int
}

type config []setting

type section uint8

const (
	otherSection section = iota
	coreSection
	includeSection
	includeIfSection
)

const (
	byteOrderMark  = "\xef\xbb\xbf"
	optionalPrefix = ":(optional)"
	prefixVariable = "%(prefix)/"
	tilde          = "~"
)

var (
	errBadSection   = errors.New("malformed section header")
	errBadVariable  = errors.New("malformed variable")
	errBadEscape    = errors.New("unknown escape sequence in a value")
	errOpenQuote    = errors.New("unterminated quote in a value")
	errNoValue      = errors.New("missing value")
	errEmptyInclude = errors.New("empty include path")
)

func parseConfig(path string, data []byte) (config, error) {
	s := scanner{text: strings.TrimPrefix(string(data), byteOrderMark), line: 1}
	var out config
	current := otherSection
	comment := false
	for {
		c := s.next()
		if c == '\n' {
			if s.eof {
				return out, nil
			}
			comment = false
			continue
		}
		if comment || isSpace(c) {
			continue
		}
		if c == '#' || c == ';' {
			comment = true
			continue
		}
		line := s.line
		if c == '[' {
			parsed, err := s.section()
			if err != nil {
				return nil, fmt.Errorf("reading git config %s, line %d: %w", path, line, err)
			}
			current = parsed
			continue
		}
		name, value, hasValue, err := s.variable(c)
		if err != nil {
			return nil, fmt.Errorf("reading git config %s, line %d: %w", path, line, err)
		}
		d, ok := current.directive(name)
		if !ok {
			continue
		}
		if !hasValue {
			return nil, fmt.Errorf("reading git config %s, line %d: %s: %w", path, line, name, errNoValue)
		}
		out = append(out, setting{directive: d, value: value, line: line})
	}
}

func (s section) directive(name string) (directive, bool) {
	switch {
	case s == coreSection && name == "excludesfile":
		return excludesFile, true
	case s == includeSection && name == "path":
		return include, true
	case s == includeIfSection && name == "path":
		return conditionalInclude, true
	}
	return 0, false
}

type scanner struct {
	text string
	pos  int
	line int
	eof  bool
}

func (s *scanner) next() byte {
	if s.pos >= len(s.text) {
		s.eof = true
		return '\n'
	}
	c := s.text[s.pos]
	s.pos++
	if c == '\r' && s.pos < len(s.text) && s.text[s.pos] == '\n' {
		c = '\n'
		s.pos++
	}
	if c == '\n' {
		s.line++
	}
	return c
}

func (s *scanner) section() (section, error) {
	var name strings.Builder
	for {
		c := s.next()
		switch {
		case c == ']':
			return plainSection(name.String())
		case isSpace(c):
			return s.subsection(name.String())
		case c == '.' || isKeyChar(c):
			name.WriteByte(lower(c))
		default:
			return 0, errBadSection
		}
	}
}

func plainSection(name string) (section, error) {
	switch name {
	case "":
		return 0, errBadSection
	case "core":
		return coreSection, nil
	case "include":
		return includeSection, nil
	}
	return otherSection, nil
}

func (s *scanner) subsection(name string) (section, error) {
	c := s.next()
	for isSpace(c) {
		c = s.next()
	}
	if c != '"' || name == "" {
		return 0, errBadSection
	}
	for {
		c = s.next()
		if c == '\\' {
			c = s.next()
		} else if c == '"' {
			break
		}
		if c == '\n' {
			return 0, errBadSection
		}
	}
	if s.next() != ']' {
		return 0, errBadSection
	}
	if name == "includeif" {
		return includeIfSection, nil
	}
	return otherSection, nil
}

func (s *scanner) variable(first byte) (name, value string, hasValue bool, err error) {
	if !isAlpha(first) {
		return "", "", false, errBadVariable
	}
	var key strings.Builder
	key.WriteByte(lower(first))
	c := s.next()
	for isKeyChar(c) {
		key.WriteByte(lower(c))
		c = s.next()
	}
	for c == ' ' || c == '\t' {
		c = s.next()
	}
	if c == '\n' {
		return key.String(), "", false, nil
	}
	if c != '=' {
		return "", "", false, errBadVariable
	}
	value, err = s.value()
	return key.String(), value, true, err
}

func (s *scanner) value() (string, error) {
	var out strings.Builder
	quoted, comment := false, false
	spaces := 0
	for {
		c := s.next()
		if c == '\n' {
			if quoted {
				return "", errOpenQuote
			}
			return out.String(), nil
		}
		if comment {
			continue
		}
		if isSpace(c) && !quoted {
			if out.Len() > 0 {
				spaces++
			}
			continue
		}
		if !quoted && (c == ';' || c == '#') {
			comment = true
			continue
		}
		for ; spaces > 0; spaces-- {
			out.WriteByte(' ')
		}
		switch c {
		case '\\':
			escaped, continued, err := s.escape()
			if err != nil {
				return "", err
			}
			if !continued {
				out.WriteByte(escaped)
			}
		case '"':
			quoted = !quoted
		default:
			out.WriteByte(c)
		}
	}
}

func (s *scanner) escape() (byte, bool, error) {
	c := s.next()
	switch c {
	case '\n':
		return 0, true, nil
	case 't':
		return '\t', false, nil
	case 'b':
		return '\b', false, nil
	case 'n':
		return '\n', false, nil
	case '\\', '"':
		return c, false, nil
	}
	return 0, false, errBadEscape
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f' }

func isAlpha(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isKeyChar(c byte) bool { return isAlpha(c) || '0' <= c && c <= '9' || c == '-' }

func lower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

type pathValue struct {
	path     string
	optional bool
}

func expandPath(value, home, base string) (pathValue, error) {
	rest, optional := strings.CutPrefix(value, optionalPrefix)
	if strings.HasPrefix(rest, prefixVariable) {
		return pathValue{}, fmt.Errorf("the path %q starts with %s, git's installation prefix, which shell-lsp does not know", value, prefixVariable)
	}
	if after, ok := strings.CutPrefix(rest, tilde); ok {
		if after != "" && !strings.HasPrefix(after, "/") && !strings.HasPrefix(after, string(filepath.Separator)) {
			return pathValue{}, fmt.Errorf("the path %q names another user's home directory, which shell-lsp does not look up", value)
		}
		if home == "" {
			return pathValue{}, fmt.Errorf("the path %q starts at the home directory, which is unknown", value)
		}
		return pathValue{path: filepath.Join(home, filepath.FromSlash(after)), optional: optional}, nil
	}
	rest = filepath.FromSlash(rest)
	if !filepath.IsAbs(rest) {
		rest = filepath.Join(base, rest)
	}
	return pathValue{path: filepath.Clean(rest), optional: optional}, nil
}
