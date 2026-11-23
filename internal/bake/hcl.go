package bake

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind uint8

const (
	tokenIdent tokenKind = iota + 1
	tokenString
	tokenNewline
	tokenPunct
	tokenOther
)

type token struct {
	kind  tokenKind
	text  string
	known bool
	pos   int
}

func (t token) is(punct string) bool { return t.kind == tokenPunct && t.text == punct }

const (
	structural      = "={}[](),:"
	targetBlock     = "target"
	heredocOpen     = "<<"
	interpolation   = "${"
	directive       = "%{"
	hexQuad         = 4
	hexOctet        = 8
	escapedTemplate = 3
	lineComment     = "//"
	commentOpen     = "/*"
	commentClose    = "*/"
)

var operators = []string{"==", "=>", "!=", "<=", ">="}

func operator(text string) string {
	for _, op := range operators {
		if strings.HasPrefix(text, op) {
			return op
		}
	}
	return ""
}

type syntaxError struct {
	line, column int
	message      string
}

func (e *syntaxError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.line, e.column, e.message)
}

func failAt(src string, pos int, format string, args ...any) error {
	before := src[:min(pos, len(src))]
	line := strings.Count(before, "\n") + 1
	column := utf8.RuneCountInString(before[strings.LastIndexByte(before, '\n')+1:]) + 1
	return &syntaxError{line: line, column: column, message: fmt.Sprintf(format, args...)}
}

type lexer struct {
	src string
	pos int
}

func lex(src string) ([]token, error) {
	l := &lexer{src: src}
	var out []token
	for {
		t, ok, err := l.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, t)
	}
}

func (l *lexer) fail(pos int, format string, args ...any) error {
	return failAt(l.src, pos, format, args...)
}

func identStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }

func identPart(r rune) bool { return identStart(r) || r == '-' || unicode.IsDigit(r) }

func (l *lexer) next() (token, bool, error) {
	for l.pos < len(l.src) {
		start := l.pos
		rest := l.src[start:]
		r, size := utf8.DecodeRuneInString(rest)
		op := operator(rest)
		switch {
		case r == ' ' || r == '\t' || r == '\r':
			l.pos += size
		case r == '\n':
			l.pos++
			return token{kind: tokenNewline, pos: start}, true, nil
		case r == '#' || strings.HasPrefix(rest, lineComment):
			l.pos += lineLength(rest)
		case strings.HasPrefix(rest, commentOpen):
			end := strings.Index(rest[len(commentOpen):], commentClose)
			if end < 0 {
				return token{}, false, l.fail(start, "the comment is never closed")
			}
			l.pos += len(commentOpen) + end + len(commentClose)
		case r == '"':
			return l.quoted()
		case strings.HasPrefix(rest, heredocOpen) && l.heredocAhead():
			return l.heredoc()
		case identStart(r):
			l.pos += size
			for l.pos < len(l.src) {
				next, n := utf8.DecodeRuneInString(l.src[l.pos:])
				if !identPart(next) {
					break
				}
				l.pos += n
			}
			return token{kind: tokenIdent, text: l.src[start:l.pos], pos: start}, true, nil
		case op != "":
			l.pos += len(op)
			return token{kind: tokenOther, text: op, pos: start}, true, nil
		case strings.ContainsRune(structural, r):
			l.pos++
			return token{kind: tokenPunct, text: rest[:1], pos: start}, true, nil
		default:
			l.pos += size
			return token{kind: tokenOther, text: rest[:size], pos: start}, true, nil
		}
	}
	return token{}, false, nil
}

func lineLength(text string) int {
	if end := strings.IndexByte(text, '\n'); end >= 0 {
		return end
	}
	return len(text)
}

func (l *lexer) quoted() (token, bool, error) {
	start := l.pos
	l.pos++
	var sb strings.Builder
	known := true
	for {
		if l.pos >= len(l.src) || l.src[l.pos] == '\n' {
			return token{}, false, l.fail(start, "the string is never closed")
		}
		rest := l.src[l.pos:]
		escaped, interpolates := templateMarker(rest)
		switch {
		case rest[0] == '"':
			l.pos++
			return token{kind: tokenString, text: sb.String(), known: known, pos: start}, true, nil
		case rest[0] == '\\':
			decoded, err := l.escape()
			if err != nil {
				return token{}, false, err
			}
			sb.WriteString(decoded)
		case escaped != "":
			sb.WriteString(escaped)
			l.pos += escapedTemplate
		case interpolates:
			known = false
			if err := l.template(); err != nil {
				return token{}, false, err
			}
		default:
			sb.WriteByte(rest[0])
			l.pos++
		}
	}
}

func templateMarker(rest string) (string, bool) {
	switch {
	case strings.HasPrefix(rest, "$"+interpolation), strings.HasPrefix(rest, "%"+directive):
		return rest[1:escapedTemplate], false
	case strings.HasPrefix(rest, interpolation), strings.HasPrefix(rest, directive):
		return "", true
	}
	return "", false
}

func (l *lexer) escape() (string, error) {
	start := l.pos
	if l.pos+1 >= len(l.src) {
		return "", l.fail(start, "the string ends inside an escape sequence")
	}
	c := l.src[l.pos+1]
	l.pos += 2
	switch c {
	case 'n':
		return "\n", nil
	case 'r':
		return "\r", nil
	case 't':
		return "\t", nil
	case '"', '\\':
		return string(c), nil
	case 'u':
		return l.codePoint(start, hexQuad)
	case 'U':
		return l.codePoint(start, hexOctet)
	}
	return "", l.fail(start, "\\%c is no escape sequence", c)
}

func (l *lexer) codePoint(start, digits int) (string, error) {
	if l.pos+digits > len(l.src) {
		return "", l.fail(start, "the escape sequence needs %d hexadecimal digits", digits)
	}
	n, err := strconv.ParseUint(l.src[l.pos:l.pos+digits], 16, 32)
	if err != nil || !utf8.ValidRune(rune(n)) {
		return "", l.fail(start, "the escape sequence needs %d hexadecimal digits naming a character", digits)
	}
	l.pos += digits
	return string(rune(n)), nil
}

func (l *lexer) template() error {
	start := l.pos
	l.pos += len(interpolation)
	depth := 0
	for {
		t, ok, err := l.next()
		if err != nil {
			return err
		}
		switch {
		case !ok:
			return l.fail(start, "the template sequence is never closed")
		case t.is("{"):
			depth++
		case t.is("}") && depth == 0:
			return nil
		case t.is("}"):
			depth--
		}
	}
}

func (l *lexer) heredocMarker() (string, bool) {
	rest := strings.TrimPrefix(l.src[l.pos+len(heredocOpen):], "-")
	r, size := utf8.DecodeRuneInString(rest)
	if !identStart(r) {
		return "", false
	}
	end := size
	for end < len(rest) {
		next, n := utf8.DecodeRuneInString(rest[end:])
		if !identPart(next) {
			break
		}
		end += n
	}
	after := strings.TrimPrefix(rest[end:], "\r")
	return rest[:end], after == "" || after[0] == '\n'
}

func (l *lexer) heredocAhead() bool {
	_, ok := l.heredocMarker()
	return ok
}

func (l *lexer) heredoc() (token, bool, error) {
	start := l.pos
	marker, _ := l.heredocMarker()
	l.pos += lineLength(l.src[l.pos:])
	for l.pos < len(l.src) {
		l.pos++
		line := l.src[l.pos : l.pos+lineLength(l.src[l.pos:])]
		l.pos += len(line)
		if strings.TrimSpace(line) == marker {
			return token{kind: tokenString, pos: start}, true, nil
		}
	}
	return token{}, false, l.fail(start, "the heredoc is never closed by a line with %s", marker)
}

func parseHCL(src string) ([]target, error) {
	tokens, err := lex(src)
	if err != nil {
		return nil, err
	}
	items, err := body(src, tokens)
	if err != nil {
		return nil, err
	}
	var out []target
	for _, it := range items {
		if !it.block || it.name != targetBlock || len(it.labels) != 1 {
			continue
		}
		label := it.labels[0]
		if label.kind == tokenString && !label.known {
			continue
		}
		t, err := targetOf(src, label.text, it.value)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

type item struct {
	name   string
	labels []token
	block  bool
	value  []token
}

func body(src string, tokens []token) ([]item, error) {
	lines, err := split(src, tokens, func(t token) bool { return t.kind == tokenNewline })
	if err != nil {
		return nil, err
	}
	var out []item
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		head := line[0]
		if head.kind != tokenIdent {
			return nil, failAt(src, head.pos, "expected an attribute name or a block type")
		}
		if len(line) > 1 && line[1].is("=") {
			out = append(out, item{name: head.text, value: line[2:]})
			continue
		}
		open := 1
		for open < len(line) && (line[open].kind == tokenString || line[open].kind == tokenIdent) {
			open++
		}
		if open == len(line) || !line[open].is("{") || matching(line, open) != len(line)-1 {
			return nil, failAt(src, head.pos, "`%s` is neither an attribute nor a block", head.text)
		}
		out = append(out, item{name: head.text, labels: line[1:open], block: true, value: line[open+1 : len(line)-1]})
	}
	return out, nil
}

func opens(t token) bool { return t.is("{") || t.is("[") || t.is("(") }

func closes(t token) bool { return t.is("}") || t.is("]") || t.is(")") }

func matching(tokens []token, open int) int {
	depth := 0
	for i := open; i < len(tokens); i++ {
		switch {
		case opens(tokens[i]):
			depth++
		case closes(tokens[i]):
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func split(src string, tokens []token, separator func(token) bool) ([][]token, error) {
	var out [][]token
	var stack []token
	start := 0
	for i, t := range tokens {
		switch {
		case opens(t):
			stack = append(stack, t)
		case closes(t):
			if len(stack) == 0 || !pair(stack[len(stack)-1], t) {
				return nil, failAt(src, t.pos, "`%s` closes nothing", t.text)
			}
			stack = stack[:len(stack)-1]
		case len(stack) == 0 && separator(t):
			out = append(out, tokens[start:i])
			start = i + 1
		}
	}
	if len(stack) > 0 {
		open := stack[len(stack)-1]
		return nil, failAt(src, open.pos, "`%s` is never closed", open.text)
	}
	return append(out, tokens[start:]), nil
}

func pair(open, close token) bool {
	return strings.Index("{[(", open.text) == strings.Index("}])", close.text)
}

func targetOf(src, name string, tokens []token) (target, error) {
	items, err := body(src, tokens)
	if err != nil {
		return target{}, err
	}
	t := target{Name: name}
	for _, it := range items {
		if it.block {
			continue
		}
		switch it.name {
		case attrContext:
			v := literal(it.value)
			t.Context = &v
		case attrDockerfile:
			v := literal(it.value)
			t.Dockerfile = &v
		case attrContexts:
			if t.Contexts, err = object(src, it.value); err != nil {
				return target{}, err
			}
		case attrInherits:
			if t.Inherits, err = names(src, it.value); err != nil {
				return target{}, err
			}
		}
	}
	return t, nil
}

func literal(tokens []token) Value {
	if len(tokens) != 1 || tokens[0].kind != tokenString || !tokens[0].known {
		return Value{}
	}
	return Value{Text: tokens[0].text, Known: true}
}

func bracketed(tokens []token, open, close string) ([]token, bool) {
	n := len(tokens)
	if n < 2 || !tokens[0].is(open) || !tokens[n-1].is(close) || matching(tokens, 0) != n-1 {
		return nil, false
	}
	return tokens[1 : n-1], true
}

func entries(src string, tokens []token) ([][]token, error) {
	parts, err := split(src, tokens, func(t token) bool { return t.kind == tokenNewline || t.is(",") })
	if err != nil {
		return nil, err
	}
	out := parts[:0]
	for _, p := range parts {
		if len(p) > 0 {
			out = append(out, p)
		}
	}
	return out, nil
}

func object(src string, tokens []token) (map[string]Value, error) {
	inner, ok := bracketed(tokens, "{", "}")
	if !ok {
		return nil, nil
	}
	parts, err := entries(src, inner)
	if err != nil {
		return nil, err
	}
	out := map[string]Value{}
	for _, p := range parts {
		if len(p) < 2 || !p[1].is("=") && !p[1].is(":") {
			continue
		}
		key := p[0]
		if key.kind == tokenIdent || key.kind == tokenString && key.known {
			out[key.text] = literal(p[2:])
		}
	}
	return out, nil
}

func names(src string, tokens []token) ([]string, error) {
	inner, ok := bracketed(tokens, "[", "]")
	if !ok {
		return nil, nil
	}
	parts, err := entries(src, inner)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range parts {
		if v := literal(p); v.Known {
			out = append(out, v.Text)
		}
	}
	return out, nil
}
