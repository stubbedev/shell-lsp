package lang

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Kind uint8

const (
	Keyword Kind = iota
	String
	Number
	Regexp
	Variable
	Parameter
	Function
	Property
	Operator
	Comment
)

var kindNames = [...]string{"keyword", "string", "number", "regexp", "variable", "parameter", "function", "property", "operator", "comment"}

func (k Kind) String() string { return kindNames[k] }

type Modifier uint8

const (
	Builtin Modifier = 1 << iota
	Declaration
)

type Token struct {
	Start, End int
	Kind       Kind
	Modifiers  Modifier
}

type Error struct {
	Start, End int
	Message    string
}

const MaxErrors = 10

type Result struct {
	Tokens []Token
	Errors []Error
}

func NewResult(src string) Result {
	return Result{Tokens: make([]Token, 0, len(src)/3+1)}
}

func (r *Result) Emit(start, end int, kind Kind, mods Modifier) int {
	if end <= start {
		return -1
	}
	r.Tokens = append(r.Tokens, Token{Start: start, End: end, Kind: kind, Modifiers: mods})
	return len(r.Tokens) - 1
}

func (r *Result) Fail(start, end int, message string) {
	e := Error{Start: start, End: max(end, start), Message: message}
	if len(r.Errors) == MaxErrors || slices.ContainsFunc(r.Errors, e.overlaps) {
		return
	}
	at, _ := slices.BinarySearchFunc(r.Errors, e, func(a, b Error) int { return cmp.Compare(a.Start, b.Start) })
	r.Errors = slices.Insert(r.Errors, at, e)
}

func (e Error) overlaps(other Error) bool {
	return e.Start < max(other.End, other.Start+1) && other.Start < max(e.End, e.Start+1)
}

func (r *Result) TokenAt(offset int) (Token, bool) {
	lo, hi := 0, len(r.Tokens)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if r.Tokens[mid].End <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(r.Tokens) && r.Tokens[lo].Start <= offset {
		return r.Tokens[lo], true
	}
	return Token{}, false
}

const quotedWidth = 24

func Quote(src string, start, end int) string {
	if start >= len(src) {
		return "end of program"
	}
	text := src[start:min(end, len(src))]
	if text == "" || text == "\n" {
		return "end of line"
	}
	if cut := strings.IndexByte(text, '\n'); cut >= 0 {
		text = text[:cut]
	}
	if len(text) > quotedWidth {
		text = text[:quotedWidth] + "…"
	}
	if strings.Contains(text, "`") {
		return strconv.Quote(text)
	}
	return "`" + text + "`"
}

func Unexpected(src string, start, end int) string {
	return "unexpected " + Quote(src, start, end)
}

func Illegal(src string, start, end int) string {
	return "unexpected character " + Quote(src, start, end)
}

func RuneEnd(src string, start int) int {
	if start >= len(src) {
		return start
	}
	_, size := utf8.DecodeRuneInString(src[start:])
	return start + size
}

func Unclosed(src string, start, end int) string {
	return Quote(src, start, end) + " is never closed"
}

type Doc struct {
	Signature string
	Summary   string
}

func (d Doc) Markdown(language string) string {
	return "```" + language + "\n" + d.Signature + "\n```\n\n" + d.Summary
}

type Docs map[string][2]string

func (d Docs) Lookup(name string) (Doc, bool) {
	entry, ok := d[name]
	return Doc{Signature: entry[0], Summary: entry[1]}, ok
}

func IsDigit(c byte) bool { return c >= '0' && c <= '9' }

func IsHexDigit(c byte) bool { return IsDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'f') }

func IsNameStart(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') }

func IsNameChar(c byte) bool { return IsNameStart(c) || IsDigit(c) }
