package justfile

import (
	"fmt"
	"strings"
)

type lexer struct {
	f      *File
	r      *reader
	erred  map[int]bool
	colon  bool
	inBody bool
	open   []token
}

var closers = map[string]string{")": "(", "]": "[", "}": "{"}

func (f *File) lexicalErrors() {
	l := &lexer{f: f, r: &reader{text: f.Text, file: &File{Text: f.Text}, lexing: true}, erred: map[int]bool{}}
	for _, e := range f.Errors {
		l.erred[f.line(e.Span.Start)] = true
	}
	for l.r.pos < len(l.r.text) {
		start := l.r.pos
		end := l.r.lineEnd(start)
		line := l.r.lineText(start, end)
		switch {
		case blank(line):
			l.colon = false
			l.r.pos = end + 1
		case line[0] == ' ' || line[0] == '\t':
			if l.inBody || l.colon {
				l.inBody, l.colon = true, false
				l.body(end)
				continue
			}
			l.tokens()
		default:
			l.inBody, l.colon = false, false
			l.tokens()
		}
	}
	for _, e := range l.r.file.Errors {
		l.report(e)
	}
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
		switch {
		case t.kind == tokEOF:
			return
		case t.kind == tokNewline:
			return
		case t.kind == tokError && t.text != "#":
			l.report(Error{Span: t.span, Message: "unknown start of token `" + t.text + "`"})
		case t.kind == tokPunct && t.text == ":":
			l.colon = true
		case t.kind == tokPunct && (t.text == "(" || t.text == "[" || t.text == "{"):
			l.open = append(l.open, t)
			l.r.depth++
		case t.kind == tokPunct && t.text == interpolationOpen:
			first, second := halves(t)
			l.open = append(l.open, first, second)
			l.r.depth += 2
		case t.kind == tokPunct && closers[t.text] != "":
			l.close(t)
		case t.kind == tokPunct && t.text == "}}":
			first, second := halves(t)
			l.close(first)
			l.close(second)
		}
	}
}

func halves(t token) (token, token) {
	c := t.text[:1]
	return token{kind: tokPunct, text: c, span: Span{Start: t.span.Start, End: t.span.Start + 1}}, token{kind: tokPunct, text: c, span: Span{Start: t.span.Start + 1, End: t.span.End}}
}

func (l *lexer) close(t token) {
	if len(l.open) == 0 {
		l.report(Error{Span: t.span, Message: "unexpected closing delimiter `" + t.text + "`"})
		return
	}
	opener := l.open[len(l.open)-1]
	l.open = l.open[:len(l.open)-1]
	l.r.depth = len(l.open)
	if opener.text != closers[t.text] {
		l.report(Error{Span: t.span, Message: fmt.Sprintf("mismatched closing delimiter `%s`; the `%s` on line %d is still open", t.text, opener.text, l.f.line(opener.span.Start))})
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
	defer func() { r.depth = 0 }()
	for {
		t, ok := r.token()
		if !ok {
			return false
		}
		switch {
		case t.kind == tokEOF:
			l.report(Error{Span: Span{Start: open, End: open + len(interpolationOpen)}, Message: "unterminated interpolation"})
			return false
		case t.kind == tokPunct && t.text == "}}":
			return true
		case t.kind == tokError && t.text != "#":
			l.report(Error{Span: t.span, Message: "unknown start of token `" + t.text + "`"})
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
