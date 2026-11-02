package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

const (
	closing     = "\n:\n)\n"
	holeStandIn = "__nix__"
	shellSub    = "$(\n"
	shellSubEnd = "\n)"
)

type bodyMode uint8

const (
	shellBody bodyMode = iota
	textBody
)

func (f *file) body(b *projection.Builder, s shellString, opener string, mode bodyMode) int {
	src := f.src
	str := s.str
	start, end := str.Content.Start, str.Content.End
	if str.Kind == syntax.Quoted {
		b.Region(quotedEncoder)
		b.Opaque(start, start+s.skip, opener)
		f.decode(b, str.Parts, start+s.skip, end, mode, quotedEncoder)
		return end
	}
	i := start
	if nl := strings.IndexByte(src[start:end], '\n'); nl >= 0 && strings.Trim(src[start:start+nl], " \t") == "" {
		i = start + nl + 1
	}
	lines := lineEnds(str)
	indent := minIndent(src, lines, i, end)
	encode := indentedEncoder(strings.Repeat(" ", indent))
	b.Region(encode)
	b.Opaque(start, i, "")
	if s.shebang {
		next := end
		for _, stop := range lines {
			if stop > i {
				next = stop
				break
			}
		}
		b.Opaque(i, next, "")
		i = next
	}
	b.Opaque(i, i, opener)
	f.indentedBody(b, str.Parts, lines, i, indent, s.skip, mode, encode)
	return end
}

func lineEnds(str *syntax.String) []int {
	var out []int
	for _, part := range str.Parts {
		literal, ok := part.(*syntax.Literal)
		if !ok {
			continue
		}
		at := literal.Span().Start
		for {
			nl := strings.IndexByte(literal.Text[at-literal.Span().Start:], '\n')
			if nl < 0 {
				break
			}
			at += nl + 1
			out = append(out, at)
		}
	}
	if n := len(out); n == 0 || out[n-1] != str.Content.End {
		out = append(out, str.Content.End)
	}
	return out
}

func leadingSpaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func minIndent(src string, lines []int, start, end int) int {
	best := -1
	at := start
	for _, stop := range lines {
		if stop <= at {
			continue
		}
		text := src[at:end]
		n := leadingSpaces(text)
		if n == len(text) {
			break
		}
		if text[n] != '\n' && (best < 0 || n < best) {
			best = n
		}
		at = stop
	}
	return max(best, 0)
}

func (f *file) indentedBody(b *projection.Builder, parts []syntax.Part, lines []int, start, indent, lead int, mode bodyMode, encode projection.Encoder) {
	at := start
	for _, stop := range lines {
		if stop <= at {
			continue
		}
		skipped := min(leadingSpaces(f.src[at:stop]), indent) + lead
		lead = 0
		b.Opaque(at, at+skipped, "")
		parts = f.decode(b, parts, at+skipped, stop, mode, encode)
		at = stop
	}
}

func (f *file) decode(b *projection.Builder, parts []syntax.Part, start, end int, mode bodyMode, encode projection.Encoder) []syntax.Part {
	from := start
	for len(parts) > 0 {
		s := parts[0].Span()
		if s.End <= start {
			parts = parts[1:]
			continue
		}
		if s.Start >= end {
			return parts
		}
		switch part := parts[0].(type) {
		case *syntax.Literal:
			if lo, hi := max(s.Start, from), min(s.End, end); lo < hi {
				b.Verbatim(lo, hi)
			}
		case *syntax.Escape:
			b.Escape(s.Start, s.End, part.Text)
		case *syntax.Interpolation:
			if mode == textBody {
				b.Placeholder(s.Start, s.End)
				break
			}
			from = f.hole(b, part, end, encode)
		}
		if s.End > end {
			return parts
		}
		parts = parts[1:]
	}
	return parts
}

func (f *file) hole(b *projection.Builder, part *syntax.Interpolation, limit int, encode projection.Encoder) int {
	span := part.Span()
	value := syntax.Unparenthesized(part.Value)
	if sp, ok := f.holeSplice(part); ok {
		f.spliced(b, span, sp, encode)
		return span.End
	}
	if pathEnd, baseEnd, ok := f.programPath(span.End, limit); ok {
		b.Program(span.Start, span.End, pathEnd, baseEnd)
		return baseEnd
	}
	if _, exe, named := programOf(value); named && exe != "" {
		b.NamedPlaceholder(span.Start, span.End, exe)
		return span.End
	}
	b.Placeholder(span.Start, span.End)
	return span.End
}

var programDirs = []string{"/bin/", "/sbin/"}

func ProgramDir(text string) bool { return slices.Contains(programDirs, text) }

func (f *file) programPath(at, limit int) (int, int, bool) {
	if at >= limit {
		return 0, 0, false
	}
	rest := f.src[at:limit]
	for _, dir := range programDirs {
		if !strings.HasPrefix(rest, dir) {
			continue
		}
		name := rest[len(dir):]
		n := strings.IndexFunc(name, func(r rune) bool {
			return !isASCIIAlnum(r) && !strings.ContainsRune("._+-", r)
		})
		if n < 0 {
			n = len(name)
		}
		if n == 0 || n < len(name) && !strings.ContainsRune(" \t\n\"';|&)\\", rune(name[n])) {
			return 0, 0, false
		}
		return at + len(dir), at + len(dir) + n, true
	}
	return 0, 0, false
}

func isASCIIAlnum(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func (f *file) insideSingleQuotes(at int) bool {
	line := f.src[strings.LastIndexByte(f.src[:at], '\n')+1 : at]
	return strings.Count(line, "'")%2 == 1
}

type splice struct {
	items   []syntax.Expr
	sep     string
	command bool
}

func spliceOf(e syntax.Expr) (splice, bool) {
	var sp splice
	if apply, isCall := syntax.Unparenthesized(e).(*syntax.Apply); isCall {
		if c := callSurfaces[calleeName(syntax.Unparenthesized(apply.Fn))]; c != nil && c.script != nil && c.script.role == scriptRole && c.positional(len(apply.Args), len(apply.Args)) {
			last := syntax.Unparenthesized(apply.Args[len(apply.Args)-1])
			if str, isString := last.(*syntax.String); isString {
				return splice{items: []syntax.Expr{last}, command: true}, str.Closed
			}
			return splice{}, false
		}
	}
	sp.collect(e)
	count := 0
	for _, item := range sp.items {
		if str, isString := item.(*syntax.String); isString {
			if !str.Closed {
				return splice{}, false
			}
			count++
		}
	}
	return sp, count > 0
}

func (s *splice) collect(e syntax.Expr) {
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.String:
		s.items = append(s.items, e)
	case *syntax.List:
		for _, item := range e.Items {
			s.collect(item)
		}
	case *syntax.Lambda:
		s.collect(e.Body)
	case *syntax.If:
		if emptyString(e.Then) {
			s.collect(e.Else)
			return
		}
		s.collect(e.Then)
	case *syntax.Binary:
		if e.Op != syntax.OpAdd {
			s.items = append(s.items, e)
			return
		}
		s.collect(e.Left)
		s.collect(e.Right)
	case *syntax.Apply:
		s.applied(e)
	default:
		s.items = append(s.items, e)
	}
}

func (s *splice) applied(e *syntax.Apply) {
	n := len(e.Args)
	name := calleeName(syntax.Unparenthesized(e.Fn))
	switch {
	case name == "optionalString" && n == 2:
		s.collect(e.Args[1])
	case name == "concatStringsSep" && n == 2:
		s.sep = stringText(e.Args[0], s.sep)
		s.collect(e.Args[1])
	case name == "concatMapStringsSep" && n == 3:
		s.sep = stringText(e.Args[0], s.sep)
		s.collect(e.Args[1])
	case name == "concatMapStrings" && n == 2, name == "map" && n == 2, name == "mapAttrsToList" && n == 2:
		s.collect(e.Args[0])
	case name == "concatStrings" && n == 1:
		s.collect(e.Args[0])
	case name == "concatLines" && n == 1:
		s.sep = "\n"
		s.collect(e.Args[0])
	default:
		s.items = append(s.items, e)
	}
}

func stringText(e syntax.Expr, fallback string) string {
	text, literal := syntax.Unparenthesized(e).(*syntax.String)
	if !literal {
		return fallback
	}
	if len(text.Parts) == 1 {
		if esc, isEscape := text.Parts[0].(*syntax.Escape); isEscape {
			return esc.Text
		}
	}
	return literalOf(text)
}

func emptyString(e syntax.Expr) bool {
	text, literal := syntax.Unparenthesized(e).(*syntax.String)
	return literal && text.Content.Start == text.Content.End
}

func (f *file) spliced(b *projection.Builder, span syntax.Span, sp splice, encode projection.Encoder) {
	at := span.Start
	open, end := "", ""
	if sp.command {
		open, end = shellSub, shellSubEnd
	}
	for i, item := range sp.items {
		sep := open
		if i > 0 {
			sep = sp.sep
		}
		if str, isString := item.(*syntax.String); isString {
			b.Opaque(at, str.Content.Start, sep)
			f.body(b, shellString{str: str}, "", shellBody)
			at = str.Content.End
			continue
		}
		s := item.Span()
		b.Opaque(at, s.Start, sep)
		b.Placeholder(s.Start, s.End)
		at = s.End
	}
	b.Opaque(at, span.End, end)
	b.Region(encode)
}

func opensAntiquote(host string, start, end int, text string) bool {
	return strings.HasPrefix(text, "{") && strings.HasSuffix(host[:start], "$") ||
		strings.HasSuffix(text, "$") && strings.HasPrefix(host[end:], "{")
}

func indentedEncoder(indent string) projection.Encoder {
	return func(host string, start, end int, text string) (string, bool) {
		quotes := strings.HasPrefix(text, "'") && strings.HasSuffix(host[:start], "'") ||
			strings.HasSuffix(text, "'") && strings.HasPrefix(host[end:], "'")
		if quotes || opensAntiquote(host, start, end, text) {
			return "", false
		}
		text = strings.ReplaceAll(text, "''", "'''")
		text = strings.ReplaceAll(text, "${", "''${")
		return strings.ReplaceAll(text, "\n", "\n"+indent), true
	}
}

var quotedEscapes = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "${", `\${`)

func quotedEncoder(host string, start, end int, text string) (string, bool) {
	if opensAntiquote(host, start, end, text) {
		return "", false
	}
	return quotedEscapes.Replace(text), true
}

func (f *file) pieces(str *syntax.String) []model.Span {
	start := str.Content.Start
	var out []model.Span
	for _, part := range str.Parts {
		hole, ok := part.(*syntax.Interpolation)
		if !ok {
			continue
		}
		span := hole.Span()
		if span.Start > start {
			out = append(out, model.Span{Start: start, End: span.Start})
		}
		if sp, spliced := f.holeSplice(hole); spliced {
			for _, item := range sp.items {
				if nested, isString := item.(*syntax.String); isString {
					out = append(out, f.pieces(nested)...)
				}
			}
		}
		start = span.End
	}
	if str.Content.End > start {
		out = append(out, model.Span{Start: start, End: str.Content.End})
	}
	return out
}

func (f *file) holeSplice(hole *syntax.Interpolation) (splice, bool) {
	sp, ok := spliceOf(hole.Value)
	return sp, ok && hole.Closed && !f.insideSingleQuotes(hole.Span().Start)
}

func (f *file) eachHole(str *syntax.String, visit func(*syntax.Interpolation)) {
	for _, part := range str.Parts {
		hole, ok := part.(*syntax.Interpolation)
		if !ok {
			continue
		}
		sp, spliced := f.holeSplice(hole)
		if !spliced {
			visit(hole)
			continue
		}
		for _, item := range sp.items {
			if nested, isString := item.(*syntax.String); isString {
				f.eachHole(nested, visit)
			}
		}
	}
}
