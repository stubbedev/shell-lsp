package makefile

import "strings"

type NodeKind uint8

const (
	NodeText NodeKind = iota
	NodeReference
	NodeCall
	NodeSubstitution
)

type Node struct {
	Kind       NodeKind
	Span       Span
	Name       Expr
	Args       []Expr
	Braced     bool
	Unbalanced bool
}

type Expr struct {
	Span  Span
	Nodes []Node
}

func (e Expr) Literal(text string) (string, bool) {
	var sb strings.Builder
	for _, n := range e.Nodes {
		if n.Kind != NodeText {
			return "", false
		}
		sb.WriteString(text[n.Span.Start:n.Span.End])
	}
	return sb.String(), true
}

func (e Expr) Dynamic() bool {
	for _, n := range e.Nodes {
		if n.Kind != NodeText {
			return true
		}
	}
	return false
}

func Walk(e Expr, visit func(Node) bool) {
	for _, n := range e.Nodes {
		if !visit(n) {
			continue
		}
		Walk(n.Name, visit)
		for _, a := range n.Args {
			Walk(a, visit)
		}
	}
}

type exprParser struct {
	text   string
	errors *[]Error
}

func (p exprParser) parse(start, end int) Expr {
	e := Expr{Span: Span{Start: start, End: end}}
	text := start
	flush := func(at int) {
		if at > text {
			e.Nodes = append(e.Nodes, Node{Kind: NodeText, Span: Span{Start: text, End: at}})
		}
	}
	for i := start; i < end; i++ {
		if p.text[i] != '$' {
			continue
		}
		if i+1 >= end {
			break
		}
		switch next := p.text[i+1]; next {
		case '$':
			i++
		case '(', '{':
			flush(i)
			n, after := p.reference(i, end)
			e.Nodes = append(e.Nodes, n)
			text = after
			i = after - 1
		default:
			flush(i)
			n := Node{Kind: NodeReference, Span: Span{Start: i, End: i + 2}}
			n.Name = Expr{Span: Span{Start: i + 1, End: i + 2}, Nodes: []Node{{Kind: NodeText, Span: Span{Start: i + 1, End: i + 2}}}}
			e.Nodes = append(e.Nodes, n)
			text = i + 2
			i++
		}
	}
	flush(end)
	return e
}

func (p exprParser) closing(open, end int) int { return closingOf(p.text, open, end) }

func closingOf(text string, open, end int) int {
	closing, _ := closingWith(text, open, end)
	return closing
}

func closingWith(text string, open, end int) (int, bool) {
	openByte := text[open]
	closeByte := byte(')')
	if openByte == '{' {
		closeByte = '}'
	}
	if _, ok := functionName(text, open+1, end); ok {
		return balanced(text, open, end, func(j int) bool { return text[j] == openByte }, closeByte), false
	}
	nested := func(j int) bool { return j > 0 && text[j] == openByte && text[j-1] == '$' }
	if closing := balanced(text, open, end, nested, closeByte); closing >= 0 {
		return closing, false
	}
	if rest := text[open+1 : end]; strings.Contains(rest, "$"+string(openByte)) && strings.IndexByte(rest, closeByte) >= 0 {
		return end, true
	}
	return -1, false
}

func balanced(text string, open, end int, opens func(int) bool, closeByte byte) int {
	depth := 1
	for j := open + 1; j < end; j++ {
		switch {
		case opens(j):
			depth++
		case text[j] == closeByte:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

func functionName(text string, start, end int) (int, bool) {
	nameEnd := start
	for nameEnd < end && (text[nameEnd] >= 'a' && text[nameEnd] <= 'z' || text[nameEnd] == '-') {
		nameEnd++
	}
	if nameEnd == start || nameEnd >= end || text[nameEnd] != ' ' && text[nameEnd] != '\t' || !builtinFunction(text[start:nameEnd]) {
		return 0, false
	}
	return nameEnd, true
}

func referenceEnd(text string, dollar, end int) int {
	if dollar+1 >= end {
		return end
	}
	if next := text[dollar+1]; next != '(' && next != '{' {
		return dollar + 2
	}
	if closing, unbalanced := closingWith(text, dollar+1, end); closing >= 0 && !unbalanced {
		return closing + 1
	}
	return end
}

func (p exprParser) reference(dollar, end int) (Node, int) {
	closing, unbalanced := closingWith(p.text, dollar+1, end)
	inner := dollar + 2
	n := Node{Kind: NodeReference, Braced: true}
	if closing < 0 {
		*p.errors = append(*p.errors, Error{Span: Span{Start: dollar, End: min(dollar+2, end)}, Message: "this variable reference is not closed on the same logical line", Kind: ErrUnterminated})
		n.Span = Span{Start: dollar, End: end}
		n.Name = p.parse(inner, end)
		return n, end
	}
	if unbalanced {
		n.Span, n.Unbalanced = Span{Start: dollar, End: end}, true
		n.Name = Expr{Span: Span{Start: inner, End: end}, Nodes: []Node{{Kind: NodeText, Span: Span{Start: inner, End: end}}}}
		return n, end
	}
	n.Span = Span{Start: dollar, End: closing + 1}
	if name, args, ok := p.call(inner, closing); ok {
		n.Kind, n.Name, n.Args = NodeCall, name, args
		return n, closing + 1
	}
	if colon := p.topLevel(inner, closing, ':'); colon >= 0 {
		if equals := p.topLevel(colon+1, closing, '='); equals >= 0 {
			n.Kind = NodeSubstitution
			n.Name = p.parse(inner, colon)
			n.Args = []Expr{p.parse(colon+1, equals), p.parse(equals+1, closing)}
			return n, closing + 1
		}
	}
	n.Name = p.parse(inner, closing)
	return n, closing + 1
}

func (p exprParser) call(start, end int) (Expr, []Expr, bool) {
	nameEnd, ok := functionName(p.text, start, end)
	if !ok {
		return Expr{}, nil, false
	}
	name := Expr{Span: Span{Start: start, End: nameEnd}, Nodes: []Node{{Kind: NodeText, Span: Span{Start: start, End: nameEnd}}}}
	from := nameEnd
	for from < end && (p.text[from] == ' ' || p.text[from] == '\t') {
		from++
	}
	var args []Expr
	for {
		comma := p.topLevel(from, end, ',')
		if comma < 0 {
			return name, append(args, p.parse(from, end)), true
		}
		args = append(args, p.parse(from, comma))
		from = comma + 1
	}
}

func (p exprParser) topLevel(start, end int, stop byte) int {
	depth := 0
	for i := start; i < end; i++ {
		switch c := p.text[i]; {
		case c == '$' && i+1 < end && (p.text[i+1] == '(' || p.text[i+1] == '{'):
			if closing := p.closing(i+1, end); closing >= 0 {
				i = closing
			}
		case c == '(' || c == '{':
			depth++
		case c == ')' || c == '}':
			depth--
		case c == stop && depth == 0:
			return i
		}
	}
	return -1
}
