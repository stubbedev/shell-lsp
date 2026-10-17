package justfile

import (
	"fmt"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

type lexer struct {
	f      *File
	r      *reader
	erred  map[int]bool
	colon  bool
	indent string
	inBody bool
	open   []token
	levels []string
}

var closers = map[string]string{")": "(", "]": "[", "}": "{"}

func (f *File) lexicalErrors() {
	l := &lexer{f: f, r: &reader{text: f.Text, file: &File{Text: f.Text}, lexing: true}, erred: map[int]bool{}, levels: []string{""}}
	for _, e := range f.Errors {
		l.erred[f.line(e.Span.Start)] = true
	}
	for l.r.pos < len(l.r.text) {
		start := l.r.pos
		end := l.r.lineEnd(start)
		line := l.r.lineText(start, end)
		if blank(line) {
			l.r.pos = end + 1
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if indent != "" && l.inBody {
			if body := l.levels[len(l.levels)-1]; !strings.HasPrefix(indent, body) {
				l.report(Error{Code: lint.CodeJustIndentation, Span: Span{Start: start, End: start + len(indent)}, Message: "recipe line has inconsistent leading whitespace: it started with " + whitespace(body) + " but this line has " + whitespace(indent)})
			}
			l.body(end)
			continue
		}
		if strings.Contains(indent, " ") && strings.Contains(indent, "\t") {
			l.lexError(Span{Start: start, End: start + len(indent)}, "leading whitespace mixes tabs and spaces: "+whitespace(indent))
		}
		if indent != "" && l.colon && len(indent) > len(l.indent) && strings.HasPrefix(indent, l.indent) {
			l.inBody, l.colon = true, false
			l.levels = append(l.levels, indent)
			l.body(end)
			continue
		}
		if len(indent) < len(l.indent) {
			l.colon = false
		}
		l.level(Span{Start: start, End: start + len(indent)}, indent)
		l.inBody, l.indent = false, indent
		l.tokens()
	}
	for _, e := range l.r.file.Errors {
		l.report(e)
	}
}

func (l *lexer) level(span Span, indent string) {
	top := l.levels[len(l.levels)-1]
	if len(indent) > len(top) {
		if !strings.HasPrefix(indent, top) {
			l.lexError(span, "inconsistent leading whitespace: the lines above start with "+whitespace(top))
		}
		l.levels = append(l.levels, indent)
		return
	}
	for len(l.levels) > 1 && len(l.levels[len(l.levels)-1]) > len(indent) {
		l.levels = l.levels[:len(l.levels)-1]
	}
	if l.levels[len(l.levels)-1] != indent {
		l.lexError(span, "inconsistent leading whitespace: the lines above start with "+whitespace(top))
	}
}

func (l *lexer) lexError(span Span, message string) {
	l.report(Error{Code: lint.CodeJustSyntax, Span: span, Message: message})
}

func (l *lexer) report(e Error) {
	if line := l.f.line(e.Span.Start); !l.erred[line] {
		l.erred[line] = true
		l.f.Errors = append(l.f.Errors, e)
	}
}

func (l *lexer) tokens() {
	for {
		t, ok := l.r.token()
		if !ok {
			return
		}
		if t.kind == tokEOF || t.kind == tokNewline {
			return
		}
		l.token(t)
		l.r.depth = len(l.open)
	}
}

func (l *lexer) token(t token) {
	switch {
	case t.kind == tokError && t.text != "#":
		l.lexError(t.span, "unknown start of token `"+t.text+"`")
	case t.kind == tokPunct && t.text == ":":
		l.colon = true
	case t.kind == tokPunct && (t.text == "(" || t.text == "[" || t.text == "{"):
		l.open = append(l.open, t)
	case t.kind == tokPunct && t.text == interpolationOpen:
		first, second := halves(t)
		l.open = append(l.open, first, second)
	case t.kind == tokPunct && closers[t.text] != "":
		l.close(t)
	case t.kind == tokPunct && t.text == "}}":
		first, second := halves(t)
		l.close(first)
		l.close(second)
	}
}

func halves(t token) (token, token) {
	c := t.text[:1]
	return token{kind: tokPunct, text: c, span: Span{Start: t.span.Start, End: t.span.Start + 1}}, token{kind: tokPunct, text: c, span: Span{Start: t.span.Start + 1, End: t.span.End}}
}

func (l *lexer) close(t token) {
	if len(l.open) == 0 {
		l.lexError(t.span, "unexpected closing delimiter `"+t.text+"`")
		return
	}
	opener := l.open[len(l.open)-1]
	l.open = l.open[:len(l.open)-1]
	if opener.text != closers[t.text] {
		l.lexError(t.span, fmt.Sprintf("mismatched closing delimiter `%s`; the `%s` on line %d is still open", t.text, opener.text, l.f.line(opener.span.Start)))
	}
}

func (l *lexer) body(end int) {
	r := l.r
	for r.pos < end {
		rest := r.text[r.pos:end]
		at := strings.Index(rest, interpolationOpen)
		if at < 0 {
			break
		}
		r.pos += at
		if strings.HasPrefix(r.text[r.pos:], escapedOpen) {
			r.pos += len(escapedOpen)
			continue
		}
		open := r.pos
		r.pos += len(interpolationOpen)
		if !l.interpolation(open) {
			return
		}
		end = r.lineEnd(r.pos)
	}
	r.pos = end + 1
}

func (l *lexer) interpolation(open int) bool {
	r := l.r
	r.depth = 1
	defer func() { r.depth, l.open = 0, nil }()
	for {
		t, ok := r.token()
		if !ok {
			return false
		}
		switch {
		case t.kind == tokEOF:
			l.lexError(Span{Start: open, End: open + len(interpolationOpen)}, "unterminated interpolation")
			return false
		case t.kind == tokPunct && t.text == "}}" && len(l.open) == 0:
			return true
		default:
			l.token(t)
			r.depth = len(l.open) + 1
		}
	}
}

func (r *reader) token() (t token, ok bool) {
	start := r.pos
	defer func() {
		if p := recover(); p != nil {
			if _, bailed := p.(bail); !bailed {
				panic(p)
			}
			r.depth, r.braces = 0, 0
			r.pos = max(r.lineEnd(start)+1, start+1)
			ok = false
		}
	}()
	return r.next(), true
}
