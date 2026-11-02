package statixconfig

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type valueKind uint8

const (
	scalarValue valueKind = iota
	stringValue
	arrayValue
	tableValue
)

func (k valueKind) String() string {
	switch k {
	case stringValue:
		return "a string"
	case arrayValue:
		return "an array"
	case tableValue:
		return "a table"
	case scalarValue:
	}
	return "a scalar"
}

type value struct {
	kind  valueKind
	text  string
	items []value
	line  int
}

type lexer struct {
	text string
	at   int
	line int
}

func parseTOML(text string) (map[string]value, error) {
	l := &lexer{text: text, line: 1}
	out := map[string]value{}
	inTable := false
	for {
		l.skipTrivia(true)
		if l.done() {
			return out, nil
		}
		if l.text[l.at] == '[' {
			if err := l.tableHeader(); err != nil {
				return nil, err
			}
			inTable = true
			continue
		}
		key, line, err := l.key()
		if err != nil {
			return nil, err
		}
		l.skipTrivia(false)
		if l.done() || l.text[l.at] != '=' {
			return nil, fmt.Errorf("line %d: expected `=` after the key %q", l.line, key)
		}
		l.at++
		l.skipTrivia(false)
		v, err := l.value()
		if err != nil {
			return nil, err
		}
		l.skipTrivia(false)
		if !l.done() && l.text[l.at] != '\n' {
			return nil, fmt.Errorf("line %d: unexpected %q after the value of %q", l.line, l.text[l.at], key)
		}
		if inTable {
			continue
		}
		if _, repeated := out[key]; repeated {
			return nil, fmt.Errorf("line %d: the key %q is set twice", line, key)
		}
		out[key] = v
	}
}

func (l *lexer) done() bool { return l.at >= len(l.text) }

func (l *lexer) skipTrivia(newlines bool) {
	for !l.done() {
		switch c := l.text[l.at]; {
		case c == ' ' || c == '\t' || c == '\r':
			l.at++
		case c == '\n' && newlines:
			l.at++
			l.line++
		case c == '#':
			for !l.done() && l.text[l.at] != '\n' {
				l.at++
			}
		default:
			return
		}
	}
}

func (l *lexer) tableHeader() error {
	start := l.line
	end := strings.IndexByte(l.text[l.at:], '\n')
	if end < 0 {
		end = len(l.text) - l.at
	}
	line := l.text[l.at : l.at+end]
	if comment := strings.IndexByte(line, '#'); comment >= 0 {
		line = line[:comment]
	}
	if !strings.HasSuffix(strings.TrimSpace(line), "]") {
		return fmt.Errorf("line %d: a table header has no closing `]`", start)
	}
	l.at += end
	return nil
}

func isBareKey(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (l *lexer) key() (string, int, error) {
	first, line, err := l.keyPart()
	if err != nil {
		return "", line, err
	}
	for {
		l.skipTrivia(false)
		if l.done() || l.text[l.at] != '.' {
			return first, line, nil
		}
		l.at++
		l.skipTrivia(false)
		if l.done() {
			return "", line, fmt.Errorf("line %d: a dotted key has nothing after the dot", line)
		}
		part, _, err := l.keyPart()
		if err != nil {
			return "", line, err
		}
		first += "." + part
	}
}

func (l *lexer) keyPart() (string, int, error) {
	line := l.line
	if c := l.text[l.at]; c == '"' || c == '\'' {
		s, err := l.basicOrLiteral()
		return s, line, err
	}
	start := l.at
	for !l.done() && isBareKey(l.text[l.at]) {
		l.at++
	}
	if start == l.at {
		return "", line, fmt.Errorf("line %d: expected a key, found %q", line, l.text[l.at])
	}
	return l.text[start:l.at], line, nil
}

func (l *lexer) value() (value, error) {
	line := l.line
	if l.done() {
		return value{}, fmt.Errorf("line %d: a key has no value", line)
	}
	switch c := l.text[l.at]; c {
	case '"', '\'':
		s, err := l.basicOrLiteral()
		return value{kind: stringValue, text: s, line: line}, err
	case '[':
		return l.array()
	case '{':
		return l.inlineTable()
	}
	start := l.at
	for !l.done() && !strings.ContainsRune(" \t\r\n#,]", rune(l.text[l.at])) {
		l.at++
	}
	if start == l.at {
		return value{}, fmt.Errorf("line %d: expected a value, found %q", line, l.text[l.at])
	}
	text := l.text[start:l.at]
	if !validScalar(text) {
		return value{}, fmt.Errorf("line %d: %q is not a TOML value", line, text)
	}
	return value{kind: scalarValue, text: text, line: line}, nil
}

func validScalar(text string) bool {
	switch text {
	case "true", "false", "inf", "+inf", "-inf", "nan", "+nan", "-nan":
		return true
	}
	c := text[0]
	return c >= '0' && c <= '9' || c == '+' || c == '-'
}

func (l *lexer) inlineTable() (value, error) {
	v := value{kind: tableValue, line: l.line}
	l.at++
	for {
		l.skipTrivia(false)
		if l.done() {
			return value{}, fmt.Errorf("line %d: an inline table has no closing `}`", v.line)
		}
		switch l.text[l.at] {
		case '}':
			l.at++
			return v, nil
		case ',':
			l.at++
			continue
		}
		key, _, err := l.key()
		if err != nil {
			return value{}, err
		}
		l.skipTrivia(false)
		if l.done() || l.text[l.at] != '=' {
			return value{}, fmt.Errorf("line %d: expected `=` after the key %q", l.line, key)
		}
		l.at++
		l.skipTrivia(false)
		if _, err := l.value(); err != nil {
			return value{}, err
		}
	}
}

func (l *lexer) array() (value, error) {
	v := value{kind: arrayValue, line: l.line}
	l.at++
	for {
		l.skipTrivia(true)
		if l.done() {
			return value{}, fmt.Errorf("line %d: an array has no closing `]`", v.line)
		}
		if l.text[l.at] == ']' {
			l.at++
			return v, nil
		}
		item, err := l.value()
		if err != nil {
			return value{}, err
		}
		v.items = append(v.items, item)
		l.skipTrivia(true)
		if l.done() {
			return value{}, fmt.Errorf("line %d: an array has no closing `]`", v.line)
		}
		switch l.text[l.at] {
		case ',':
			l.at++
		case ']':
		default:
			return value{}, fmt.Errorf("line %d: expected `,` or `]` in an array, found %q", l.line, l.text[l.at])
		}
	}
}

func (l *lexer) basicOrLiteral() (string, error) {
	line := l.line
	quote := l.text[l.at]
	if fence := string([]byte{quote, quote, quote}); strings.HasPrefix(l.text[l.at:], fence) {
		body := l.text[l.at+len(fence):]
		end := strings.Index(body, fence)
		if end < 0 {
			return "", fmt.Errorf("line %d: a multi-line string has no closing quotes", line)
		}
		l.at += 2*len(fence) + end
		l.line += strings.Count(body[:end], "\n")
		return body[:end], nil
	}
	l.at++
	var out strings.Builder
	for !l.done() {
		c := l.text[l.at]
		switch {
		case c == quote:
			l.at++
			return out.String(), nil
		case c == '\n':
			return "", fmt.Errorf("line %d: a string has no closing quote", line)
		case c == '\\' && quote == '"':
			r, width, err := unescape(l.text[l.at:])
			if err != nil {
				return "", fmt.Errorf("line %d: %w", line, err)
			}
			out.WriteRune(r)
			l.at += width
		default:
			out.WriteByte(c)
			l.at++
		}
	}
	return "", fmt.Errorf("line %d: a string has no closing quote", line)
}

func unescape(s string) (rune, int, error) {
	if len(s) < 2 {
		return 0, 0, errors.New("a string ends in a lone backslash")
	}
	simple := map[byte]rune{'b': '\b', 't': '\t', 'n': '\n', 'f': '\f', 'r': '\r', '"': '"', '\\': '\\'}
	if r, ok := simple[s[1]]; ok {
		return r, 2, nil
	}
	width := 0
	switch s[1] {
	case 'u':
		width = 4
	case 'U':
		width = 8
	default:
		return 0, 0, fmt.Errorf("unknown escape \\%c", s[1])
	}
	if len(s) < 2+width {
		return 0, 0, fmt.Errorf("a \\%c escape is cut short", s[1])
	}
	n, err := strconv.ParseUint(s[2:2+width], 16, 32)
	if err != nil || n > unicode.MaxRune || !utf8.ValidRune(rune(n)) {
		return 0, 0, fmt.Errorf("%q is not a Unicode escape", s[:2+width])
	}
	return rune(n), 2 + width, nil
}
