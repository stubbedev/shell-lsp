package yaml

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

type Kind uint8

const (
	Scalar Kind = iota + 1
	Sequence
	Mapping
)

func (k Kind) String() string {
	switch k {
	case Scalar:
		return "a scalar"
	case Sequence:
		return "a sequence"
	case Mapping:
		return "a mapping"
	}
	return "nothing"
}

type Node struct {
	Kind   Kind
	Value  string
	Quoted bool
	Items  []*Node
	Pairs  []Pair
	Line   int
	Column int
}

type Pair struct {
	Key   *Node
	Value *Node
}

func (n *Node) Null() bool {
	if n == nil {
		return true
	}
	return n.Kind == Scalar && !n.Quoted && slices.Contains(nulls, n.Value)
}

func (n *Node) Bool() (bool, bool) {
	if n.Kind != Scalar || n.Quoted {
		return false, false
	}
	switch {
	case slices.Contains(trues, n.Value):
		return true, true
	case slices.Contains(falses, n.Value):
		return false, true
	}
	return false, false
}

func (n *Node) Get(key string) (*Node, bool) {
	for _, p := range n.Pairs {
		if p.Key.Value == key {
			return p.Value, true
		}
	}
	return nil, false
}

var (
	nulls  = []string{"", "~", "null", "Null", "NULL"}
	trues  = []string{"true", "True", "TRUE"}
	falses = []string{"false", "False", "FALSE"}
)

type Error struct {
	Line    int
	Column  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

const byteOrderMark = "\uFEFF"

func Parse(text string) (*Node, error) {
	p := &parser{text: strings.TrimPrefix(text, byteOrderMark)}
	for i := range len(p.text) {
		if p.text[i] == '\n' {
			p.lineStarts = append(p.lineStarts, i+1)
		}
	}
	return p.document()
}

type parser struct {
	text       string
	lineStarts []int
}

func (p *parser) position(pos int) (int, int) {
	line, _ := slices.BinarySearch(p.lineStarts, pos+1)
	start := 0
	if line > 0 {
		start = p.lineStarts[line-1]
	}
	return line + 1, utf8.RuneCountInString(p.text[start:pos]) + 1
}

func (p *parser) failAt(pos int, format string, args ...any) error {
	line, column := p.position(pos)
	return &Error{Line: line, Column: column, Message: fmt.Sprintf(format, args...)}
}

func (p *parser) column(pos int) int {
	return pos - (strings.LastIndexByte(p.text[:pos], '\n') + 1)
}

func (p *parser) at(pos int) byte {
	if pos >= len(p.text) {
		return 0
	}
	return p.text[pos]
}

func (p *parser) lineEnd(pos int) int {
	if i := strings.IndexByte(p.text[pos:], '\n'); i >= 0 {
		return pos + i
	}
	return len(p.text)
}

func breakOrEnd(b byte) bool { return b == '\n' || b == '\r' || b == 0 }

func (p *parser) skipBlanks(pos int) int {
	for scan.Blank(p.at(pos)) {
		pos++
	}
	return pos
}

func (p *parser) restIsEmpty(pos int) bool {
	pos = p.skipBlanks(pos)
	return breakOrEnd(p.at(pos)) || p.at(pos) == '#'
}

func (p *parser) nextContent(pos int) (int, error) {
	for pos < len(p.text) {
		start := pos
		indent := start
		for p.at(indent) == ' ' {
			indent++
		}
		content := p.skipBlanks(indent)
		if breakOrEnd(p.at(content)) || p.at(content) == '#' {
			pos = p.lineEnd(start) + 1
			continue
		}
		if content != indent {
			return 0, p.failAt(indent, "a tab indents this line; YAML indents with spaces only")
		}
		return content, nil
	}
	return len(p.text), nil
}

func (p *parser) document() (*Node, error) {
	pos, err := p.nextContent(0)
	if err != nil {
		return nil, err
	}
	if p.at(pos) == '%' {
		return nil, p.failAt(pos, "YAML directives are not supported")
	}
	if p.marker(pos, "---") {
		if !p.restIsEmpty(pos + 3) {
			return nil, p.failAt(pos+3, "content after the --- that starts the document is not supported")
		}
		if pos, err = p.nextContent(p.lineEnd(pos)); err != nil {
			return nil, err
		}
	}
	var root *Node
	if pos < len(p.text) && !p.marker(pos, "...") {
		if p.column(pos) != 0 {
			return nil, p.failAt(pos, "the document is indented; its top level must start in the first column")
		}
		if root, pos, err = p.block(pos, -1); err != nil {
			return nil, err
		}
	}
	if pos, err = p.nextContent(pos); err != nil {
		return nil, err
	}
	if p.marker(pos, "...") {
		if !p.restIsEmpty(pos + 3) {
			return nil, p.failAt(pos+3, "content after the ... that ends the document")
		}
		if pos, err = p.nextContent(p.lineEnd(pos)); err != nil {
			return nil, err
		}
	}
	switch {
	case pos >= len(p.text):
		return root, nil
	case p.marker(pos, "---"):
		return nil, p.failAt(pos, "a second document starts here; only one document is supported")
	}
	return nil, p.failAt(pos, "unexpected content; check the indentation")
}

func (p *parser) marker(pos int, m string) bool {
	return p.column(pos) == 0 && strings.HasPrefix(p.text[pos:], m) && (scan.Blank(p.at(pos+3)) || breakOrEnd(p.at(pos+3)))
}

func (p *parser) indicator(pos int) bool {
	b := p.at(pos + 1)
	return scan.Blank(b) || breakOrEnd(b)
}

func (p *parser) block(pos, parent int) (*Node, int, error) {
	indent := p.column(pos)
	if p.at(pos) == '-' && p.indicator(pos) {
		return p.sequence(pos, indent)
	}
	key, end, err := p.inline(pos, false)
	if err != nil {
		return nil, 0, err
	}
	if p.at(end) == ':' && p.indicator(end) {
		return p.mapping(pos, indent, key, end)
	}
	if err := p.lineDone(end); err != nil {
		return nil, 0, err
	}
	next, err := p.nextContent(p.lineEnd(end))
	if err != nil {
		return nil, 0, err
	}
	if next < len(p.text) && p.column(next) > parent && !p.marker(next, "...") {
		return nil, 0, p.failAt(next, "a scalar continues onto this line; multi-line plain scalars are not supported, so quote it on one line")
	}
	return key, p.lineEnd(end), nil
}

func (p *parser) lineDone(pos int) error {
	pos = p.skipBlanks(pos)
	if breakOrEnd(p.at(pos)) || p.at(pos) == '#' && scan.Blank(p.at(pos-1)) {
		return nil
	}
	return p.failAt(pos, "unexpected %q after the value", p.at(pos))
}

func (p *parser) sequence(pos, indent int) (*Node, int, error) {
	line, column := p.position(pos)
	seq := &Node{Kind: Sequence, Line: line, Column: column, Items: []*Node{}}
	for {
		item, end, err := p.value(pos+1, indent, true)
		if err != nil {
			return nil, 0, err
		}
		seq.Items = append(seq.Items, item)
		next, err := p.nextContent(end)
		if err != nil {
			return nil, 0, err
		}
		switch {
		case next >= len(p.text) || p.column(next) < indent || p.marker(next, "...") || p.marker(next, "---"):
			return seq, next, nil
		case p.column(next) > indent:
			return nil, 0, p.failAt(next, "this line is indented deeper than the sequence item before it")
		case p.at(next) != '-' || !p.indicator(next):
			return seq, next, nil
		}
		pos = next
	}
}

func (p *parser) mapping(pos, indent int, key *Node, colon int) (*Node, int, error) {
	line, column := p.position(pos)
	m := &Node{Kind: Mapping, Line: line, Column: column, Pairs: []Pair{}}
	for {
		if key.Kind != Scalar {
			return nil, 0, p.failAt(pos, "a mapping key must be a scalar")
		}
		if _, dup := m.Get(key.Value); dup {
			return nil, 0, p.failAt(pos, "the key %q appears twice in this mapping", key.Value)
		}
		value, end, err := p.value(colon+1, indent, false)
		if err != nil {
			return nil, 0, err
		}
		m.Pairs = append(m.Pairs, Pair{Key: key, Value: value})
		next, err := p.nextContent(end)
		if err != nil {
			return nil, 0, err
		}
		switch {
		case next >= len(p.text) || p.column(next) < indent || p.marker(next, "...") || p.marker(next, "---"):
			return m, next, nil
		case p.column(next) > indent:
			return nil, 0, p.failAt(next, "this line is indented deeper than the mapping key before it")
		case p.at(next) == '-' && p.indicator(next):
			return nil, 0, p.failAt(next, "a sequence item appears where the mapping expects a key")
		}
		pos = next
		if key, colon, err = p.inline(pos, false); err != nil {
			return nil, 0, err
		}
		if p.at(colon) != ':' || !p.indicator(colon) {
			return nil, 0, p.failAt(colon, "expected `:` after the mapping key")
		}
	}
}

func (p *parser) value(pos, indent int, item bool) (*Node, int, error) {
	start := p.skipBlanks(pos)
	if p.restIsEmpty(start) {
		next, err := p.nextContent(p.lineEnd(start))
		if err != nil {
			return nil, 0, err
		}
		switch {
		case next < len(p.text) && p.column(next) > indent && !p.marker(next, "..."):
			return p.block(next, indent)
		case !item && next < len(p.text) && p.column(next) == indent && p.at(next) == '-' && p.indicator(next):
			return p.sequence(next, indent)
		}
		line, column := p.position(start)
		return &Node{Kind: Scalar, Line: line, Column: column}, p.lineEnd(start), nil
	}
	if !item && p.at(start) == '-' && p.indicator(start) {
		return nil, 0, p.failAt(start, "a block sequence cannot start on the line of its key; start it on the next line")
	}
	if item {
		return p.block(start, indent)
	}
	node, end, err := p.inline(start, false)
	if err != nil {
		return nil, 0, err
	}
	if p.at(end) == ':' && p.indicator(end) {
		return nil, 0, p.failAt(end, "a mapping cannot start on the line of its key; start it on the next line")
	}
	if err := p.lineDone(end); err != nil {
		return nil, 0, err
	}
	next, err := p.nextContent(p.lineEnd(end))
	if err != nil {
		return nil, 0, err
	}
	if next < len(p.text) && p.column(next) > indent && !p.marker(next, "...") {
		return nil, 0, p.failAt(next, "a scalar continues onto this line; multi-line plain scalars are not supported, so quote it on one line")
	}
	return node, p.lineEnd(end), nil
}

func (p *parser) unsupported(pos int) error {
	switch p.at(pos) {
	case '&':
		return p.failAt(pos, "anchors are not supported")
	case '*':
		return p.failAt(pos, "aliases are not supported")
	case '!':
		return p.failAt(pos, "tags are not supported")
	case '|', '>':
		return p.failAt(pos, "block scalars are not supported; quote the value on one line")
	case '?':
		if p.indicator(pos) {
			return p.failAt(pos, "complex mapping keys are not supported")
		}
	case '@', '`':
		return p.failAt(pos, "a plain scalar cannot start with %q; quote it", p.at(pos))
	}
	return nil
}

func (p *parser) inline(pos int, flow bool) (*Node, int, error) {
	if err := p.unsupported(pos); err != nil {
		return nil, 0, err
	}
	line, column := p.position(pos)
	switch p.at(pos) {
	case '[':
		return p.flowSequence(pos)
	case '{':
		return p.flowMapping(pos)
	case '\'':
		value, end, err := p.singleQuoted(pos)
		return &Node{Kind: Scalar, Value: value, Quoted: true, Line: line, Column: column}, end, err
	case '"':
		value, end, err := p.doubleQuoted(pos)
		return &Node{Kind: Scalar, Value: value, Quoted: true, Line: line, Column: column}, end, err
	case ']', '}', ',':
		if !flow {
			return nil, 0, p.failAt(pos, "a plain scalar cannot start with %q; quote it", p.at(pos))
		}
		return &Node{Kind: Scalar, Line: line, Column: column}, pos, nil
	}
	end := p.plainEnd(pos, flow)
	return &Node{Kind: Scalar, Value: strings.TrimRight(p.text[pos:end], " \t"), Line: line, Column: column}, end, nil
}

func flowIndicator(b byte) bool { return b == ',' || b == '[' || b == ']' || b == '{' || b == '}' }

func (p *parser) plainEnd(pos int, flow bool) int {
	end := pos
	for !breakOrEnd(p.at(end)) {
		b := p.at(end)
		switch {
		case b == ':' && (scan.Blank(p.at(end+1)) || breakOrEnd(p.at(end+1)) || flow && flowIndicator(p.at(end+1))):
			return end
		case b == '#' && end > pos && scan.Blank(p.at(end-1)):
			return end
		case flow && flowIndicator(b):
			return end
		}
		end++
	}
	return end
}

func (p *parser) singleQuoted(pos int) (string, int, error) {
	var sb strings.Builder
	for i := pos + 1; i < len(p.text); i++ {
		switch p.text[i] {
		case '\'':
			if p.at(i+1) == '\'' {
				sb.WriteByte('\'')
				i++
				continue
			}
			return sb.String(), i + 1, nil
		case '\n':
			return "", 0, p.failAt(pos, "a quoted scalar continues onto the next line; multi-line scalars are not supported")
		}
		sb.WriteByte(p.text[i])
	}
	return "", 0, p.failAt(pos, "the single-quoted scalar is never closed")
}

var escapes = map[byte]string{
	'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n", 'v': "\v", 'f': "\f", 'r': "\r", 'e': "\x1b",
	' ': " ", '"': "\"", '/': "/", '\\': "\\", 'N': "\u0085", '_': " ", 'L': " ", 'P': " ",
}

var hexEscapes = map[byte]int{'x': 2, 'u': 4, 'U': 8}

func (p *parser) doubleQuoted(pos int) (string, int, error) {
	var sb strings.Builder
	for i := pos + 1; i < len(p.text); i++ {
		switch b := p.text[i]; b {
		case '"':
			return sb.String(), i + 1, nil
		case '\n':
			return "", 0, p.failAt(pos, "a quoted scalar continues onto the next line; multi-line scalars are not supported")
		case '\\':
			e := p.at(i + 1)
			if s, ok := escapes[e]; ok {
				sb.WriteString(s)
				i++
				continue
			}
			digits, ok := hexEscapes[e]
			if !ok || i+2+digits > len(p.text) {
				return "", 0, p.failAt(i, "unknown escape \\%c", e)
			}
			r, err := strconv.ParseUint(p.text[i+2:i+2+digits], 16, 21)
			if err != nil {
				return "", 0, p.failAt(i, "the escape \\%c needs %d hexadecimal digits: %v", e, digits, err)
			}
			if !utf8.ValidRune(rune(r)) {
				return "", 0, p.failAt(i, "the escape \\%c%s is no valid character", e, p.text[i+2:i+2+digits])
			}
			sb.WriteRune(rune(r))
			i += 1 + digits
		default:
			sb.WriteByte(b)
		}
	}
	return "", 0, p.failAt(pos, "the double-quoted scalar is never closed")
}

func (p *parser) skipFlowSpace(pos int) (int, error) {
	for pos < len(p.text) {
		switch b := p.at(pos); {
		case scan.Blank(b) || b == '\n' || b == '\r':
			pos++
		case b == '#' && (pos == 0 || scan.Blank(p.at(pos-1)) || p.at(pos-1) == '\n'):
			pos = p.lineEnd(pos)
		default:
			return pos, nil
		}
	}
	return pos, p.failAt(pos, "the flow collection is never closed")
}

func (p *parser) flowSequence(pos int) (*Node, int, error) {
	line, column := p.position(pos)
	seq := &Node{Kind: Sequence, Line: line, Column: column, Items: []*Node{}}
	pos++
	for {
		var err error
		if pos, err = p.skipFlowSpace(pos); err != nil {
			return nil, 0, err
		}
		if p.at(pos) == ']' {
			return seq, pos + 1, nil
		}
		item, end, err := p.flowEntry(pos)
		if err != nil {
			return nil, 0, err
		}
		seq.Items = append(seq.Items, item)
		if pos, err = p.flowSeparator(end, ']'); err != nil {
			return nil, 0, err
		}
	}
}

func (p *parser) flowMapping(pos int) (*Node, int, error) {
	line, column := p.position(pos)
	m := &Node{Kind: Mapping, Line: line, Column: column, Pairs: []Pair{}}
	pos++
	for {
		var err error
		if pos, err = p.skipFlowSpace(pos); err != nil {
			return nil, 0, err
		}
		if p.at(pos) == '}' {
			return m, pos + 1, nil
		}
		key, end, err := p.inline(pos, true)
		if err != nil {
			return nil, 0, err
		}
		if key.Kind != Scalar {
			return nil, 0, p.failAt(pos, "a mapping key must be a scalar")
		}
		if _, dup := m.Get(key.Value); dup {
			return nil, 0, p.failAt(pos, "the key %q appears twice in this mapping", key.Value)
		}
		if end, err = p.skipFlowSpace(end); err != nil {
			return nil, 0, err
		}
		kline, kcolumn := p.position(end)
		value := &Node{Kind: Scalar, Line: kline, Column: kcolumn}
		if p.at(end) == ':' {
			if end, err = p.skipFlowSpace(end + 1); err != nil {
				return nil, 0, err
			}
			if p.at(end) != ',' && p.at(end) != '}' {
				if value, end, err = p.inline(end, true); err != nil {
					return nil, 0, err
				}
			}
		}
		m.Pairs = append(m.Pairs, Pair{Key: key, Value: value})
		if pos, err = p.flowSeparator(end, '}'); err != nil {
			return nil, 0, err
		}
	}
}

func (p *parser) flowEntry(pos int) (*Node, int, error) {
	node, end, err := p.inline(pos, true)
	if err != nil {
		return nil, 0, err
	}
	after, err := p.skipFlowSpace(end)
	if err != nil {
		return nil, 0, err
	}
	if p.at(after) != ':' {
		return node, end, nil
	}
	value, end, err := p.inline(p.skipBlanks(after+1), true)
	if err != nil {
		return nil, 0, err
	}
	return &Node{Kind: Mapping, Line: node.Line, Column: node.Column, Pairs: []Pair{{Key: node, Value: value}}}, end, nil
}

func (p *parser) flowSeparator(pos int, closer byte) (int, error) {
	pos, err := p.skipFlowSpace(pos)
	if err != nil {
		return 0, err
	}
	switch p.at(pos) {
	case ',':
		return pos + 1, nil
	case closer:
		return pos, nil
	}
	return 0, p.failAt(pos, "expected `,` or `%c` in the flow collection", closer)
}
