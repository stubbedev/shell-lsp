package justfile

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

type tokenKind uint8

const (
	tokEOF tokenKind = iota
	tokNewline
	tokName
	tokString
	tokBacktick
	tokPunct
	tokError
	tokFormat
)

type token struct {
	kind tokenKind
	span Span
	text string
	str  *StringLit
}

var punctuation = []string{"{{", "}}", ":=", "::", "==", "=~", "!=", "!~", "&&", "||", "++", ":", "=", "+", "/", "!", "(", ")", "[", "]", "{", "}", ",", "@", "$", "*", "?"}

type bail struct{}

type reader struct {
	text    string
	pos     int
	depth   int
	braces  int
	file    *File
	pending []Attribute
	header  *RecipeHeader
	defined *Name
	doc     string
	failure Span
	lexing  bool
}

func Parse(text string) *File {
	p := &reader{text: text, file: &File{Text: text}}
	if at, ok := invalidUTF8(text); ok {
		p.lexError(Span{Start: at, End: at + 1}, "just only reads UTF-8, and this byte is not")
	}
	p.parseFile()
	p.file.lexicalErrors()
	p.file.sortItems()
	return p.file
}

func invalidUTF8(text string) (int, bool) {
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			return i, true
		}
		i += size
	}
	return 0, false
}

func isNameStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isNameByte(c byte) bool {
	return isNameStart(c) || c == '-' || c >= '0' && c <= '9'
}

func (p *reader) fail(code lint.Code, span Span, format string, args ...any) {
	p.report(code, span, fmt.Sprintf(format, args...))
	p.failure = span
	panic(bail{})
}

func (p *reader) lexError(span Span, message string) {
	p.report(lint.CodeJustSyntax, span, message)
}

func (p *reader) known(kind model.Kind, name Name, code lint.Code) {
	if _, ok := Builtins.Lookup(kind, name.Text); !ok {
		p.report(code, name.Span, fmt.Sprintf("unknown %s `%s`", kind, name.Text))
	}
}

func (p *reader) report(code lint.Code, span Span, message string) {
	e := Error{Code: code, Span: span, Message: message}
	if !slices.Contains(p.file.Errors, e) {
		p.file.Errors = append(p.file.Errors, e)
	}
}

func (p *reader) lineText(start, end int) string {
	if end < len(p.text) {
		return strings.TrimSuffix(p.text[start:end], "\r")
	}
	return p.text[start:end]
}

func blank(line string) bool { return strings.TrimLeft(line, " \t") == "" }

func (p *reader) lineEnd(from int) int {
	if i := strings.IndexByte(p.text[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(p.text)
}

func (p *reader) skipSpace() {
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' && p.pos+1 < len(p.text) && p.text[p.pos+1] == '\n':
			p.pos++
		case c == '\\' && strings.HasPrefix(p.text[p.pos+1:], "\n"):
			p.pos += 2
		case c == '\\' && strings.HasPrefix(p.text[p.pos+1:], "\r\n"):
			p.pos += 3
		case c == '\n' && p.depth > 0:
			p.pos++
		default:
			return
		}
	}
}

func (p *reader) comment() {
	end := p.lineEnd(p.pos)
	text := p.lineText(p.pos, end)
	c := Comment{Span: Span{Start: p.pos, End: p.pos + len(text)}, Text: text}
	comments := p.file.Comments
	i, found := slices.BinarySearchFunc(comments, c.Span.Start, func(c Comment, start int) int { return c.Span.Start - start })
	overlaps := found || i > 0 && comments[i-1].Span.End > c.Span.Start || i < len(comments) && comments[i].Span.Start < c.Span.End
	if !overlaps {
		p.file.Comments = slices.Insert(comments, i, c)
	}
	p.pos = end
}

func (p *reader) peek() token {
	save := p.pos
	t := p.next()
	p.pos = save
	return t
}

func (p *reader) next() token {
	p.skipSpace()
	start := p.pos
	if start >= len(p.text) {
		return token{kind: tokEOF, span: Span{Start: start, End: start}}
	}
	c := p.text[start]
	switch {
	case c == '\n':
		p.pos++
		return token{kind: tokNewline, span: Span{Start: start, End: start + 1}}
	case c == '#' && p.depth > 0:
		end := p.lineEnd(start)
		p.pos = end
		return token{kind: tokError, span: Span{Start: start, End: end}, text: "#"}
	case c == '#':
		p.comment()
		return p.next()
	case isNameStart(c):
		end := start + 1
		for end < len(p.text) && isNameByte(p.text[end]) {
			end++
		}
		p.pos = end
		return token{kind: tokName, span: Span{Start: start, End: end}, text: p.text[start:end]}
	case c == '\'' || c == '"':
		return p.stringToken(start, p.formatAt(start))
	case c == '`':
		return p.backtickToken(start)
	case c == '}' && p.braces > 0:
		p.pos++
		return token{kind: tokPunct, span: Span{Start: start, End: start + 1}, text: "}"}
	}
	for _, punct := range punctuation {
		if strings.HasPrefix(p.text[start:], punct) {
			p.pos += len(punct)
			return token{kind: tokPunct, span: Span{Start: start, End: p.pos}, text: punct}
		}
	}
	_, size := utf8.DecodeRuneInString(p.text[start:])
	p.pos += size
	return token{kind: tokError, span: Span{Start: start, End: p.pos}, text: p.text[start:p.pos]}
}

func (p *reader) quoteAt(at int) string {
	for _, q := range []string{`"""`, "'''", "```", `"`, "'", "`"} {
		if strings.HasPrefix(p.text[at:], q) {
			return q
		}
	}
	return ""
}

type format uint8

const (
	unformatted format = iota
	parsedFormat
	skippedFormat
	splitFormat
)

func (p *reader) formatAt(quote int) format {
	switch {
	case quote == 0 || p.text[quote-1] != 'f' || quote > 1 && isNameByte(p.text[quote-2]):
		return unformatted
	case p.lexing:
		return splitFormat
	}
	return skippedFormat
}

func (p *reader) stringToken(start int, f format) token {
	quote := p.quoteAt(p.pos)
	lit := &StringLit{Quote: quote}
	if f != unformatted {
		lit.Prefix = 'f'
	}
	return p.stringRest(start, p.pos, p.pos+len(quote), lit, f)
}

func (p *reader) formatContinuation(quote string) token {
	return p.stringRest(p.pos, p.pos, p.pos+len(interpolationClose), &StringLit{Prefix: 'f', Quote: quote}, splitFormat)
}

func (p *reader) stringRest(start, resumed, open int, lit *StringLit, f format) token {
	quote := lit.Quote
	cooked := quote[0] == '"'
	i := open
	segment := open
	quoted := resumed
	flush := func(at int) {
		if at > segment {
			lit.Segments = append(lit.Segments, Segment{Kind: FragmentText, Span: Span{Start: segment, End: at}})
		}
	}
	for {
		if i >= len(p.text) {
			flush(len(p.text))
			lit.Unclosed = true
			lit.Inner = Span{Start: open, End: len(p.text)}
			p.pos = len(p.text)
			p.lexError(Span{Start: resumed, End: len(p.text)}, "unterminated string")
			return token{kind: skippedKind(f, lit), span: Span{Start: start, End: p.pos}, str: lit}
		}
		switch {
		case strings.HasPrefix(p.text[i:], quote):
			flush(i)
			lit.Inner = Span{Start: open, End: i}
			p.pos = i + len(quote)
			value, invalid := cook(p.text[open:i], cooked, lit.Indented())
			lit.Value = value
			if invalid != "" {
				p.lexError(Span{Start: quoted, End: p.pos}, fmt.Sprintf("`%s` is not a valid escape sequence", invalid))
			}
			return token{kind: skippedKind(f, lit), span: Span{Start: start, End: p.pos}, str: lit}
		case cooked && p.text[i] == '\\' && i+1 < len(p.text):
			i += 2
		case f != unformatted && strings.HasPrefix(p.text[i:], escapedOpen):
			flush(i)
			lit.Segments = append(lit.Segments, Segment{Kind: FragmentEscape, Span: Span{Start: i, End: i + len(escapedOpen)}})
			i += len(escapedOpen)
			segment = i
		case f == splitFormat && strings.HasPrefix(p.text[i:], interpolationOpen):
			p.pos = i + len(interpolationOpen)
			return token{kind: tokFormat, span: Span{Start: start, End: p.pos}, text: interpolationOpen, str: lit}
		case f != unformatted && strings.HasPrefix(p.text[i:], interpolationOpen):
			flush(i)
			p.pos = i + len(interpolationOpen)
			var interp *Expr
			closed := true
			if f == parsedFormat {
				interp = p.interpolation()
			} else {
				closed = p.skipInterpolation()
			}
			lit.Segments = append(lit.Segments, Segment{Kind: FragmentInterpolation, Span: Span{Start: i, End: p.pos}, Interp: interp})
			if !closed {
				lit.Unclosed = true
				lit.Inner = Span{Start: open, End: p.pos}
				return token{kind: skippedKind(f, lit), span: Span{Start: start, End: p.pos}, str: lit}
			}
			i = p.pos
			segment = i
			resumed = i - len(interpolationClose)
		default:
			i++
		}
	}
}

func skippedKind(f format, lit *StringLit) tokenKind {
	if f != skippedFormat {
		return tokString
	}
	if lit.braced() {
		return tokFormat
	}
	lit.Prefix = 0
	return tokString
}

func (s *StringLit) braced() bool {
	return slices.ContainsFunc(s.Segments, func(seg Segment) bool { return seg.Kind != FragmentText })
}

func (p *reader) skipInterpolation() bool {
	depth, braces := p.depth, p.braces
	defer func() { p.depth, p.braces = depth, braces }()
	open := 0
	for {
		p.depth, p.braces = open+1, open
		t := p.next()
		switch {
		case t.kind == tokEOF:
			return false
		case t.kind == tokPunct && t.text == interpolationClose:
			return true
		case t.kind == tokPunct && t.text == interpolationOpen:
			open += len(interpolationOpen)
		case t.kind == tokPunct && closers[t.text] != "":
			open = max(open-1, 0)
		case t.kind == tokPunct && (t.text == "(" || t.text == "[" || t.text == "{"):
			open++
		}
	}
}

func (p *reader) backtickToken(start int) token {
	quote := p.quoteAt(start)
	open := start + len(quote)
	end := strings.Index(p.text[open:], quote)
	lit := &StringLit{Quote: quote}
	if end < 0 {
		lit.Unclosed = true
		lit.Inner = Span{Start: open, End: len(p.text)}
		p.pos = len(p.text)
		p.lexError(Span{Start: start, End: len(p.text)}, "unterminated backtick")
		return token{kind: tokBacktick, span: Span{Start: start, End: p.pos}, str: lit}
	}
	lit.Inner = Span{Start: open, End: open + end}
	lit.Value = p.text[open : open+end]
	p.pos = open + end + len(quote)
	return token{kind: tokBacktick, span: Span{Start: start, End: p.pos}, str: lit}
}

func cook(raw string, escapes, indented bool) (string, string) {
	if indented {
		raw = unindent(raw)
	}
	if !escapes || !strings.Contains(raw, `\`) {
		return raw, ""
	}
	var sb strings.Builder
	invalid := ""
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c != '\\' || i+1 >= len(raw) {
			sb.WriteByte(c)
			continue
		}
		i++
		switch raw[i] {
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case '"':
			sb.WriteByte('"')
		case '\\':
			sb.WriteByte('\\')
		case '\n':
		case 'u':
			end := strings.IndexByte(raw[i:], '}')
			if strings.HasPrefix(raw[i:], "u{") && end > 2 && end <= 8 {
				if n, err := strconv.ParseUint(raw[i+2:i+end], 16, 32); err == nil && n <= unicode.MaxRune && utf8.ValidRune(rune(n)) {
					sb.WriteRune(rune(n))
					i += end
					continue
				}
			}
			invalid = cmp.Or(invalid, `\u`)
		default:
			_, size := utf8.DecodeRuneInString(raw[i:])
			invalid = cmp.Or(invalid, `\`+raw[i:i+size])
			sb.WriteString(`\` + raw[i:i+size])
			i += size - 1
		}
	}
	return sb.String(), invalid
}

func unindent(s string) string {
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 && blankLine(lines[0]) {
		lines = lines[1:]
	}
	if len(lines) > 0 && blankLine(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	common := ""
	first := true
	for _, l := range lines {
		if blankLine(l) {
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if first {
			common, first = indent, false
			continue
		}
		for !strings.HasPrefix(indent, common) {
			common = common[:len(common)-1]
		}
	}
	var sb strings.Builder
	for _, l := range lines {
		if blankLine(l) {
			sb.WriteString(l[len(strings.TrimRight(l, "\r\n")):])
			continue
		}
		sb.WriteString(strings.TrimPrefix(l, common))
	}
	return sb.String()
}

func blankLine(l string) bool { return strings.Trim(l, " \t\r\n") == "" }

func (p *reader) expect(punct string) token {
	t := p.next()
	if t.kind != tokPunct || t.text != punct {
		p.unexpected(t, "`"+punct+"`")
	}
	return t
}

func (p *reader) accept(punct string) (token, bool) {
	t := p.peek()
	if t.kind == tokPunct && t.text == punct {
		return p.next(), true
	}
	return t, false
}

func describe(t token) string {
	switch t.kind {
	case tokEOF:
		return "end of file"
	case tokNewline:
		return "end of line"
	case tokName:
		return "name `" + t.text + "`"
	case tokString:
		if len(t.str.Interps()) > 0 {
			return "format string"
		}
		return "string"
	case tokFormat:
		return "format string"
	case tokBacktick:
		return "backtick"
	case tokError:
		if t.text == "#" {
			return "a comment, which cannot appear inside brackets"
		}
	case tokPunct:
	}
	return "`" + t.text + "`"
}

func (p *reader) unexpected(t token, wanted string) {
	p.mismatch(lint.CodeJustSyntax, t, "expected "+wanted)
}

func (p *reader) mismatch(code lint.Code, t token, message string) {
	span := t.span
	switch {
	case t.kind == tokEOF && t.span.Start > 0 && p.text[t.span.Start-1] == '\n':
		span = Span{Start: t.span.Start - 1, End: t.span.Start - 1}
	case t.kind == tokNewline || t.kind == tokEOF:
		span = Span{Start: t.span.Start, End: t.span.Start}
	}
	p.fail(code, span, "%s, found %s", message, describe(t))
}

func (p *reader) name() Name {
	t := p.next()
	if t.kind != tokName {
		p.unexpected(t, "a name")
	}
	return Name{Text: t.text, Span: t.span}
}

func (p *reader) endOfLine() {
	t := p.next()
	if t.kind != tokNewline && t.kind != tokEOF {
		p.unexpected(t, "end of line")
	}
}

func (p *reader) atLineStart() bool {
	return p.pos == 0 || p.text[p.pos-1] == '\n'
}

func (p *reader) parseFile() {
	if strings.HasPrefix(p.text, "#!") {
		p.comment()
	}
	joined := false
	for p.pos < len(p.text) {
		if !p.atLineStart() && !joined {
			p.pos = p.lineEnd(p.pos) + 1
			continue
		}
		end := p.lineEnd(p.pos)
		line := p.lineText(p.pos, end)
		trimmed := strings.TrimLeft(line, " \t")
		if joined {
			p.pos += len(line) - len(trimmed)
			line, joined = trimmed, false
		}
		switch {
		case line == `\` && end < len(p.text):
			p.pos, joined = end+1, true
		case trimmed == "" && end == len(p.text):
			p.pos = end
		case trimmed == "":
			p.extraneous()
			p.doc = ""
			p.pos = end + 1
		case strings.HasPrefix(trimmed, "#") && len(trimmed) == len(line):
			p.extraneous()
			start := p.pos
			p.comment()
			text := strings.TrimSpace(strings.TrimPrefix(p.text[start:p.pos], "#"))
			p.doc = text
			p.pos = end + 1
		case len(trimmed) != len(line):
			p.lexError(Span{Start: p.pos, End: p.pos + len(line)}, "unexpected indentation")
			p.skipIndented(p.pos)
			p.stray()
			p.doc = ""
		default:
			p.item()
		}
	}
	if len(p.pending) > 0 {
		end := len(p.text)
		if strings.HasSuffix(p.text, "\n") {
			end--
		}
		p.lexError(Span{Start: end, End: end}, "expected an item after the attributes, found the end of the file")
		p.stray()
	}
}

func (p *reader) extraneous() {
	if len(p.pending) == 0 {
		return
	}
	p.lexError(p.pending[0].Span, "extraneous attribute: nothing follows it to apply to")
	p.stray()
}

func (p *reader) skipIndented(from int) {
	p.pos = from
	for p.pos < len(p.text) {
		end := p.lineEnd(p.pos)
		line := p.lineText(p.pos, end)
		if !blank(line) && line[0] != ' ' && line[0] != '\t' {
			return
		}
		p.pos = end + 1
	}
}

func (p *reader) recover(start int, header bool) {
	r := recover()
	if r == nil {
		return
	}
	if _, ok := r.(bail); !ok {
		panic(r)
	}
	p.depth, p.braces = 0, 0
	end := p.lineEnd(max(p.failure.End, start))
	if header {
		p.skipIndented(end + 1)
	} else {
		p.pos = end + 1
	}
	p.stray()
	p.doc = ""
}

func (p *reader) stray() {
	if len(p.pending) > 0 {
		p.file.Stray = append(p.file.Stray, Stray{Attributes: p.pending, Recipe: p.header, Function: p.defined})
	}
	p.pending, p.header, p.defined = nil, nil, nil
}

func (p *reader) item() {
	start := p.pos
	defer p.recover(start, true)
	first := p.peek()
	if first.kind == tokPunct && first.text == "[" {
		p.attributes()
		return
	}
	attrs, doc := p.pending, p.doc
	p.doc = ""
	p.dispatch(start, first, attrs, doc)
	p.pending = nil
}

func (p *reader) dispatch(start int, first token, attrs []Attribute, doc string) {
	if first.kind == tokPunct && first.text == "@" {
		p.recipe(start, attrs, doc)
		return
	}
	if first.kind != tokName {
		p.unexpected(p.next(), "a recipe, assignment, alias, setting, import or module")
	}
	save := p.pos
	p.next()
	pathFollows := p.atPath()
	second := p.next()
	pathFollowsSecond := p.atPath()
	third := p.peek()
	p.pos = save
	isName := second.kind == tokName
	defines := third.kind == tokPunct && third.text == ":="
	ends := third.kind == tokNewline || third.kind == tokEOF
	optional := second.kind == tokPunct && second.text == "?"
	switch first.text {
	case "alias":
		if isName && defines {
			p.alias(start, attrs)
			return
		}
	case "export", "eager":
		if isName && defines {
			p.assignment(start, attrs, doc)
			return
		}
	case "unexport":
		if isName && ends {
			p.unexport(start, attrs)
			return
		}
	case "set":
		if isName && (defines || ends) {
			p.setting(start, attrs)
			return
		}
	case "import":
		if optional || pathFollows {
			p.importItem(start, attrs)
			return
		}
	case "mod":
		if optional || isName && (ends || pathFollowsSecond) {
			p.module(start, attrs, doc)
			return
		}
	}
	switch {
	case second.kind == tokPunct && second.text == ":=":
		p.assignment(start, attrs, doc)
	case second.kind == tokPunct && second.text == "(":
		p.function(start, attrs)
	default:
		p.recipe(start, attrs, doc)
	}
}

func (p *reader) attributes() {
	bracket := p.expect("[").span.Start
	p.depth++
	for {
		p.attribute(bracket)
		if _, ok := p.accept(","); ok {
			continue
		}
		break
	}
	p.expect("]")
	p.depth--
	p.endOfLine()
}

func (p *reader) attribute(bracket int) {
	name := p.name()
	p.known(model.KindAttribute, name, lint.CodeJustUnknownAttribute)
	p.pending = append(p.pending, Attribute{Name: name, Span: name.Span, Bracket: bracket, Partial: true})
	attr := &p.pending[len(p.pending)-1]
	switch t := p.peek(); {
	case t.kind == tokPunct && t.text == ":":
		p.next()
		value := p.value()
		attr.Args = []AttributeArg{{Value: value, Span: value.Span}}
		attr.Span.End = value.Span.End
	case t.kind == tokPunct && t.text == "(":
		p.next()
		p.depth++
		for {
			if _, ok := p.accept(")"); ok {
				break
			}
			arg := p.attributeArg(attributeKeys[name.Text] != nil)
			switch {
			case arg.Key.Text == "" && slices.ContainsFunc(attr.Args, func(prior AttributeArg) bool { return prior.Key.Text != "" }):
				p.report(lint.CodeJustAttributeArguments, arg.Span, "positional attribute arguments cannot follow keyword attribute arguments")
			case arg.Key.Text != "" && slices.ContainsFunc(attr.Args, func(prior AttributeArg) bool { return prior.Key.Text == arg.Key.Text }):
				p.report(lint.CodeJustAttributeArguments, arg.Key.Span, fmt.Sprintf("duplicate key `%s` for `%s` attribute", arg.Key.Text, name.Text))
			}
			attr.Args = append(attr.Args, arg)
			if _, ok := p.accept(","); ok {
				continue
			}
			p.expect(")")
			break
		}
		p.depth--
		attr.Span.End = p.pos
	}
	p.literalArguments(*attr)
	attr.Partial = false
}

func (p *reader) literalArguments(a Attribute) {
	if arg, ok := nonLiteralArgument(a); ok {
		p.report(lint.CodeJustAttributeArguments, arg.Span, fmt.Sprintf("the arguments of attribute `%s` must be string literals", a.Name.Text))
	}
}

func (p *reader) attributeArg(keyed bool) AttributeArg {
	if t := p.peek(); keyed && t.kind == tokName && !p.prefixesString(t, "x") {
		p.next()
		key := Name{Text: t.text, Span: t.span}
		if _, ok := p.accept("="); ok {
			value := p.expression()
			return AttributeArg{Key: key, Value: value, Span: Span{Start: t.span.Start, End: value.Span.End}}
		}
		return AttributeArg{Key: key, Span: t.span}
	}
	value := p.expression()
	return AttributeArg{Value: value, Span: value.Span}
}

func (p *reader) alias(start int, attrs []Attribute) {
	p.next()
	name := p.name()
	p.expect(":=")
	target := p.path()
	end := p.pos
	p.endOfLine()
	p.file.Items = append(p.file.Items, Item{Kind: ItemAlias, Alias: &Alias{Attributes: attrs, Name: name, Target: target, Span: Span{Start: start, End: end}}})
}

func (p *reader) path() Path {
	out := Path{p.name()}
	for {
		if _, ok := p.accept("::"); !ok {
			return out
		}
		out = append(out, p.name())
	}
}

func (p *reader) assignment(start int, attrs []Attribute, doc string) {
	a := &Assignment{Attributes: attrs, Doc: doc}
	first := p.name()
	switch first.Text {
	case "export":
		if t := p.peek(); t.kind == tokName {
			a.Export, a.Keyword = true, first.Span
			first = p.name()
		}
	case "eager":
		if t := p.peek(); t.kind == tokName {
			a.Eager, a.Keyword = true, first.Span
			first = p.name()
		}
	}
	a.Name = first
	p.expect(":=")
	a.Value = p.expression()
	a.Span = Span{Start: start, End: a.Value.Span.End}
	p.endOfLine()
	p.file.Items = append(p.file.Items, Item{Kind: ItemAssignment, Assignment: a})
}

func (p *reader) unexport(start int, attrs []Attribute) {
	p.next()
	name := p.name()
	p.endOfLine()
	p.file.Items = append(p.file.Items, Item{Kind: ItemUnexport, Unexport: &Unexport{Attributes: attrs, Name: name, Span: Span{Start: start, End: name.Span.End}}})
}

func (p *reader) setting(start int, attrs []Attribute) {
	p.next()
	s := &Setting{Attributes: attrs, Name: p.name()}
	s.Span = Span{Start: start, End: s.Name.Span.End}
	if spec, ok := settingsByName[s.Name.Text]; ok {
		s.Value = p.settingValue(s.Name, spec)
	} else {
		p.known(model.KindSetting, s.Name, lint.CodeJustUnknownSetting)
		if _, ok := p.accept(":="); ok {
			s.Value = p.expression()
		}
	}
	if s.Value != nil {
		s.Span.End = s.Value.Span.End
	}
	p.endOfLine()
	p.file.Items = append(p.file.Items, Item{Kind: ItemSetting, Setting: s})
}

func (p *reader) settingValue(name Name, spec settingSpec) *Expr {
	if spec.kind == settingBool {
		if _, ok := p.accept(":="); !ok {
			return nil
		}
		t := p.next()
		if t.kind != tokName || t.text != "true" && t.text != "false" {
			p.mismatch(lint.CodeJustSettingValue, t, fmt.Sprintf("setting `%s` takes `true` or `false`", name.Text))
		}
		return &Expr{Kind: ExprVariable, Span: t.span, Name: Name{Text: t.text, Span: t.span}}
	}
	if t := p.next(); t.kind != tokPunct || t.text != ":=" {
		p.mismatch(lint.CodeJustSettingValue, t, fmt.Sprintf("setting `%s` needs a value", name.Text))
	}
	switch {
	case spec.kind == settingList:
		return p.interpreter(name)
	case spec.literal != nil:
		return p.literalSetting(name, spec.literal)
	}
	return p.expression()
}

func (p *reader) interpreter(name Name) *Expr {
	open := p.next()
	if open.kind != tokPunct || open.text != "[" {
		p.mismatch(lint.CodeJustSettingValue, open, fmt.Sprintf("setting `%s` takes a list such as `['bash', '-c']`", name.Text))
	}
	p.depth++
	if t := p.peek(); t.kind == tokPunct && t.text == "]" {
		p.mismatch(lint.CodeJustSettingValue, t, fmt.Sprintf("setting `%s` needs at least the command", name.Text))
	}
	p.depth--
	return p.list(open)
}

func (p *reader) literalSetting(name Name, valid func(string) error) *Expr {
	v := p.expression()
	switch {
	case v.Kind != ExprString || v.Str.Prefix != 0 || v.Str.Indented():
		p.report(lint.CodeJustSettingValue, name.Span, fmt.Sprintf("`%s` setting must be a plain string literal", name.Text))
	default:
		if err := valid(v.Str.Value); err != nil {
			p.report(lint.CodeJustSettingValue, v.Span, err.Error())
		}
	}
	return v
}

func (p *reader) importItem(start int, attrs []Attribute) {
	p.next()
	imp := &Import{Attributes: attrs}
	if _, ok := p.accept("?"); ok {
		imp.Optional = true
	}
	imp.Path = p.stringValue()
	imp.Span = Span{Start: start, End: imp.Path.Span.End}
	p.endOfLine()
	p.file.Items = append(p.file.Items, Item{Kind: ItemImport, Import: imp})
}

func (p *reader) atPath() bool {
	save := p.pos
	defer func() { p.pos = save }()
	t := p.next()
	if t.kind == tokName {
		t = p.next()
	}
	return t.kind == tokString
}

func (p *reader) stringValue() *Expr {
	t := p.next()
	start := t.span.Start
	expanded := t.kind == tokName
	if expanded {
		if t.text != "x" {
			p.unexpected(t, "a string")
		}
		t = p.next()
	}
	if t.kind != tokString {
		p.unexpected(t, "a string")
	}
	if expanded {
		t.str.Prefix = 'x'
	}
	return &Expr{Kind: ExprString, Span: Span{Start: start, End: t.span.End}, Str: t.str}
}

func (p *reader) module(start int, attrs []Attribute, doc string) {
	p.next()
	m := &Module{Attributes: attrs, Doc: doc}
	if _, ok := p.accept("?"); ok {
		m.Optional = true
	}
	m.Name = p.name()
	m.Span = Span{Start: start, End: m.Name.Span.End}
	if p.atPath() {
		m.Path = p.stringValue()
		m.Span.End = m.Path.Span.End
	}
	p.endOfLine()
	p.file.Items = append(p.file.Items, Item{Kind: ItemModule, Module: m})
}

func (p *reader) function(start int, attrs []Attribute) {
	f := &Function{Attributes: attrs, Name: p.name()}
	p.defined = &f.Name
	p.expect("(")
	p.depth++
	for {
		if _, ok := p.accept(")"); ok {
			break
		}
		f.Params = append(f.Params, p.name())
		if _, ok := p.accept(","); ok {
			continue
		}
		p.expect(")")
		break
	}
	p.depth--
	p.expect(":=")
	f.Body = p.expression()
	f.Span = Span{Start: start, End: f.Body.Span.End}
	p.endOfLine()
	p.defined = nil
	p.file.Items = append(p.file.Items, Item{Kind: ItemFunction, Function: f})
}

func (p *reader) recipe(start int, attrs []Attribute, doc string) {
	r := &Recipe{Attributes: attrs, Doc: doc}
	if _, ok := p.accept("@"); ok {
		r.Quiet = true
	}
	r.Name = p.name()
	p.header = &RecipeHeader{Name: r.Name}
	for {
		t := p.peek()
		if t.kind == tokPunct && t.text == ":" {
			p.next()
			p.header.Colon = true
			break
		}
		if t.kind != tokName && (t.kind != tokPunct || !slices.Contains([]string{"$", "+", "*"}, t.text)) {
			p.unexpected(p.next(), "a parameter or `:`")
		}
		r.Params = append(r.Params, p.param())
		p.header.Params = r.Params
		if r.Params[len(r.Params)-1].Variadic == Singular {
			continue
		}
		switch t := p.peek(); {
		case t.kind == tokName:
			p.fail(lint.CodeJustParameterOrder, t.span, "parameter `%s` follows variadic parameter", t.text)
		case t.kind != tokPunct || t.text != ":":
			p.unexpected(p.next(), "`:`")
		}
	}
	r.Deps = p.dependencies()
	if _, ok := p.accept("&&"); ok {
		r.Subsequent = p.dependencies()
		if len(r.Subsequent) == 0 {
			p.unexpected(p.next(), "a dependency after `&&`")
		}
	}
	r.Header = Span{Start: start, End: p.pos}
	p.endOfLine()
	r.Span = r.Header
	p.body(r)
	p.header = nil
	p.file.Items = append(p.file.Items, Item{Kind: ItemRecipe, Recipe: r})
}

func (p *reader) param() Param {
	start := p.peek().span.Start
	param := Param{}
	if t, ok := p.accept("+"); ok {
		param.Variadic = Plus
		start = t.span.Start
	} else if t, ok := p.accept("*"); ok {
		param.Variadic = Star
		start = t.span.Start
	}
	if _, ok := p.accept("$"); ok {
		param.Export = true
	}
	param.Name = p.name()
	param.Span = Span{Start: start, End: param.Name.Span.End}
	if _, ok := p.accept("="); ok {
		param.Default = p.value()
		param.Span.End = param.Default.Span.End
	}
	return param
}

func (p *reader) dependencies() []Dependency {
	var out []Dependency
	for {
		t := p.peek()
		switch {
		case t.kind == tokName:
			target := p.path()
			out = append(out, Dependency{Target: target, Span: target.Span()})
		case t.kind == tokPunct && (t.text == "(" || t.text == "*"):
			out = append(out, p.parenDependency())
		default:
			return out
		}
	}
}

func (p *reader) parenDependency() Dependency {
	start := p.peek().span.Start
	dep := Dependency{Paren: true}
	if _, ok := p.accept("*"); ok {
		dep.Star = true
	}
	p.expect("(")
	p.depth++
	dep.Target = p.path()
	var starred []token
	for {
		if _, ok := p.accept(")"); ok {
			break
		}
		if t, ok := p.accept("*"); ok {
			if len(starred) == 1 {
				p.report(lint.CodeJustDependencyArguments, t.span, "a mapped dependency takes only one starred argument")
			}
			starred = append(starred, t)
			arg := p.value()
			arg.Starred = true
			dep.Args = append(dep.Args, arg)
			continue
		}
		dep.Args = append(dep.Args, p.expression())
	}
	p.depth--
	dep.Span = Span{Start: start, End: p.pos}
	switch {
	case len(starred) > 1:
	case dep.Star && len(starred) == 0:
		p.report(lint.CodeJustDependencyArguments, dep.Span, "a mapped dependency needs a starred argument such as `*items`")
	case !dep.Star && len(starred) > 0:
		p.report(lint.CodeJustDependencyArguments, starred[0].span, "a starred argument needs a mapped dependency such as `*(recipe *items)`")
	}
	return dep
}

func (p *reader) body(r *Recipe) {
	pos := p.pos
	var blanks []Line
	for pos < len(p.text) {
		end := p.lineEnd(pos)
		line := p.lineText(pos, end)
		span := Span{Start: pos, End: pos + len(line)}
		switch {
		case blank(line) && len(r.Body) == 0:
		case blank(line):
			blanks = append(blanks, Line{Span: span, Indent: span})
			pos = end + 1
			continue
		case line[0] == ' ' || line[0] == '\t':
			if len(r.Body) == 0 {
				content := strings.TrimLeft(line, " \t")
				r.Indent = line[:len(line)-len(content)]
				r.Shebang = strings.HasPrefix(content, "#!")
			}
			r.Body = append(r.Body, blanks...)
			blanks = nil
			l := p.bodyLine(span)
			r.Body = append(r.Body, l)
			pos = p.lineEnd(l.Span.End) + 1
			continue
		}
		break
	}
	if len(r.Body) == 0 {
		return
	}
	r.Span.End = r.Body[len(r.Body)-1].Span.End
	p.pos = p.lineEnd(r.Span.End) + 1
}

func (p *reader) bodyLine(l Span) Line {
	content := p.text[l.Start:l.End]
	line := Line{Span: l}
	indent := len(content) - len(strings.TrimLeft(content, " \t"))
	line.Indent = Span{Start: l.Start, End: l.Start + indent}
	at := l.Start + indent
	sigils := at
	for sigils < l.End && strings.IndexByte("@-?", p.text[sigils]) >= 0 {
		sigils++
	}
	line.Sigils = Span{Start: at, End: sigils}
	end := l.End
	pos := at
	textStart := at
	text := func(upTo int) {
		if upTo > textStart {
			line.Fragments = append(line.Fragments, Fragment{Kind: FragmentText, Span: Span{Start: textStart, End: upTo}})
		}
	}
	for pos < end {
		switch {
		case strings.HasPrefix(p.text[pos:], escapedOpen):
			text(pos)
			line.Fragments = append(line.Fragments, Fragment{Kind: FragmentEscape, Span: Span{Start: pos, End: pos + len(escapedOpen)}})
			pos += len(escapedOpen)
			textStart = pos
		case strings.HasPrefix(p.text[pos:], interpolationOpen):
			text(pos)
			interp := p.bodyInterpolation(pos)
			line.Fragments = append(line.Fragments, Fragment{Kind: FragmentInterpolation, Span: interp.span, Interp: interp.expr, Closed: interp.closed})
			pos = interp.span.End
			textStart = pos
			if pos > end {
				lineEnd := p.lineEnd(pos)
				end = pos + len(p.lineText(pos, lineEnd))
			}
		default:
			pos++
		}
	}
	text(end)
	line.Span.End = end
	line.Continued = strings.HasSuffix(p.text[line.Span.Start:line.Span.End], `\`)
	return line
}

type interpolated struct {
	span   Span
	expr   *Expr
	closed bool
}

func (p *reader) bodyInterpolation(at int) (out interpolated) {
	save := p.pos
	p.pos = at + 2
	defer func() {
		r := recover()
		if r != nil {
			if _, ok := r.(bail); !ok {
				panic(r)
			}
			end := strings.Index(p.text[at:], interpolationClose)
			lineEnd := p.lineEnd(at)
			if end < 0 || at+end > lineEnd {
				end = lineEnd - at
			} else {
				end += 2
			}
			out = interpolated{span: Span{Start: at, End: at + end}, expr: &Expr{Kind: ExprBad, Span: Span{Start: at + 2, End: at + end}}}
		}
		p.pos = save
		p.depth, p.braces = 0, 0
	}()
	expr := p.interpolation()
	return interpolated{span: Span{Start: at, End: p.pos}, expr: expr, closed: true}
}

func (p *reader) interpolation() *Expr {
	depth, braces := p.depth, p.braces
	p.depth, p.braces = 1, 0
	if t := p.peek(); t.kind == tokPunct && t.text == interpolationClose {
		p.fail(lint.CodeJustSyntax, t.span, "an interpolation needs an expression")
	}
	expr := p.expression()
	p.expect(interpolationClose)
	p.depth, p.braces = depth, braces
	return expr
}

const comparison = 3

var operators = map[string]int{"||": 1, "&&": 2, "==": comparison, "!=": comparison, "=~": comparison, "!~": comparison}

func (p *reader) expression() *Expr {
	return p.binary(1)
}

func (p *reader) binary(level int) *Expr {
	if level > comparison {
		return p.conjunct()
	}
	left := p.binary(level + 1)
	t := p.peek()
	if t.kind != tokPunct || operators[t.text] != level {
		return left
	}
	p.next()
	var right *Expr
	if level == comparison {
		right = p.binary(level + 1)
	} else {
		right = p.binary(level)
	}
	return &Expr{Kind: ExprBinary, Op: t.text, OpSpan: t.span, Span: Span{Start: left.Span.Start, End: right.Span.End}, Args: []*Expr{left, right}}
}

func (p *reader) conjunct() *Expr {
	t := p.peek()
	switch {
	case t.kind == tokName && t.text == "if":
		return p.conditional()
	case t.kind == tokPunct && t.text == "/":
		p.next()
		x := p.conjunct()
		return &Expr{Kind: ExprRoot, Span: Span{Start: t.span.Start, End: x.Span.End}, Args: []*Expr{x}}
	}
	left := p.value()
	op := p.peek()
	if op.kind == tokPunct && (op.text == "+" || op.text == "++" || op.text == "/") {
		p.next()
		right := p.conjunct()
		return &Expr{Kind: ExprBinary, Op: op.text, OpSpan: op.span, Span: Span{Start: left.Span.Start, End: right.Span.End}, Args: []*Expr{left, right}}
	}
	return left
}

func (p *reader) conditional() *Expr {
	start := p.next().span.Start
	cond := p.expression()
	then := p.block()
	e := &Expr{Kind: ExprIf, Args: []*Expr{cond, then}, Span: Span{Start: start, End: then.Span.End}}
	if t := p.peek(); t.kind == tokName && t.text == "else" {
		e.Else = p.next().span
		var alt *Expr
		if t := p.peek(); t.kind == tokName && t.text == "if" {
			alt = p.conditional()
		} else {
			alt = p.block()
		}
		e.Args = append(e.Args, alt)
		e.Span.End = alt.Span.End
	}
	return e
}

func (p *reader) block() *Expr {
	open := p.expect("{")
	p.braces++
	p.depth++
	x := p.expression()
	closing := p.expect("}")
	p.braces--
	p.depth--
	return &Expr{Kind: ExprGroup, Span: Span{Start: open.span.Start, End: closing.span.End}, Args: []*Expr{x}}
}

func (p *reader) value() *Expr {
	t := p.next()
	switch {
	case t.kind == tokPunct && t.text == "!":
		x := p.value()
		return &Expr{Kind: ExprNot, Span: Span{Start: t.span.Start, End: x.Span.End}, Args: []*Expr{x}}
	case t.kind == tokString:
		return &Expr{Kind: ExprString, Span: t.span, Str: t.str}
	case t.kind == tokBacktick:
		return &Expr{Kind: ExprBacktick, Span: t.span, Str: t.str}
	case t.kind == tokName:
		if s, ok := p.prefixedString(t); ok {
			return &Expr{Kind: ExprString, Span: s.span, Str: s.str}
		}
		name := Name{Text: t.text, Span: t.span}
		if open := p.peek(); name.Text == "assert" || open.kind == tokPunct && open.text == "(" {
			return p.call(name)
		}
		return &Expr{Kind: ExprVariable, Span: t.span, Name: name}
	case t.kind == tokPunct && t.text == "(":
		p.depth++
		x := p.expression()
		closing := p.expect(")")
		p.depth--
		return &Expr{Kind: ExprGroup, Span: Span{Start: t.span.Start, End: closing.span.End}, Args: []*Expr{x}}
	case t.kind == tokPunct && t.text == "[":
		return p.list(t)
	}
	p.unexpected(t, "an expression")
	return nil
}

func (p *reader) prefixesString(t token, prefix string) bool {
	if t.kind != tokName || t.text != prefix {
		return false
	}
	q := p.quoteAt(t.span.End)
	return q != "" && q[0] != '`'
}

func (p *reader) prefixedString(prefix token) (token, bool) {
	if p.pos != prefix.span.End || !p.prefixesString(prefix, "f") && !p.prefixesString(prefix, "x") {
		return token{}, false
	}
	if prefix.text == "f" {
		return p.stringToken(prefix.span.Start, parsedFormat), true
	}
	s := p.stringToken(prefix.span.Start, unformatted)
	s.str.Prefix = 'x'
	return s, true
}

func (p *reader) arguments(e *Expr) {
	for {
		if _, ok := p.accept(")"); ok {
			return
		}
		e.Args = append(e.Args, p.expression())
		if _, ok := p.accept(","); !ok {
			p.expect(")")
			return
		}
	}
}

func (p *reader) assertArguments(e *Expr) {
	e.Args = append(e.Args, p.expression())
	if _, ok := p.accept(","); ok {
		e.Args = append(e.Args, p.expression())
	}
	p.expect(")")
}

func (p *reader) call(name Name) *Expr {
	p.expect("(")
	p.depth++
	e := &Expr{Kind: ExprCall, Name: name}
	if name.Text == "assert" {
		p.assertArguments(e)
	} else {
		p.arguments(e)
	}
	p.depth--
	e.Span = Span{Start: name.Span.Start, End: p.pos}
	return e
}

func (p *reader) list(open token) *Expr {
	p.depth++
	e := &Expr{Kind: ExprList}
	for {
		if _, ok := p.accept("]"); ok {
			break
		}
		e.Args = append(e.Args, p.expression())
		if _, ok := p.accept(","); ok {
			continue
		}
		p.expect("]")
		break
	}
	p.depth--
	e.Span = Span{Start: open.span.Start, End: p.pos}
	return e
}
