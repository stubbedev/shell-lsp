package assist

import (
	"iter"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/scan"
)

const (
	schemeSeparator = "://"
	subDelims       = "!$&'()*+,;="
	proseTrailers   = ".,;:!?'"
)

func webURLs(text string) iter.Seq[parser.Span] {
	return func(yield func(parser.Span) bool) {
		for from := 0; ; {
			found := strings.Index(text[from:], schemeSeparator)
			if found < 0 {
				return
			}
			separator := from + found
			authority := separator + len(schemeSeparator)
			from = authority
			start, ok := webScheme(text, separator)
			if !ok {
				continue
			}
			end := iriEnd(text, authority)
			from = max(from, end)
			end = start + trimProse(text[start:end], authority-start)
			if end == authority {
				continue
			}
			if !yield(parser.Span{Start: start, End: end}) {
				return
			}
		}
	}
}

func webScheme(text string, separator int) (int, bool) {
	for _, scheme := range [...]string{"https", "http"} {
		start := separator - len(scheme)
		if start >= 0 && strings.EqualFold(text[start:separator], scheme) {
			return start, start == 0 || !schemeChar(text[start-1])
		}
	}
	return 0, false
}

func schemeChar(c byte) bool {
	return 'a' <= c|0x20 && c|0x20 <= 'z' || scan.Digit(c) || c == '+' || c == '-' || c == '.'
}

func iriEnd(text string, authority int) int {
	at := authorityEnd(text, authority)
	if at == authority {
		return authority
	}
	fragment := false
	for at < len(text) {
		switch next, ok := pchar(text, at); {
		case ok:
			at = next
		case text[at] == '/' || text[at] == '?':
			at++
		case text[at] == '#' && !fragment:
			fragment = true
			at++
		default:
			return at
		}
	}
	return at
}

func authorityEnd(text string, at int) int {
	for at < len(text) {
		if text[at] == '[' && (at == 0 || text[at-1] == '/' || text[at-1] == '@') {
			literal, ok := ipLiteralEnd(text, at)
			if !ok {
				return at
			}
			at = literal
			continue
		}
		next, ok := pchar(text, at)
		if !ok {
			return at
		}
		at = next
	}
	return at
}

func ipLiteralEnd(text string, open int) (int, bool) {
	for at := open + 1; at < len(text); at++ {
		c := text[at]
		switch {
		case c == ']':
			return at + 1, at > open+1
		case !unreserved(c) && c != ':' && strings.IndexByte(subDelims, c) < 0:
			return open, false
		}
	}
	return open, false
}

func pchar(text string, at int) (int, bool) {
	c := text[at]
	switch {
	case unreserved(c) || c == ':' || c == '@' || strings.IndexByte(subDelims, c) >= 0:
		return at + 1, true
	case c == '%':
		if at+2 < len(text) && hexDigit(text[at+1]) && hexDigit(text[at+2]) {
			return at + 3, true
		}
		return at, false
	case c >= utf8.RuneSelf:
		if r, size := utf8.DecodeRuneInString(text[at:]); ucschar(r) {
			return at + size, true
		}
	}
	return at, false
}

func unreserved(c byte) bool {
	return 'a' <= c|0x20 && c|0x20 <= 'z' || scan.Digit(c) || c == '-' || c == '.' || c == '_' || c == '~'
}

func hexDigit(c byte) bool { return scan.Digit(c) || 'a' <= c|0x20 && c|0x20 <= 'f' }

func ucschar(r rune) bool {
	switch {
	case 0xA0 <= r && r <= 0xD7FF, 0xF900 <= r && r <= 0xFDCF, 0xFDF0 <= r && r <= 0xFFEF:
		return true
	case r < 0x10000 || r > 0xEFFFD || 0xE0000 <= r && r < 0xE1000:
		return false
	}
	return r&0xFFFF <= 0xFFFD
}

func trimProse(url string, minimum int) int {
	end := len(url)
	for end > minimum {
		last := url[end-1]
		unbalanced := last == ')' && strings.Count(url[:end], "(") < strings.Count(url[:end], ")")
		if !unbalanced && strings.IndexByte(proseTrailers, last) < 0 {
			break
		}
		end--
	}
	return end
}
