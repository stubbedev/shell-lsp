package nix

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

type tokenKind uint8

const (
	identToken tokenKind = iota + 1
	stringToken
	punctToken
	otherToken
)

type token struct {
	kind     tokenKind
	start    int
	end      int
	text     string
	indented bool
	comment  string
}

func (t token) is(punct string) bool { return t.kind == punctToken && t.text == punct }

func (t token) keyword(word string) bool { return t.kind == identToken && t.text == word }

func (t token) name() string {
	if i := strings.LastIndexByte(t.text, '.'); i >= 0 {
		return t.text[i+1:]
	}
	return t.text
}

type lexer struct {
	src     string
	tokens  []token
	comment string
}

func tokenize(src string) []token {
	l := &lexer{src: src}
	scan.Forward(0, len(src), l.next)
	return l.tokens
}

func (l *lexer) emit(kind tokenKind, start, end int) int {
	t := token{kind: kind, start: start, end: end, comment: l.comment}
	if kind != stringToken {
		t.text = l.src[start:end]
	}
	l.tokens = append(l.tokens, t)
	l.comment = ""
	return end
}

func (l *lexer) next(i int) int {
	src := l.src
	c := src[i]
	switch {
	case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		return i + 1
	case c == '#':
		end := lineEnd(src, i)
		l.comment = src[i+1 : end]
		return end
	case strings.HasPrefix(src[i:], "/*"):
		end := strings.Index(src[i+2:], "*/")
		if end < 0 {
			return len(src)
		}
		l.comment = src[i+2 : i+2+end]
		return i + 2 + end + 2
	case c == '"':
		end := quotedEnd(src, i+1)
		l.tokens = append(l.tokens, token{kind: stringToken, start: i, end: end, comment: l.comment})
		l.comment = ""
		return end
	case strings.HasPrefix(src[i:], "''"):
		end := indentedEnd(src, i+2)
		l.tokens = append(l.tokens, token{kind: stringToken, start: i, end: end, indented: true, comment: l.comment})
		l.comment = ""
		return end
	case identStart(c):
		end := i
		for end < len(src) && (identByte(src[end]) || src[end] == '.' && end+1 < len(src) && identStart(src[end+1])) {
			end++
		}
		return l.emit(identToken, i, end)
	case strings.HasPrefix(src[i:], "++"):
		return l.emit(punctToken, i, i+2)
	case pathPrefix(src[i:]) > 0:
		end := i + pathPrefix(src[i:])
		for end < len(src) && pathByte(src[end]) {
			end++
		}
		return l.emit(otherToken, i, end)
	case strings.ContainsRune("{}()[]=;:+,@?", rune(c)):
		return l.emit(punctToken, i, i+1)
	}
	end := i + 1
	for end < len(src) && !strings.ContainsRune(" \t\r\n#\"{}()[]=;:+,@?", rune(src[end])) && !strings.HasPrefix(src[end:], "''") {
		end++
	}
	return l.emit(otherToken, i, end)
}

func lineEnd(src string, i int) int {
	if end := strings.IndexByte(src[i:], '\n'); end >= 0 {
		return i + end
	}
	return len(src)
}

func identStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func identByte(c byte) bool {
	return identStart(c) || c >= '0' && c <= '9' || c == '\'' || c == '-'
}

func pathPrefix(s string) int {
	for _, prefix := range []string{"./", "../", "~/", "/"} {
		if strings.HasPrefix(s, prefix) && len(s) > len(prefix) && pathByte(s[len(prefix)]) {
			return len(prefix)
		}
	}
	return 0
}

func pathByte(c byte) bool {
	return identByte(c) || c == '.' || c == '/' || c == '+'
}

func quotedEnd(src string, start int) int {
	end := len(src)
	scan.Forward(start, len(src), func(i int) int {
		switch {
		case src[i] == '\\':
			return i + 2
		case src[i] == '"':
			end = i + 1
			return len(src)
		case strings.HasPrefix(src[i:], "${"):
			return antiquoteEnd(src, i+2)
		case src[i] == '$' && i+1 < len(src) && src[i+1] != '"' && src[i+1] != '\\':
			return i + 2
		}
		return i + 1
	})
	return end
}

func indentedEnd(src string, start int) int {
	end := len(src)
	scan.Forward(start, len(src), func(i int) int {
		switch {
		case strings.HasPrefix(src[i:], "'''"), strings.HasPrefix(src[i:], "''$"):
			return i + 3
		case strings.HasPrefix(src[i:], "''\\"):
			return i + 4
		case strings.HasPrefix(src[i:], "''"):
			end = i + 2
			return len(src)
		case strings.HasPrefix(src[i:], "${"):
			return antiquoteEnd(src, i+2)
		case src[i] == '$' && i+1 < len(src) && src[i+1] != '\'':
			return i + 2
		}
		return i + 1
	})
	return end
}

func antiquoteEnd(src string, start int) int {
	end, depth := len(src), 1
	scan.Forward(start, len(src), func(i int) int {
		switch c := src[i]; {
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				end = i + 1
				return len(src)
			}
		case c == '"':
			return quotedEnd(src, i+1)
		case strings.HasPrefix(src[i:], "''"):
			return indentedEnd(src, i+2)
		case c == '#':
			return lineEnd(src, i)
		case strings.HasPrefix(src[i:], "/*"):
			if closing := strings.Index(src[i+2:], "*/"); closing >= 0 {
				return i + 2 + closing + 2
			}
			return len(src)
		}
		return i + 1
	})
	return end
}
