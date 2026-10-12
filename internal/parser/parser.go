package parser

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var byteOrderMark = []byte{0xef, 0xbb, 0xbf}

const (
	maxRecoveredTokens = 64
	maxUnrecoverable   = 32
)

type Span struct {
	Start, End int
}

func (s Span) Touches(offset int) bool {
	return s.Start <= offset && offset <= s.End
}

func (s Span) HasByteAt(offset int) bool {
	return s.Start <= offset && offset < s.End
}

func (s Span) HasByteBefore(offset int) bool {
	return s.Start < offset && offset <= s.End
}

func (s Span) Covers(offset int) bool {
	return s.HasByteAt(offset) || s.Start == s.End && offset == s.Start
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
	dialect.Shell
	File   *syntax.File
	Errors []Error
	Index
	text       string
	lineStarts []int
	origin     Origin
}

type Origin interface {
	SourceLine(offset int) int
	Synthetic(offset int) bool
}

func Parse(text string, d dialect.Dialect) *Tree {
	return ParseEdit(nil, text, dialect.ShellOf(d, dialect.Input{Text: text}))
}

func (t *Tree) Runners() Wrappers { return Wrappers{Bats: t.Bats(), Programs: true} }

func newSyntaxParser(lang syntax.LangVariant, options ...syntax.ParserOption) *syntax.Parser {
	return syntax.NewParser(append([]syntax.ParserOption{
		syntax.Variant(lang),
		syntax.KeepComments(true),
		syntax.RecoverErrors(maxRecoveredTokens),
	}, options...)...)
}

func ParseEdit(previous *Tree, text string, shell dialect.Shell) *Tree {
	src := []byte(text)
	if bytes.HasPrefix(src, byteOrderMark) {
		copy(src, "   ")
	}
	file, errs, closedAtEnd := parseSource(newSyntaxParser(shell.Lang()), src, 0, text, shell.Dialect)
	return newTree(previous, file, errs, closedAtEnd, text, shell, nil)
}

func ParseScripts(previous *Tree, text string, scripts []Span, shell dialect.Shell, origin Origin) *Tree {
	starts := lineStarts(text)
	file := &syntax.File{}
	var errs []Error
	closedAtEnd := map[*syntax.Redirect]bool{}
	for _, script := range scripts {
		line := sort.SearchInts(starts, script.Start+1)
		at := syntax.StartAt(uint(script.Start), uint(line), uint(script.Start-starts[line-1]+1))
		part, partErrs, closed := parseSource(newSyntaxParser(shell.Lang(), at), []byte(text[script.Start:script.End]), script.Start, text[:script.End], shell.Dialect)
		file.Stmts = append(file.Stmts, part.Stmts...)
		file.Last = append(file.Last, part.Last...)
		errs = append(errs, partErrs...)
		maps.Copy(closedAtEnd, closed)
	}
	return newTree(previous, file, errs, closedAtEnd, text, shell, origin)
}

func newTree(previous *Tree, file *syntax.File, errs []Error, closedAtEnd map[*syntax.Redirect]bool, text string, shell dialect.Shell, origin Origin) *Tree {
	index, recovered := buildIndex(file, shell.Lang(), shell.Dialect, text, closedAtEnd, previous)
	errs = append(errs, recovered...)
	return &Tree{Shell: shell, File: file, Errors: sortErrors(errs), Index: index, text: text, lineStarts: lineStarts(text), origin: origin}
}

func parseSource(parser *syntax.Parser, src []byte, base int, text string, d dialect.Dialect) (*syntax.File, []Error, map[*syntax.Redirect]bool) {
	var errs []Error
	var file *syntax.File
	parsedSrc := src
	closedAt := -1
	for range maxUnrecoverable {
		parsed, err := parser.Parse(bytes.NewReader(src), "")
		if len(src) > len(text)-base && parsedPastText(parsed, err, text) {
			break
		}
		file, parsedSrc = parsed, src
		if err == nil {
			break
		}
		parseErr, located := convertError(err, text, d)
		at := parseErr.Span.Start
		delimiter, unclosed := unclosedHeredocDelimiter(err)
		if unclosed && at == closedAt {
			break
		}
		errs = append(errs, parseErr)
		if !located {
			break
		}
		if unclosed {
			src, closedAt = endAtEndOfFile(src, delimiter), at
			continue
		}
		if at >= len(text) || !blankLine(src, at-base) {
			break
		}
	}
	var closedAtEnd map[*syntax.Redirect]bool
	if len(parsedSrc) > len(text)-base {
		closedAtEnd = closeAtEndOfText(file, text)
	}
	return file, errs, closedAtEnd
}

func parsedPastText(file *syntax.File, err error, text string) bool {
	if parseErr, ok := errors.AsType[syntax.ParseError](err); ok && parseErr.Pos.IsValid() && offset(parseErr.Pos) >= len(text) {
		return true
	}
	if langErr, ok := errors.AsType[syntax.LangError](err); ok && langErr.Pos.IsValid() && offset(langErr.Pos) >= len(text) {
		return true
	}
	return outsideHeredocsPastText(file, text)
}

func outsideHeredocsPastText(file *syntax.File, text string) bool {
	end := uint(len(text))
	bodies := map[*syntax.Word]bool{}
	past := false
	Walk(file, func(n syntax.Node) bool {
		switch n := n.(type) {
		case nil:
			return true
		case *syntax.Redirect:
			if n.Hdoc != nil {
				bodies[n.Hdoc] = true
			}
		case *syntax.Word:
			if bodies[n] {
				return false
			}
			past = endsPast(n, end)
		case *syntax.Comment:
			past = endsPast(n, end)
		}
		if past {
			return false
		}
		if pos := n.Pos(); pos.IsValid() && pos.Offset() >= end {
			past = true
		}
		return !past
	})
	return past
}

func endsPast(n syntax.Node, end uint) bool {
	nodeEnd := n.End()
	return nodeEnd.IsValid() && nodeEnd.Offset() > end
}

func unclosedHeredocDelimiter(err error) (string, bool) {
	parseErr, ok := errors.AsType[syntax.ParseError](err)
	if !ok {
		return "", false
	}
	return quotedToken(parseErr.Text, unclosedHeredocPrefix)
}

func endAtEndOfFile(src []byte, delimiter string) []byte {
	if len(src) > 0 && src[len(src)-1] != '\n' {
		src = append(src, '\n')
	}
	return append(src, delimiter...)
}

func closeAtEndOfText(file *syntax.File, text string) map[*syntax.Redirect]bool {
	end := uint(len(text))
	closed := map[*syntax.Redirect]bool{}
	Walk(file, func(n syntax.Node) bool {
		r, ok := n.(*syntax.Redirect)
		if !ok || r.Hdoc == nil || r.Hdoc.End().Offset() <= end {
			return true
		}
		closed[r] = true
		r.Hdoc.Parts = partsWithinText(r.Hdoc.Parts, text)
		if len(r.Hdoc.Parts) == 0 {
			r.Hdoc = nil
		}
		return true
	})
	return closed
}

func partsWithinText(parts []syntax.WordPart, text string) []syntax.WordPart {
	end := uint(len(text))
	for len(parts) > 0 && parts[len(parts)-1].Pos().Offset() >= end {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 || parts[len(parts)-1].End().Offset() <= end {
		return parts
	}
	last, ok := parts[len(parts)-1].(*syntax.Lit)
	if !ok {
		panic(fmt.Sprintf("parser: here-document part %T runs past the end of the text", parts[len(parts)-1]))
	}
	lineStart := textline.Start(text, len(text))
	if lineStart < len(text) {
		last.Value = strings.TrimSuffix(last.Value, "\n")
	}
	last.ValueEnd = syntax.NewPos(end, uint(strings.Count(text, "\n")+1), uint(len(text)-lineStart+1))
	return parts
}

func convertError(err error, text string, d dialect.Dialect) (Error, bool) {
	if parseErr, ok := errors.AsType[syntax.ParseError](err); ok && parseErr.Pos.IsValid() {
		return refine(Error{Span: tokenSpan(text, offset(parseErr.Pos)), Kind: ErrSyntax, Message: parseErr.Text}, text, d), true
	}
	if langErr, ok := errors.AsType[syntax.LangError](err); ok && langErr.Pos.IsValid() {
		return refine(Error{Span: tokenSpan(text, offset(langErr.Pos)), Kind: ErrDialect, Message: dialectMessage(langErr, d)}, text, d), true
	}
	return Error{Span: tokenSpan(text, 0), Kind: ErrSyntax, Message: err.Error()}, false
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

func lineStarts(text string) []int {
	starts := make([]int, 1, strings.Count(text, "\n")+1)
	for i := range len(text) {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}
