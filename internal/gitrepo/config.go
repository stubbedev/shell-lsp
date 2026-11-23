package gitrepo

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type key uint8

const (
	excludesFile key = iota + 1
	include
	conditionalInclude
	remoteURL
	worktreeConfig
	refStorage
	formatVersion
	objectFormat
)

type setting struct {
	key       key
	condition string
	value     string
	line      int
}

type config []setting

const (
	byteOrderMark      = "\xef\xbb\xbf"
	conditionalSection = "includeif"
	remoteSection      = "remote"
	pathName           = "path"
	urlName            = "url"
)

var (
	errBadSection   = errors.New("malformed section header")
	errBadVariable  = errors.New("malformed variable")
	errBadEscape    = errors.New("unknown escape sequence in a value")
	errOpenQuote    = errors.New("unterminated quote in a value")
	errNoValue      = errors.New("missing value")
	errEmptyInclude = errors.New("empty include path")
	errBadBoolean   = errors.New("not a boolean")
	errBadNumber    = errors.New("not a number in git's range")
	errNoSection    = errors.New("the key has no section")
	errNoName       = errors.New("the key has no variable name")
	errBadKey       = errors.New("invalid key")
)

func classify(name string) (key, string, bool) {
	switch name {
	case "core.excludesfile":
		return excludesFile, "", true
	case "include.path":
		return include, "", true
	case "extensions.worktreeconfig":
		return worktreeConfig, "", true
	case "extensions.refstorage":
		return refStorage, "", true
	case "core.repositoryformatversion":
		return formatVersion, "", true
	case "extensions.objectformat":
		return objectFormat, "", true
	}
	section, rest, _ := strings.Cut(name, ".")
	before, after, ok := strings.CutLast(rest, ".")
	if !ok {
		return 0, "", false
	}
	subsection, variable := before, after
	switch {
	case section == conditionalSection && variable == pathName:
		return conditionalInclude, subsection, true
	case section == remoteSection && variable == urlName:
		return remoteURL, "", true
	}
	return 0, "", false
}

func newSetting(name, value string, hasValue bool, line int) (setting, bool, error) {
	k, condition, ok := classify(name)
	if !ok {
		return setting{}, false, nil
	}
	if !hasValue {
		switch k {
		case worktreeConfig:
			value = "true"
		case formatVersion:
		case excludesFile, include, conditionalInclude, remoteURL, refStorage, objectFormat:
			return setting{}, false, fmt.Errorf("%s: %w", name, errNoValue)
		}
	}
	return setting{key: k, condition: condition, value: value, line: line}, true, nil
}

func parseConfig(path string, data []byte) (config, error) {
	s := scanner{text: strings.TrimPrefix(string(data), byteOrderMark), line: 1}
	var out config
	base := ""
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
			base = parsed
			continue
		}
		name, value, hasValue, err := s.variable(c)
		if err != nil {
			return nil, fmt.Errorf("reading git config %s, line %d: %w", path, line, err)
		}
		parsed, ok, err := newSetting(base+"."+name, value, hasValue, line)
		if err != nil {
			return nil, fmt.Errorf("reading git config %s, line %d: %w", path, line, err)
		}
		if ok {
			out = append(out, parsed)
		}
	}
}

func canonicalKey(name string) (string, error) {
	last := strings.LastIndexByte(name, '.')
	if last <= 0 {
		return "", fmt.Errorf("%q: %w", name, errNoSection)
	}
	if last == len(name)-1 {
		return "", fmt.Errorf("%q: %w", name, errNoName)
	}
	first := strings.IndexByte(name, '.')
	out := []byte(name)
	for i := range out {
		c := out[i]
		if i > first && i < last {
			if c == '\n' {
				return "", fmt.Errorf("%q: %w", name, errBadKey)
			}
			continue
		}
		if i == first || i == last {
			continue
		}
		if !isKeyChar(c) || i == last+1 && !isAlpha(c) {
			return "", fmt.Errorf("%q: %w", name, errBadKey)
		}
		out[i] = lower(c)
	}
	return string(out), nil
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off", "":
		return false, nil
	}
	n, err := parseInt(value)
	if err != nil {
		return false, fmt.Errorf("%q: %w", value, errBadBoolean)
	}
	return n != 0, nil
}

const (
	maxInt = 1<<31 - 1
	minInt = -1 << 31
)

func parseInt(value string) (int64, error) {
	text := strings.TrimLeft(value, cSpace)
	sign := ""
	if text != "" && (text[0] == '+' || text[0] == '-') {
		sign, text = text[:1], text[1:]
	}
	base, digits := 10, text
	switch {
	case hasRadix(text, "xX", 16):
		base, digits = 16, text[2:]
	case hasRadix(text, "bB", 2):
		base, digits = 2, text[2:]
	case strings.HasPrefix(text, "0"):
		base = 8
	}
	end := 0
	for end < len(digits) && digitValue(digits[end]) < base {
		end++
	}
	if end == 0 {
		return 0, fmt.Errorf("%q: %w", value, errBadNumber)
	}
	n, err := strconv.ParseInt(sign+digits[:end], base, 64)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", value, errBadNumber)
	}
	unit, ok := unitFactor(digits[end:])
	if !ok || n > maxInt/unit || n < minInt/unit {
		return 0, fmt.Errorf("%q: %w", value, errBadNumber)
	}
	return n * unit, nil
}

func hasRadix(text, letters string, base int) bool {
	return len(text) > 2 && text[0] == '0' && strings.IndexByte(letters, text[1]) >= 0 && digitValue(text[2]) < base
}

func digitValue(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= lower(c) && lower(c) <= 'z':
		return int(lower(c)-'a') + 10
	}
	return 36
}

func unitFactor(unit string) (int64, bool) {
	switch strings.ToLower(unit) {
	case "":
		return 1, true
	case "k":
		return 1 << 10, true
	case "m":
		return 1 << 20, true
	case "g":
		return 1 << 30, true
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

func (s *scanner) section() (string, error) {
	var name strings.Builder
	for {
		c := s.next()
		switch {
		case c == ']':
			if name.Len() == 0 {
				return "", errBadSection
			}
			return name.String(), nil
		case isSpace(c):
			return s.subsection(name.String())
		case c == '.' || isKeyChar(c):
			name.WriteByte(lower(c))
		default:
			return "", errBadSection
		}
	}
}

func (s *scanner) subsection(name string) (string, error) {
	c := s.next()
	for isSpace(c) {
		c = s.next()
	}
	if c != '"' || name == "" {
		return "", errBadSection
	}
	out := []byte(name + ".")
	for {
		c = s.next()
		if c == '\\' {
			c = s.next()
		} else if c == '"' {
			break
		}
		if c == '\n' {
			return "", errBadSection
		}
		out = append(out, c)
	}
	if s.next() != ']' {
		return "", errBadSection
	}
	return string(out), nil
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
