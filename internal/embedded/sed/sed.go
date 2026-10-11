package sed

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/embedded/regex"
)

type Syntax uint8

const (
	Basic Syntax = iota
	Extended
)

var flavors = [...]regex.Flavor{Basic: regex.Basic, Extended: regex.Extended}

type expression struct {
	start, end  int
	delimiter   byte
	replacement bool
}

const maxExpressions = 4

type (
	bailout struct{}
	abort   struct{}
)

type label struct {
	name       string
	start, end int
}

type parser struct {
	src       string
	pos       int
	res       lang.Result
	blocks    []int
	marks     []label
	jumps     []label
	structure bool
	flavor    regex.Flavor
	pending   [maxExpressions]expression
	queued    int
	commandAt int
}

func Parse(src string, syntax Syntax) lang.Result {
	p := &parser{src: src, res: lang.NewResult(src), structure: true, flavor: flavors[syntax]}
	p.run()
	return p.res
}

func (p *parser) run() {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(abort); !ok {
				panic(r)
			}
		}
	}()
	for !p.attempt() {
	}
	if !p.structure {
		return
	}
	if len(p.blocks) > 0 {
		open := p.blocks[len(p.blocks)-1]
		p.res.Fail(open, open+1, lang.Unclosed(p.src, open, open+1))
	}
	for _, jump := range p.jumps {
		if !slices.ContainsFunc(p.marks, func(m label) bool { return m.name == jump.name }) {
			p.res.Fail(jump.start, jump.end, "no label named "+lang.Quote(p.src, jump.start, jump.end))
		}
	}
}

func (p *parser) attempt() (done bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if _, ok := r.(bailout); !ok {
			panic(r)
		}
		end := lineEnd(p.src, max(p.commandAt, min(p.pos, len(p.src))))
		if strings.ContainsAny(p.src[p.commandAt:end], "{}:") {
			p.structure = false
		}
		p.pos = end
		done = false
	}()
	for {
		p.skip(" \t\r\n;")
		if p.eof() {
			return true
		}
		p.commandAt = p.pos
		p.queued = 0
		p.parseCommand()
		p.checkExpressions()
	}
}

func (p *parser) fail(start, end int, message string) {
	p.res.Fail(start, end, message)
	panic(bailout{})
}

func (p *parser) unterminated(start, end int, message string) {
	p.res.Fail(start, end, message)
	panic(abort{})
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peekIs(chars string) bool {
	return !p.eof() && strings.IndexByte(chars, p.src[p.pos]) >= 0
}

func (p *parser) skip(chars string) {
	for p.peekIs(chars) {
		p.pos++
	}
}

func (p *parser) blanks() { p.skip(" \t\r") }

func (p *parser) emit(start int, kind lang.Kind) {
	p.res.Emit(start, p.pos, kind, 0)
}

func (p *parser) parseCommand() {
	start := p.pos
	addresses := p.addresses()
	p.blanks()
	negated := false
	for p.peekIs("!") {
		if negated {
			p.fail(p.pos, p.pos+1, "multiple `!`s")
		}
		negated = true
		p.pos++
		p.emit(p.pos-1, lang.Operator)
		p.blanks()
	}
	if p.eof() || p.peekIs("\n;") {
		if addresses > 0 || negated {
			p.fail(start, p.pos, "missing command")
		}
		return
	}
	at := p.pos
	c := p.src[at]
	switch c {
	case '{':
		p.blocks = append(p.blocks, at)
		p.pos++
		return
	case '}', '#', ':':
		if addresses > 0 || negated {
			p.fail(at, at+1, lang.Quote(p.src, at, at+1)+" doesn't take an address")
		}
	}
	p.pos++
	switch c {
	case '}':
		if len(p.blocks) > 0 {
			p.blocks = p.blocks[:len(p.blocks)-1]
		} else if p.structure {
			p.fail(at, at+1, lang.Unexpected(p.src, at, at+1))
		}
		return
	case '#':
		p.pos = lineEnd(p.src, at)
		p.emit(at, lang.Comment)
		return
	}
	p.res.Emit(at, p.pos, lang.Keyword, lang.Builtin)
	switch c {
	case ':':
		name := p.label()
		if name.name == "" {
			p.fail(at, at+1, "`:` needs a label")
		}
		p.marks = append(p.marks, name)
		return
	case 'a', 'i', 'c':
		p.text(at)
		return
	case 'b', 't', 'T':
		if name := p.label(); name.name != "" {
			p.jumps = append(p.jumps, name)
		}
	case 'r', 'R', 'w', 'W':
		p.filename(at)
		return
	case 'e':
		p.blanks()
		textStart := p.pos
		p.pos = lineEnd(p.src, p.pos)
		p.emit(textStart, lang.String)
		return
	case 'v':
		p.blanks()
		version := p.pos
		p.pos = wordEnd(p.src, p.pos)
		p.emit(version, lang.String)
	case 's':
		p.substitute(at)
		return
	case 'y':
		p.transliterate(at)
	case 'q', 'Q':
		if addresses > 1 {
			p.fail(at, at+1, lang.Quote(p.src, at, at+1)+" takes at most one address")
		}
		p.number()
	case 'l', 'L':
		p.number()
	case '=', 'd', 'D', 'g', 'G', 'h', 'H', 'n', 'N', 'p', 'P', 'x', 'z', 'F':
	default:
		end := lang.RuneEnd(p.src, at)
		p.fail(at, end, "unknown command "+lang.Quote(p.src, at, end))
	}
	p.end("extra characters after command")
}

func (p *parser) end(message string) {
	p.blanks()
	if p.eof() || p.peekIs("}#") {
		return
	}
	if p.peekIs(";\n") {
		p.pos++
		return
	}
	p.fail(p.pos, lang.RuneEnd(p.src, p.pos), message)
}

func lineEnd(src string, from int) int {
	if i := strings.IndexByte(src[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(src)
}

func wordEnd(src string, from int) int {
	if i := strings.IndexAny(src[from:], "\n;}"); i >= 0 {
		return from + i
	}
	return len(src)
}

func (p *parser) label() label {
	p.blanks()
	start := p.pos
	p.pos = wordEnd(p.src, p.pos)
	name, _, _ := strings.Cut(strings.TrimRight(p.src[start:p.pos], " \t\r"), " ")
	name, _, _ = strings.Cut(name, "\t")
	end := start + len(name)
	p.res.Emit(start, end, lang.Property, 0)
	return label{name: name, start: start, end: end}
}

func (p *parser) filename(command int) {
	p.blanks()
	start := p.pos
	p.pos = lineEnd(p.src, p.pos)
	if p.pos == start && (p.src[command] == 'w' || p.src[command] == 'W') {
		p.fail(command, command+1, lang.Quote(p.src, command, command+1)+" needs a file name")
	}
	p.emit(start, lang.String)
}

func (p *parser) number() {
	p.blanks()
	start := p.pos
	p.skip("0123456789")
	p.emit(start, lang.Number)
}

func (p *parser) text(command int) {
	p.blanks()
	if p.eof() {
		p.fail(command, command+1, lang.Quote(p.src, command, command+1)+" needs text")
	}
	if p.peekIs(`\`) {
		p.pos++
		switch {
		case strings.HasPrefix(p.src[p.pos:], "\n"):
			p.pos++
		case strings.HasPrefix(p.src[p.pos:], "\r\n"):
			p.pos += 2
		}
	}
	start := p.pos
	for !p.eof() && p.src[p.pos] != '\n' {
		if p.src[p.pos] == '\\' {
			p.pos++
		}
		p.pos++
	}
	p.pos = min(p.pos, len(p.src))
	p.emit(start, lang.String)
}

type address uint8

const (
	noAddress address = iota
	lineAddress
	zeroAddress
	lastAddress
	regexpAddress
	stepAddress
)

func (p *parser) addresses() int {
	start := p.pos
	first := p.address(false)
	if first == noAddress {
		return 0
	}
	end := p.pos
	p.blanks()
	if !p.peekIs(",") {
		p.pos = end
		if first == zeroAddress {
			p.fail(start, end, "line address 0 is only valid as `0,/regexp/`")
		}
		return 1
	}
	p.pos++
	p.emit(p.pos-1, lang.Operator)
	p.blanks()
	second := p.address(true)
	if second == noAddress {
		p.fail(p.pos-1, p.pos, "missing address after `,`")
	}
	if first == zeroAddress && second != regexpAddress {
		p.fail(start, end, "line address 0 is only valid as `0,/regexp/`")
	}
	return 2
}

func (p *parser) address(second bool) address {
	if p.eof() {
		return noAddress
	}
	start := p.pos
	switch c := p.src[p.pos]; {
	case lang.IsDigit(c):
		p.skip("0123456789")
		p.emit(start, lang.Number)
		if !second && p.peekIs("~") {
			p.step()
			return stepAddress
		}
		if strings.Trim(p.src[start:p.pos], "0") == "" {
			return zeroAddress
		}
		return lineAddress
	case c == '$':
		p.pos++
		p.emit(start, lang.Operator)
		return lastAddress
	case c == '/':
		p.regexpAddress(start, '/')
		return regexpAddress
	case c == '\\':
		p.pos++
		if p.eof() || p.peekIs("\n\\") {
			p.unterminated(start, p.pos, "unterminated address regex")
		}
		p.regexpAddress(start, p.src[p.pos])
		return regexpAddress
	case second && (c == '+' || c == '~'):
		p.step()
		return stepAddress
	}
	return noAddress
}

func (p *parser) step() {
	p.pos++
	p.emit(p.pos-1, lang.Operator)
	start := p.pos
	p.skip("0123456789")
	if p.pos == start {
		p.fail(start-1, start, "expected a number after "+lang.Quote(p.src, start-1, start))
	}
	p.emit(start, lang.Number)
}

func (p *parser) regexpAddress(start int, delimiter byte) {
	p.pos++
	from := p.pos
	if !p.delimitedRegex(delimiter) {
		p.unterminated(start, p.pos, "unterminated address regex")
	}
	p.queue(expression{start: from, end: p.pos, delimiter: delimiter})
	p.pos++
	p.emit(start, lang.Regexp)
	for p.peekIs("IM") {
		p.pos++
		p.option(p.pos-1, lang.Keyword)
	}
}

func (p *parser) option(start int, kind lang.Kind) {
	p.res.Emit(start, p.pos, kind, lang.Builtin|lang.Option)
}

func (p *parser) delimited(delimiter byte) bool { return p.scanTo(delimiter, false) }

func (p *parser) delimitedRegex(delimiter byte) bool { return p.scanTo(delimiter, true) }

func (p *parser) scanTo(delimiter byte, brackets bool) bool {
	for !p.eof() {
		switch c := p.src[p.pos]; {
		case c == '\\':
			p.pos += 2
			continue
		case c == '\n':
			return false
		case c == delimiter:
			return true
		case brackets && c == '[':
			if end, ok := regex.BracketEnd(p.src[:lineEnd(p.src, p.pos)], p.pos); ok {
				p.pos = end
				continue
			}
		}
		p.pos++
	}
	p.pos = len(p.src)
	return false
}

func (p *parser) substitute(command int) {
	if p.eof() || p.peekIs("\n\\") || !p.substitution() {
		p.unterminated(command, command+1, "unterminated `s` command")
	}
	p.substituteFlags()
}

func (p *parser) substitution() bool {
	delimiter := p.src[p.pos]
	start := p.pos
	p.pos++
	if !p.delimitedRegex(delimiter) {
		return false
	}
	p.queue(expression{start: start + 1, end: p.pos, delimiter: delimiter})
	p.pos++
	p.emit(start, lang.Regexp)
	start = p.pos
	if !p.delimited(delimiter) {
		return false
	}
	p.queue(expression{start: start, end: p.pos, delimiter: delimiter, replacement: true})
	p.pos++
	p.emit(start, lang.String)
	return true
}

func (p *parser) queue(e expression) {
	p.pending[p.queued] = e
	p.queued++
}

func (p *parser) checkExpressions() {
	var pattern regex.Pattern
	for _, e := range p.pending[:p.queued] {
		if e.replacement {
			p.references(e, pattern)
			continue
		}
		pattern = p.regex(e)
	}
}

func (p *parser) regex(e expression) regex.Pattern {
	if e.start == e.end || lang.IsNameChar(e.delimiter) {
		return regex.Pattern{}
	}
	return p.res.CheckRegex(p.excerpt(e.start, e.end, e.delimiter), p.flavor)
}

func (p *parser) excerpt(start, end int, delimiter byte) lang.Excerpt {
	escaped := string([]byte{'\\', delimiter})
	if !strings.Contains(p.src[start:end], escaped) {
		return lang.Verbatim(p.src, start, end)
	}
	b := lang.NewExcerptBuilder(end - start)
	for i := start; i < end; i++ {
		switch {
		case strings.HasPrefix(p.src[i:end], escaped):
			b.Add(escaped[1:], i, i+2)
			i++
		case p.src[i] == '\\' && i+1 < end:
			b.Add(p.src[i:i+1], i, i+1)
			b.Add(p.src[i+1:i+2], i+1, i+2)
			i++
		default:
			b.Add(p.src[i:i+1], i, i+1)
		}
	}
	return b.Excerpt()
}

func (p *parser) references(e expression, pattern regex.Pattern) {
	groups, known := pattern.Groups()
	if !known || lang.IsDigit(e.delimiter) {
		return
	}
	for i := e.start; i+1 < e.end; i++ {
		if p.src[i] != '\\' {
			continue
		}
		i++
		if c := p.src[i]; c >= '1' && c <= '9' && int(c-'0') > groups {
			p.res.Fail(i-1, i+1, "`"+p.src[i-1:i+1]+"` refers to group "+string(c)+", but the regular expression has "+regex.GroupCount(groups))
		}
	}
}

type substituteFlag uint8

const (
	globalFlag substituteFlag = 1 << iota
	printFlag
	countFlag
)

func (p *parser) once(seen *substituteFlag, flag substituteFlag, message string) {
	if *seen&flag != 0 {
		p.fail(p.pos, p.pos+1, message)
	}
	*seen |= flag
}

func (p *parser) substituteFlags() {
	var seen substituteFlag
	for !p.eof() {
		switch c := p.src[p.pos]; {
		case c == 'g':
			p.once(&seen, globalFlag, "multiple `g` options to `s`")
		case c == 'p':
			p.once(&seen, printFlag, "multiple `p` options to `s`")
		case lang.IsDigit(c):
			p.once(&seen, countFlag, "multiple number options to `s`")
			digits := p.pos
			p.skip("0123456789")
			p.option(digits, lang.Number)
			if strings.Trim(p.src[digits:p.pos], "0") == "" {
				p.fail(digits, p.pos, "the number option to `s` may not be zero")
			}
			continue
		case c == 'i' || c == 'I' || c == 'm' || c == 'M' || c == 'e':
		case c == 'w':
			p.pos++
			p.option(p.pos-1, lang.Keyword)
			p.filename(p.pos - 1)
			return
		default:
			p.end("unknown option to `s`")
			return
		}
		p.pos++
		p.option(p.pos-1, lang.Keyword)
	}
}

func (p *parser) transliterate(command int) {
	if p.eof() || p.peekIs("\n\\") {
		p.unterminated(command, command+1, "unterminated `y` command")
	}
	delimiter := p.src[p.pos]
	p.pos++
	source, exact := p.transliteration(command, delimiter)
	target, exactTarget := p.transliteration(command, delimiter)
	if exact && exactTarget && source != target {
		p.fail(command, command+1, "the strings of `y` have different lengths")
	}
}

func (p *parser) transliteration(command int, delimiter byte) (int, bool) {
	start := p.pos
	length, exact := 0, true
	for {
		if p.eof() || p.src[p.pos] == '\n' {
			p.unterminated(command, command+1, "unterminated `y` command")
		}
		c := p.src[p.pos]
		if c == delimiter {
			break
		}
		if c == '\\' && p.pos+1 < len(p.src) {
			next := p.src[p.pos+1]
			exact = exact && (next == '\\' || next == delimiter || next == 'n')
			p.pos += 2
			length++
			continue
		}
		p.pos = lang.RuneEnd(p.src, p.pos)
		length++
	}
	p.emit(start, lang.String)
	p.pos++
	return length, exact
}
