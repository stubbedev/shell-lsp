package parser

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

var byteOrderMark = []byte{0xef, 0xbb, 0xbf}

const (
	maxRecoveredTokens = 64
	maxUnrecoverable   = 32
)

type Span struct {
	Start, End int
}

func (s Span) Contains(offset int) bool {
	return s.Start <= offset && offset <= s.End
}

type ErrorKind uint8

const (
	ErrSyntax ErrorKind = iota
	ErrDialect
)

type Error struct {
	Span    Span
	Kind    ErrorKind
	Message string
}

type Tree struct {
	Dialect dialect.Dialect
	File    *syntax.File
	Errors  []Error
	Index
	text       string
	lineStarts []int
}

func Parse(text string, d dialect.Dialect) *Tree {
	lang := Language(d)
	if d == dialect.Bash && IsBats(text) {
		lang = syntax.LangBats
	}
	parser := syntax.NewParser(
		syntax.Variant(lang),
		syntax.KeepComments(true),
		syntax.RecoverErrors(maxRecoveredTokens),
	)
	src := []byte(text)
	if bytes.HasPrefix(src, byteOrderMark) {
		copy(src, "   ")
	}
	var errs []Error
	var file *syntax.File
	for range maxUnrecoverable {
		parsed, err := parser.Parse(bytes.NewReader(src), "")
		file = parsed
		if err == nil {
			break
		}
		parseErr, ok := convertError(err, text, d)
		if !ok {
			break
		}
		errs = append(errs, parseErr)
		if !blankLine(src, parseErr.Span.Start) {
			break
		}
	}
	index, recovered := buildIndex(file, text)
	errs = append(errs, recovered...)
	return &Tree{Dialect: d, File: file, Errors: sortErrors(errs), Index: index, text: text, lineStarts: lineStarts(text)}
}

func Language(d dialect.Dialect) syntax.LangVariant {
	switch d {
	case dialect.Bash, dialect.Posix, dialect.Dash, dialect.Busybox:
		return syntax.LangBash
	case dialect.Zsh:
		return syntax.LangZsh
	case dialect.Ksh:
		return syntax.LangMirBSDKorn
	}
	panic(fmt.Sprintf("parser: no language variant for dialect %d", d))
}

func IsBats(text string) bool {
	first, _, _ := strings.Cut(text, "\n")
	return strings.HasPrefix(first, "#!") && strings.Contains(first, "bats")
}

func convertError(err error, text string, d dialect.Dialect) (Error, bool) {
	if parseErr, ok := errors.AsType[syntax.ParseError](err); ok && parseErr.Pos.IsValid() {
		return refine(Error{Span: tokenSpan(text, offset(parseErr.Pos)), Kind: ErrSyntax, Message: parseErr.Text}, text, d), true
	}
	if langErr, ok := errors.AsType[syntax.LangError](err); ok && langErr.Pos.IsValid() {
		return refine(Error{Span: tokenSpan(text, offset(langErr.Pos)), Kind: ErrDialect, Message: dialectMessage(langErr, d)}, text, d), true
	}
	return Error{}, false
}

func dialectMessage(err syntax.LangError, d dialect.Dialect) string {
	var names strings.Builder
	for i, lang := range err.Langs {
		if i > 0 {
			names.WriteByte('/')
		}
		names.WriteString(lang.String())
	}
	verb := "is"
	if strings.HasSuffix(err.Feature, "s") {
		verb = "are"
	}
	return fmt.Sprintf("%s %s a %s feature, not available in %s", err.Feature, verb, names.String(), d)
}

func blankLine(src []byte, at int) bool {
	if at >= len(src) {
		return false
	}
	start := bytes.LastIndexByte(src[:at], '\n') + 1
	end := len(src)
	if i := bytes.IndexByte(src[at:], '\n'); i >= 0 {
		end = at + i
	}
	changed := false
	for i := start; i < end; i++ {
		if src[i] != ' ' {
			src[i] = ' '
			changed = true
		}
	}
	return changed
}

func tokenSpan(text string, start int) Span {
	start = min(start, len(text))
	end := start
	for end < len(text) && !isBlank(text[end]) {
		end++
	}
	if end > start {
		return Span{Start: start, End: end}
	}
	for start > 0 && !isBlank(text[start-1]) {
		start--
	}
	return Span{Start: start, End: end}
}

func isBlank(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n'
}

func sortErrors(errs []Error) []Error {
	slices.SortStableFunc(errs, func(a, b Error) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	return slices.CompactFunc(errs, func(a, b Error) bool {
		return a.Span.Start == b.Span.Start && a.Message == b.Message
	})
}

func Offset(p syntax.Pos) int {
	return offset(p)
}

func offset(p syntax.Pos) int {
	return int(p.Offset())
}

func (s Span) Covers(offset int) bool {
	return s.Start <= offset && offset < max(s.End, s.Start+1)
}

func lineStarts(text string) []int {
	starts := make([]int, 1, strings.Count(text, "\n")+1)
	for i := range len(text) {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}
