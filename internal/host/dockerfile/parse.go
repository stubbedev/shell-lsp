package dockerfile

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

const defaultEscape = '\\'

var directiveNames = []string{"syntax", "escape", "check"}

type line struct {
	start, end int
}

type logical struct {
	text string
	at   []int
}

func (l logical) span(start, end int) Span {
	if start >= end {
		return Span{Start: l.at[start], End: l.at[start]}
	}
	return Span{Start: l.at[start], End: l.at[end-1] + 1}
}

type reader struct {
	text  string
	lines []line
	file  *File
}

func Parse(text string) *File {
	r := &reader{text: text, file: &File{Text: text, Escape: defaultEscape}}
	scan.Forward(0, len(text), func(start int) int {
		end := textline.End(text, start)
		r.lines = append(r.lines, line{start: start, end: end})
		return end + 1
	})
	r.parse()
	return r.file
}

func (r *reader) content(l line) string {
	return strings.TrimSuffix(r.text[l.start:l.end], "\r")
}

func (r *reader) fail(kind ErrorKind, span Span, format string, args ...any) {
	r.file.Errors = append(r.file.Errors, Error{Span: span, Message: fmt.Sprintf(format, args...), Kind: kind})
}

func (r *reader) parse() {
	i := r.directives()
	for i < len(r.lines) {
		l := r.lines[i]
		content := r.content(l)
		trimmed := strings.TrimLeft(content, " \t")
		switch {
		case trimmed == "":
			i++
		case trimmed[0] == '#':
			r.file.Comments = append(r.file.Comments, Span{Start: l.start + len(content) - len(trimmed), End: l.start + len(content)})
			i++
		default:
			i = r.instruction(i, l.start+len(content)-len(trimmed))
		}
	}
}

func (r *reader) directives() int {
	seen := map[string]bool{}
	for i, l := range r.lines {
		content := r.content(l)
		d, ok := r.directive(l.start, content)
		if !ok {
			return i
		}
		name := strings.ToLower(d.Name.Text)
		if !slices.Contains(directiveNames, name) {
			r.file.Comments = append(r.file.Comments, Span{Start: l.start, End: l.start + len(content)})
			return i + 1
		}
		if seen[name] {
			r.fail(ErrDirective, d.Span, "only one %s parser directive can be used", name)
		}
		seen[name] = true
		if name == "escape" {
			switch d.Value.Text {
			case "\\", "`":
				r.file.Escape = d.Value.Text[0]
			default:
				r.fail(ErrDirective, d.Value.Span, "invalid escape token '%s' does not match ` or \\", d.Value.Text)
			}
		}
		r.file.Directives = append(r.file.Directives, d)
	}
	return len(r.lines)
}

func (r *reader) directive(at int, content string) (Directive, bool) {
	if !strings.HasPrefix(content, "#") {
		return Directive{}, false
	}
	rest := strings.TrimLeft(content[1:], " \t")
	nameAt := at + len(content) - len(rest)
	name := rest[:len(rest)-len(strings.TrimLeftFunc(rest, func(c rune) bool { return c < unicode.MaxASCII && (unicode.IsLetter(c) || unicode.IsDigit(c)) }))]
	if name == "" || !unicode.IsLetter(rune(name[0])) {
		return Directive{}, false
	}
	after := strings.TrimLeft(rest[len(name):], " \t")
	if !strings.HasPrefix(after, "=") {
		return Directive{}, false
	}
	value := strings.TrimLeft(after[1:], " \t")
	valueAt := at + len(content) - len(value)
	value = strings.TrimRight(value, " \t")
	return Directive{
		Name:  Word{Text: name, Span: Span{Start: nameAt, End: nameAt + len(name)}},
		Value: Word{Text: value, Span: Span{Start: valueAt, End: valueAt + len(value)}},
		Span:  Span{Start: at, End: at + len(content)},
	}, true
}

func (r *reader) continued(body string) bool {
	return strings.HasSuffix(strings.TrimRight(body, " \t"), string(r.file.Escape))
}

func (r *reader) instruction(i, start int) int {
	var l logical
	add := func(from, to int) {
		l.text += r.text[from:to]
		for k := from; k < to; k++ {
			l.at = append(l.at, k)
		}
	}
	var pieces []Segment
	end := start
	for i < len(r.lines) {
		ln := r.lines[i]
		from := max(ln.start, start)
		body := r.content(ln)[from-ln.start:]
		if !r.continued(body) {
			add(from, from+len(body))
			pieces = append(pieces, Segment{Span: Span{Start: from, End: from + len(body)}})
			end = from + len(body)
			i++
			break
		}
		trimmed := strings.TrimRight(body, " \t")
		cut := from + len(trimmed) - 1
		add(from, cut)
		pieces = append(pieces, Segment{Span: Span{Start: from, End: cut}})
		end = from + len(body)
		i++
		for i < len(r.lines) {
			next := r.lines[i]
			content := r.content(next)
			inner := strings.TrimLeft(content, " \t")
			if inner == "" {
				r.file.Empty = append(r.file.Empty, Span{Start: next.start, End: next.start + len(content)})
				i++
				continue
			}
			if inner[0] != '#' {
				break
			}
			comment := Span{Start: next.start + len(content) - len(inner), End: next.start + len(content)}
			r.file.Comments = append(r.file.Comments, comment)
			pieces = append(pieces, Segment{Span: comment, Comment: true})
			i++
		}
	}
	l.at = append(l.at, end)
	in := r.decode(l, pieces, Span{Start: start, End: end})
	i = r.heredocs(in, i)
	r.file.Instructions = append(r.file.Instructions, in)
	return i
}

func spaceAt(s string, i int) bool { return s[i] == ' ' || s[i] == '\t' }

func skipSpace(s string, i int) int {
	for i < len(s) && spaceAt(s, i) {
		i++
	}
	return i
}

func wordEnd(s string, i int) int {
	for i < len(s) && !spaceAt(s, i) {
		i++
	}
	return i
}

func (r *reader) decode(l logical, pieces []Segment, whole Span) *Instruction {
	s := l.text
	k := wordEnd(s, 0)
	in := &Instruction{Keyword: Word{Text: s[:k], Span: l.span(0, k)}, Name: strings.ToUpper(s[:k]), Span: whole, Line: whole}
	in.Pieces = pieces
	i := skipSpace(s, k)
	spec, known := instructions[in.Name]
	if !known {
		r.fail(ErrUnknownInstruction, in.Keyword.Span, "unknown instruction: %s", in.Keyword.Text)
	}
	if in.Name != "ONBUILD" {
		for i < len(s) && strings.HasPrefix(s[i:], "--") {
			end := wordEnd(s, i)
			in.Flags = append(in.Flags, r.flag(l, i, end))
			i = skipSpace(s, end)
		}
	}
	for _, f := range in.Flags {
		if known && !slices.Contains(spec.flags, f.Name.Text) {
			r.fail(ErrUnknownFlag, f.Span, "unknown flag: --%s", f.Name.Text)
		}
	}
	rest := strings.TrimRight(s[i:], " \t")
	jsonForm, shellForm := spec.json, spec.shell
	if in.Name == "HEALTHCHECK" {
		if end := wordEnd(s, i); strings.EqualFold(s[i:end], "CMD") {
			in.Args = append(in.Args, Word{Text: s[i:end], Span: l.span(i, end)})
			i = skipSpace(s, end)
			rest = strings.TrimRight(s[i:], " \t")
			jsonForm, shellForm = true, true
		}
	}
	in.rest = logical{text: s[i : i+len(rest)], at: l.at[i : i+len(rest)+1]}
	if in.Name == "ONBUILD" && rest != "" {
		inner := logical{text: s[i : i+len(rest)], at: l.at[i : i+len(rest)+1]}
		in.Inner = r.decode(inner, pieces, inner.span(0, len(inner.text)))
		return in
	}
	if jsonForm && strings.HasPrefix(rest, "[") {
		if words, ok := jsonWords(l, i, rest); ok {
			in.Form, in.JSON = FormJSON, words
			return in
		}
	}
	for j := i; j < i+len(rest); {
		end := wordEnd(s, j)
		in.Args = append(in.Args, Word{Text: s[j:end], Span: l.span(j, end)})
		j = skipSpace(s, end)
	}
	if shellForm && rest != "" {
		in.Form, in.Command = FormShell, l.span(i, i+len(rest))
	}
	return in
}

func (in *Instruction) Logical(text string, span Span) string {
	var sb strings.Builder
	for _, p := range in.Pieces {
		if p.Comment {
			continue
		}
		from, to := max(p.Span.Start, span.Start), min(p.Span.End, span.End)
		if from < to {
			sb.WriteString(text[from:to])
		}
	}
	return sb.String()
}

func (r *reader) flag(l logical, start, end int) Flag {
	text := l.text[start+2 : end]
	name, value, hasValue := strings.Cut(text, "=")
	f := Flag{Name: Word{Text: name, Span: l.span(start+2, start+2+len(name))}, Span: l.span(start, end)}
	if hasValue {
		at := start + 2 + len(name) + 1
		f.Value = &Word{Text: value, Span: l.span(at, end)}
	}
	return f
}

func jsonWords(l logical, at int, rest string) ([]Word, bool) {
	var values []string
	if json.Unmarshal([]byte(rest), &values) != nil {
		return nil, false
	}
	words := make([]Word, 0, len(values))
	i := 1
	for _, v := range values {
		for i < len(rest) && rest[i] != '"' {
			i++
		}
		end := i + 1
		for end < len(rest) && rest[end] != '"' {
			if rest[end] == '\\' {
				end++
			}
			end++
		}
		words = append(words, Word{Text: v, Span: l.span(at+i+1, at+end)})
		i = end + 1
	}
	return words, true
}

func (r *reader) heredocs(in *Instruction, i int) int {
	if !instructions[in.Name].heredoc {
		return i
	}
	for _, w := range in.Args {
		name, chomp, expand, ok := heredocMarker(w.Text)
		if !ok {
			continue
		}
		h := Heredoc{Name: Word{Text: name, Span: Span{Start: w.Span.End - len(name), End: w.Span.End}}, Chomp: chomp, Expand: expand}
		if !expand {
			h.Name.Span = Span{Start: w.Span.End - len(name) - 1, End: w.Span.End - 1}
		}
		bodyStart := len(r.text)
		if i < len(r.lines) {
			bodyStart = r.lines[i].start
		}
		h.Body = Span{Start: bodyStart, End: bodyStart}
		closed := false
		for i < len(r.lines) {
			ln := r.lines[i]
			content := r.content(ln)
			check := content
			if chomp {
				check = strings.TrimLeft(check, "\t")
			}
			i++
			if check == name {
				closed = true
				h.Span = Span{Start: w.Span.Start, End: ln.start + len(content)}
				in.Span.End = ln.start + len(content)
				break
			}
			h.Body.End = min(ln.end+1, len(r.text))
		}
		if !closed {
			r.fail(ErrSyntax, h.Name.Span, "the heredoc %s has no line %q that ends it", name, name)
			h.Span = Span{Start: w.Span.Start, End: len(r.text)}
			in.Span.End = len(r.text)
		}
		in.Heredocs = append(in.Heredocs, h)
	}
	return i
}

func heredocMarker(word string) (name string, chomp, expand, ok bool) {
	rest, found := strings.CutPrefix(word, "<<")
	if !found {
		return "", false, false, false
	}
	rest, chomp = strings.CutPrefix(rest, "-")
	expand = true
	for _, q := range []string{`"`, `'`} {
		if inner, quoted := strings.CutPrefix(rest, q); quoted {
			if !strings.HasSuffix(inner, q) || len(inner) < 2 {
				return "", false, false, false
			}
			rest, expand = inner[:len(inner)-1], false
		}
	}
	if rest == "" || strings.ContainsFunc(rest, func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '-' && c != '.'
	}) {
		return "", false, false, false
	}
	return rest, chomp, expand, true
}
