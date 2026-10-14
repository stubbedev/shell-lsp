package glob

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type SegmentKind uint8

const (
	SegmentPattern SegmentKind = iota
	SegmentText
	SegmentLiteral
	SegmentUnknown
	SegmentQualifiers
)

type Segment struct {
	Span parser.Span
	Kind SegmentKind
	Text string
	Glob bool
}

type item struct {
	c       byte
	at      parser.Span
	active  bool
	escaped bool
	unknown *Segment
	quals   *Segment
}

type parse struct {
	items   []item
	pos     int
	syn     Syntax
	out     *Pattern
	cs      Case
	capture bool
}

func Parse(segments []Segment, syn Syntax) Pattern {
	out := Pattern{Syntax: syn}
	p := &parse{items: lex(segments), syn: syn, out: &out}
	out.Root = p.alternation(0)
	return out
}

func lex(segments []Segment) []item {
	size := 0
	for i := range segments {
		size += len(segments[i].Text)
	}
	items := make([]item, 0, size)
	for i := range segments {
		s := &segments[i]
		switch s.Kind {
		case SegmentPattern:
			items = lexPattern(items, s, true)
		case SegmentText:
			items = lexPattern(items, s, false)
		case SegmentLiteral:
			for j := range len(s.Text) {
				items = append(items, item{c: s.Text[j], at: s.Span})
			}
		case SegmentUnknown:
			items = append(items, item{at: s.Span, unknown: s})
		case SegmentQualifiers:
			items = append(items, item{at: s.Span, quals: s})
		}
	}
	return items
}

func lexPattern(items []item, s *Segment, quotes bool) []item {
	var quote byte
	for j := 0; j < len(s.Text); j++ {
		c := s.Text[j]
		at := parser.Span{Start: s.Span.Start + j, End: s.Span.Start + j + 1}
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == '\'' || quote == '"' && c != '\\':
			items = append(items, item{c: c, at: at})
		case c == '\\' && j+1 < len(s.Text):
			items = append(items, item{c: s.Text[j+1], at: parser.Span{Start: at.Start, End: at.End + 1}, escaped: quote == 0})
			j++
		case quotes && quote == 0 && (c == '\'' || c == '"'):
			quote = c
		default:
			items = append(items, item{c: c, at: at, active: quote == 0})
		}
	}
	return items
}

func sourceOffsets(s *Segment) []int {
	offsets := make([]int, len(s.Text)+1)
	for i := range offsets {
		offsets[i] = s.Span.Start + i
	}
	return offsets
}

func (p *parse) zsh() bool { return p.syn.Dialect == dialect.Zsh }

func (p *parse) kshGroups() bool {
	switch p.syn.Dialect {
	case dialect.Bash, dialect.Ksh:
		return true
	case dialect.Zsh:
		return p.syn.KshGlob
	case dialect.Posix, dialect.Dash, dialect.Busybox:
	}
	return false
}

func (p *parse) peek(offset int) (item, bool) {
	if p.pos+offset >= len(p.items) {
		return item{}, false
	}
	return p.items[p.pos+offset], true
}

func (p *parse) activeAt(offset int, c byte) bool {
	it, ok := p.peek(offset)
	return ok && it.active && it.c == c
}

func (p *parse) span(from int) parser.Span {
	if len(p.items) == 0 {
		return parser.Span{}
	}
	if from >= p.pos {
		at := p.items[min(from, len(p.items)-1)].at
		return parser.Span{Start: at.Start, End: at.Start}
	}
	return parser.Span{Start: p.items[from].at.Start, End: p.items[p.pos-1].at.End}
}

type snapshot struct {
	pos, needs, errs, captures int
	cs                         Case
	capture, setsMatch, lazy   bool
	unevaluable                string
}

func (p *parse) save() snapshot {
	return snapshot{
		pos: p.pos, needs: len(p.out.Needs), errs: len(p.out.Errors), captures: p.out.Captures,
		cs: p.cs, capture: p.capture, setsMatch: p.out.SetsMatch, unevaluable: p.out.Unevaluable, lazy: p.out.Lazy,
	}
}

func (p *parse) restore(s snapshot) {
	p.pos, p.cs, p.capture = s.pos, s.cs, s.capture
	p.out.Needs, p.out.Errors = p.out.Needs[:s.needs], p.out.Errors[:s.errs]
	p.out.Captures, p.out.SetsMatch, p.out.Unevaluable, p.out.Lazy = s.captures, s.setsMatch, s.unevaluable, s.lazy
}

func (p *parse) inGroup(depth int) Node {
	cs, capture := p.cs, p.capture
	x := p.alternation(depth + 1)
	p.cs, p.capture = cs, capture
	return x
}

func (p *parse) need(span parser.Span, o Option) {
	p.out.Needs = append(p.out.Needs, Need{Span: span, Option: o})
}

func (p *parse) fail(span parser.Span, format string, args ...any) {
	p.out.Errors = append(p.out.Errors, Error{Span: span, Message: fmt.Sprintf(format, args...)})
}

func (p *parse) alternation(depth int) Node {
	start := p.pos
	choices := []Node{p.exclusion(depth)}
	for depth > 0 && p.activeAt(0, '|') {
		p.pos++
		choices = append(choices, p.exclusion(depth))
	}
	if len(choices) == 1 {
		return choices[0]
	}
	return &Alternatives{At: p.span(start), Choices: choices}
}

func (p *parse) exclusion(depth int) Node {
	start := p.pos
	base := p.sequence(depth, false)
	var excepts []Node
	for p.zsh() && p.pos > 0 && p.activeAt(0, '~') && !p.stopsAfter(1, depth) {
		p.need(p.items[p.pos].at, ExtendedGlob)
		p.pos++
		excepts = append(excepts, p.sequence(depth, false))
	}
	if len(excepts) == 0 {
		return base
	}
	return &Exclude{At: p.span(start), Base: base, Except: excepts}
}

func (p *parse) stopsAfter(offset, depth int) bool {
	it, ok := p.peek(offset)
	return !ok || it.active && (it.c == '|' || it.c == ')') && depth > 0
}

func (p *parse) stops(depth int, slash bool) bool {
	it, ok := p.peek(0)
	if !ok {
		return true
	}
	if !it.active {
		return false
	}
	switch it.c {
	case '|', ')':
		return depth > 0
	case '~':
		return p.zsh() && p.pos > 0 && !p.stopsAfter(1, depth)
	case '/':
		return slash
	}
	return false
}

func (p *parse) sequence(depth int, slash bool) Node {
	start := p.pos
	var items []Node
	for !p.stops(depth, slash) {
		if p.zsh() && p.activeAt(0, '^') {
			notStart := p.pos
			p.pos++
			x := p.sequence(depth, p.syn.Filenames)
			not := &Not{At: p.span(notStart), X: x}
			p.need(not.At, ExtendedGlob)
			items = append(items, not)
			continue
		}
		unit := p.unit(depth)
		items = append(items, p.postfix(unit))
	}
	return &Seq{At: p.span(start), Items: mergeLiterals(items)}
}

func mergeLiterals(items []Node) []Node {
	out := items[:0]
	for i := 0; i < len(items); {
		first, ok := items[i].(*Literal)
		out = append(out, items[i])
		i++
		if !ok {
			continue
		}
		var text strings.Builder
		merged := false
		for ; i < len(items); i++ {
			next, ok := items[i].(*Literal)
			if !ok || next.Case != first.Case {
				break
			}
			if !merged {
				text.WriteString(first.Text)
				merged = true
			}
			text.WriteString(next.Text)
			first.At.End = max(first.At.End, next.At.End)
		}
		if merged {
			first.Text = text.String()
		}
	}
	return out
}

func (p *parse) literal() *Literal {
	r, width := p.runeAt(0)
	at := parser.Span{Start: p.items[p.pos].at.Start, End: p.items[p.pos+width-1].at.End}
	p.pos += width
	text := string(r)
	if r == utf8.RuneError && width == 1 {
		text = string([]byte{p.items[p.pos-1].c})
	}
	return &Literal{At: at, Text: text, Case: p.cs}
}

func (p *parse) unit(depth int) Node {
	it := p.items[p.pos]
	switch {
	case it.unknown != nil:
		p.pos++
		return &Unknown{At: it.at, Source: it.unknown.Text, Glob: it.unknown.Glob}
	case it.quals != nil:
		p.pos++
		return parseQualifiers(it.quals.Text, sourceOffsets(it.quals), strings.HasPrefix(it.quals.Text, "(#q"))
	case !it.active:
		return p.literal()
	}
	if p.kshOptionsAt() {
		if n, ok := p.kshOptions(depth); ok {
			return n
		}
	}
	if n, ok := p.kshGroup(depth); ok {
		return n
	}
	switch it.c {
	case '*':
		if n, ok := p.recursive(); ok {
			return n
		}
		p.pos++
		return &AnyString{At: it.at}
	case '?':
		p.pos++
		return &AnyChar{At: it.at}
	case '[':
		if n, ok := p.bracket(); ok {
			return n
		}
	case '<':
		if n, ok := p.numRange(); ok {
			return n
		}
	case '(':
		if p.zsh() {
			return p.zshGroup(depth)
		}
	}
	return p.literal()
}

func (p *parse) atSegmentStart() bool {
	if p.pos == 0 {
		return true
	}
	prev := p.items[p.pos-1]
	return prev.active && prev.c == '/'
}

func (p *parse) recursive() (Node, bool) {
	if !p.syn.Filenames || !p.atSegmentStart() || !p.activeAt(1, '*') {
		return nil, false
	}
	start := p.pos
	follow := p.zsh() && p.activeAt(2, '*')
	stars := 2
	if follow {
		stars = 3
	}
	slash := p.activeAt(stars, '/')
	_, more := p.peek(stars)
	if !slash && (more || p.zsh()) {
		return nil, false
	}
	p.pos += stars
	if slash {
		p.pos++
	}
	n := &Recursive{At: p.span(start), Follow: follow}
	if p.syn.Dialect == dialect.Bash {
		p.need(n.At, GlobStar)
	}
	return n, true
}

var kshGroupOps = map[byte]GroupOp{
	'?': GroupZeroOrOne, '*': GroupZeroOrMore, '+': GroupOneOrMore, '@': GroupOne, '!': GroupNot,
}

func (p *parse) kshGroup(depth int) (Node, bool) {
	if !p.kshGroups() {
		return nil, false
	}
	it := p.items[p.pos]
	op, ok := kshGroupOps[it.c]
	open := 1
	lo, hi := 0, 0
	if !ok {
		var counted bool
		lo, hi, open, counted = p.kshCount()
		if !counted {
			return nil, false
		}
		op = GroupCounted
	}
	lazy := p.syn.Dialect == dialect.Ksh && p.activeAt(open, '-') && p.activeAt(open+1, '(')
	if lazy {
		open++
	}
	if !p.activeAt(open, '(') {
		return nil, false
	}
	start := p.pos
	saved := p.save()
	p.pos += open + 1
	x := p.inGroup(depth)
	if !p.activeAt(0, ')') {
		p.restore(saved)
		return nil, false
	}
	p.pos++
	g := &Group{At: p.span(start), Op: op, X: x, Min: lo, Max: hi, Lazy: lazy}
	p.out.Lazy = p.out.Lazy || lazy
	if p.syn.Dialect == dialect.Bash {
		p.need(g.At, ExtGlob)
	}
	return g, true
}

func (p *parse) kshCount() (lo, hi, width int, ok bool) {
	if p.syn.Dialect != dialect.Ksh || !p.activeAt(0, '{') {
		return 0, 0, 0, false
	}
	var text strings.Builder
	for i := 1; ; i++ {
		it, more := p.peek(i)
		if !more || !it.active {
			return 0, 0, 0, false
		}
		if it.c == '}' {
			width = i + 1
			break
		}
		text.WriteByte(it.c)
	}
	low, high, comma := strings.Cut(text.String(), ",")
	lo, err := strconv.Atoi(low)
	if err != nil {
		return 0, 0, 0, false
	}
	hi = lo
	if comma {
		hi = unbounded
		if high != "" {
			if hi, err = strconv.Atoi(high); err != nil || hi < lo {
				return 0, 0, 0, false
			}
		}
	}
	return lo, hi, width, true
}

func (p *parse) zshGroup(depth int) Node {
	start := p.pos
	if p.activeAt(1, '#') {
		if n, ok := p.globFlags(); ok {
			return n
		}
	}
	p.pos++
	capture := 0
	if p.capture {
		p.out.Captures++
		capture = p.out.Captures
	}
	x := p.inGroup(depth)
	if !p.activeAt(0, ')') {
		p.fail(p.items[start].at, "unclosed group")
		return x
	}
	p.pos++
	return &Group{At: p.span(start), Op: GroupPlain, X: x, Capture: capture}
}

func (p *parse) closingParen(from int) (int, bool) {
	for i := from; i < len(p.items)-p.pos; i++ {
		it := p.items[p.pos+i]
		if it.unknown != nil || it.quals != nil {
			return 0, false
		}
		if it.active && it.c == ')' {
			return i, true
		}
	}
	return 0, false
}

func (p *parse) flagText(end int) string {
	var sb strings.Builder
	for i := 2; i < end; i++ {
		sb.WriteByte(p.items[p.pos+i].c)
	}
	return sb.String()
}

func (p *parse) globFlags() (Node, bool) {
	end, ok := p.closingParen(2)
	if !ok {
		return nil, false
	}
	text := p.flagText(end)
	start := p.pos
	if strings.HasPrefix(text, "q") {
		offsets := make([]int, 0, end+2)
		for i := range end + 1 {
			offsets = append(offsets, p.items[p.pos+i].at.Start)
		}
		offsets = append(offsets, p.items[p.pos+end].at.End)
		p.pos += end + 1
		q := parseQualifiers("(#"+text+")", offsets, true)
		p.need(q.At, ExtendedGlob)
		return q, true
	}
	if strings.HasPrefix(text, "c") {
		return nil, false
	}
	flags, err := parseFlags(text)
	p.pos += end + 1
	at := p.span(start)
	p.need(at, ExtendedGlob)
	if err != "" {
		p.fail(at, "%s", err)
	}
	for _, f := range flags {
		switch f.Name {
		case 'i':
			p.cs = CaseInsensitive
		case 'l':
			p.cs = CaseLowerFolds
		case 'I':
			p.cs = CaseSensitive
		case 'b':
			p.capture = true
		case 'B':
			p.capture = false
		case 'm':
			p.out.SetsMatch = true
		case 'a':
			if f.Arg != "0" {
				p.out.Unevaluable = "approximate matching with (#a)"
			}
		case 's', 'e':
			return &Anchor{At: at, Start: f.Name == 's'}, true
		}
	}
	return &Flags{At: at, Flags: flags}, true
}

func parseFlags(text string) ([]Flag, string) {
	var flags []Flag
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch c {
		case 'i', 'l', 'I', 'b', 'B', 'm', 'M', 'u', 'U':
			flags = append(flags, Flag{Name: c})
		case 's', 'e':
			if len(text) != 1 {
				return flags, fmt.Sprintf("(#%c) must stand alone in its group", c)
			}
			flags = append(flags, Flag{Name: c})
		case 'a':
			j := i + 1
			for j < len(text) && text[j] >= '0' && text[j] <= '9' {
				j++
			}
			if j == i+1 {
				return flags, "(#a) needs the number of errors to allow, as in (#a1)"
			}
			flags = append(flags, Flag{Name: c, Arg: text[i+1 : j]})
			i = j - 1
		default:
			return flags, fmt.Sprintf("unknown globbing flag %q", string(c))
		}
	}
	return flags, ""
}

func repeatable(n Node) bool {
	switch n := n.(type) {
	case *Literal:
		return utf8.RuneCountInString(n.Text) == 1
	case *AnyChar, *Bracket, *NumRange, *Group, *Unknown:
		return true
	}
	return false
}

func (p *parse) postfix(unit Node) Node {
	if !p.zsh() || !repeatable(unit) {
		return unit
	}
	start := unit.Span()
	switch {
	case p.activeAt(0, '#'):
		lo := 0
		p.pos++
		if p.activeAt(0, '#') {
			lo = 1
			p.pos++
		}
		r := &Repeat{At: parser.Span{Start: start.Start, End: p.items[p.pos-1].at.End}, X: unit, Min: lo, Max: unbounded}
		p.need(parser.Span{Start: start.End, End: r.At.End}, ExtendedGlob)
		return p.postfix(r)
	case p.activeAt(0, '(') && p.activeAt(1, '#') && p.activeAt(2, 'c'):
		end, ok := p.closingParen(3)
		if !ok {
			return unit
		}
		lo, hi, ok := repeatCount(p.flagText(end)[1:])
		if !ok {
			p.fail(p.items[p.pos].at, "(#c) needs a count or a range, as in (#c2) or (#c1,3)")
			return unit
		}
		opStart := p.items[p.pos].at.Start
		p.pos += end + 1
		r := &Repeat{At: parser.Span{Start: start.Start, End: p.items[p.pos-1].at.End}, X: unit, Min: lo, Max: hi}
		p.need(parser.Span{Start: opStart, End: r.At.End}, ExtendedGlob)
		return r
	}
	return unit
}

func repeatCount(text string) (int, int, bool) {
	low, high, comma := strings.Cut(text, ",")
	lo, err := strconv.Atoi(low)
	if err != nil || lo < 0 {
		return 0, 0, false
	}
	if !comma {
		return lo, lo, true
	}
	if high == "" {
		return lo, unbounded, true
	}
	hi, err := strconv.Atoi(high)
	if err != nil || hi < lo {
		return 0, 0, false
	}
	return lo, hi, true
}

func (p *parse) numRange() (Node, bool) {
	if !p.zsh() {
		return nil, false
	}
	var lo, hi strings.Builder
	dash := false
	for i := 1; ; i++ {
		it, ok := p.peek(i)
		if !ok || it.unknown != nil || it.quals != nil {
			return nil, false
		}
		switch {
		case it.c == '>' && it.active:
			if !dash {
				return nil, false
			}
			start := p.pos
			p.pos += i + 1
			n := &NumRange{At: p.span(start)}
			n.Lo, n.HasLo = parseBound(lo.String())
			n.Hi, n.HasHigh = parseBound(hi.String())
			return n, true
		case it.c == '-' && !dash:
			dash = true
		case it.c >= '0' && it.c <= '9':
			if dash {
				hi.WriteByte(it.c)
			} else {
				lo.WriteByte(it.c)
			}
		default:
			return nil, false
		}
	}
}

func parseBound(text string) (uint64, bool) {
	if text == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(text, 10, 64)
	return v, err == nil
}

func (p *parse) bracket() (Node, bool) {
	i := 1
	negated := false
	negators := "!^"
	if p.syn.Regex != NotRegex {
		negators = "^"
	}
	if it, ok := p.peek(i); ok && it.active && strings.IndexByte(negators, it.c) >= 0 {
		negated = true
		i++
	}
	var items []BracketItem
	first := true
	for {
		it, ok := p.peek(i)
		if !ok || it.unknown != nil || it.quals != nil {
			return nil, false
		}
		if it.c == ']' && it.active && !first {
			break
		}
		first = false
		if it.escaped && p.syn.Regex != NotRegex {
			items = append(items, BracketItem{Lo: '\\', Hi: '\\'})
		}
		if it.active && it.c == '[' {
			if class, width, ok := p.bracketClass(i); ok {
				items = append(items, class)
				i += width
				continue
			}
		}
		lo, width := p.runeAt(i)
		i += width
		hi := lo
		if p.activeAt(i, '-') {
			if next, ok := p.peek(i + 1); ok && (!next.active || next.c != ']') && next.unknown == nil && next.quals == nil {
				hi, width = p.runeAt(i + 1)
				i += 1 + width
			}
		}
		items = append(items, BracketItem{Lo: lo, Hi: hi})
	}
	start := p.pos
	p.pos += i + 1
	return &Bracket{At: p.span(start), Negated: negated, Items: items, Case: p.cs}, true
}

func (p *parse) bracketClass(at int) (BracketItem, int, bool) {
	open, ok := p.peek(at + 1)
	if !ok || !open.active || (open.c != ':' && open.c != '=' && open.c != '.') {
		return BracketItem{}, 0, false
	}
	var name strings.Builder
	for i := at + 2; ; i++ {
		it, ok := p.peek(i)
		if !ok || it.unknown != nil || it.quals != nil {
			return BracketItem{}, 0, false
		}
		if it.c == open.c && p.activeAt(i+1, ']') {
			width := i + 2 - at
			if open.c == ':' {
				return BracketItem{Class: name.String()}, width, true
			}
			r, _ := utf8.DecodeRuneInString(name.String())
			return BracketItem{Lo: r, Hi: r}, width, true
		}
		name.WriteByte(it.c)
	}
}

func (p *parse) runeAt(at int) (rune, int) {
	var buf [utf8.UTFMax]byte
	n := 0
	for n < utf8.UTFMax {
		it, ok := p.peek(at + n)
		if !ok || it.unknown != nil || it.quals != nil {
			break
		}
		buf[n] = it.c
		n++
		if utf8.FullRune(buf[:n]) {
			break
		}
	}
	r, width := utf8.DecodeRune(buf[:n])
	return r, max(width, 1)
}
