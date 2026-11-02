package syntax

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

type (
	Span  = parser.Span
	Error = parser.Error
)

type File struct {
	Text     string
	Root     Expr
	Stray    []Expr
	Comments []Comment
	Tokens   []Token
	Errors   []Error
	BareOr   int
	Slots    int
	Reads    int
	Binders  int
}

type Token struct {
	Kind TokenKind
	Span Span
}

func (f *File) TokensIn(span Span) []Token {
	first, _ := slices.BinarySearchFunc(f.Tokens, span.Start, func(t Token, at int) int { return t.Span.End - at - 1 })
	last, _ := slices.BinarySearchFunc(f.Tokens, span.End, func(t Token, at int) int { return t.Span.Start - at })
	return f.Tokens[first:max(first, last)]
}

type CommentKind uint8

const (
	LineComment CommentKind = iota
	BlockComment
)

type Comment struct {
	Span Span
	Body Span
	Kind CommentKind
}

func (f *File) CommentBefore(offset int) (Comment, bool) {
	after, _ := slices.BinarySearchFunc(f.Comments, offset, func(c Comment, at int) int { return c.Span.End - at - 1 })
	if after == 0 {
		return Comment{}, false
	}
	c := f.Comments[after-1]
	if strings.TrimLeft(f.Text[c.Span.End:offset], " \t\r\n") != "" {
		return Comment{}, false
	}
	return c, true
}

type Node interface {
	Span() Span
}

type node struct{ span Span }

func (n node) Span() Span { return n.span }

type Expr interface {
	Node
	expr()
}

type exprNode struct{ node }

func (exprNode) expr() {}

func exprAt(s Span) exprNode { return exprNode{node{s}} }

type Part interface {
	Node
	part()
}

type partNode struct{ node }

func (partNode) part() {}

func partAt(s Span) partNode { return partNode{node{s}} }

type Attr interface {
	Node
	attr()
	Slot() int
}

type attrNode struct {
	node
	slot int32
}

func (attrNode) attr() {}

func (a attrNode) Slot() int { return int(a.slot) }

type Binding interface {
	Node
	binding()
}

type bindingNode struct{ node }

func (bindingNode) binding() {}

func bindingAt(s Span) bindingNode { return bindingNode{node{s}} }

type Param interface {
	Node
	param()
}

type paramNode struct{ node }

func (paramNode) param() {}

func paramAt(s Span) paramNode { return paramNode{node{s}} }

type Bad struct{ exprNode }

type Ident struct {
	exprNode
	Name string
	slot int32
}

func (n *Ident) Slot() int { return int(n.slot) }

type Int struct {
	exprNode
	Text string
}

type Float struct {
	exprNode
	Text string
}

type URI struct {
	exprNode
	Text string
}

type StringKind uint8

const (
	Quoted StringKind = iota
	Indented
)

type String struct {
	exprNode
	Kind    StringKind
	Content Span
	Parts   []Part
	Closed  bool
}

type Literal struct {
	partNode
	Text string
}

type Escape struct {
	partNode
	Text string
}

type Interpolation struct {
	partNode
	Value  Expr
	Closed bool
}

type PathKind uint8

const (
	RelativePath PathKind = iota
	AbsolutePath
	HomePath
	SearchPath
)

type Path struct {
	exprNode
	Kind  PathKind
	Parts []Part
}

type Paren struct {
	exprNode
	Inner  Expr
	Closed bool
}

type List struct {
	exprNode
	Items  []Expr
	Closed bool
}

type AttrSet struct {
	exprNode
	Rec      bool
	Bindings []Binding
	Closed   bool
}

type Let struct {
	exprNode
	Bindings []Binding
	Body     Expr
}

type LegacyLet struct {
	exprNode
	Set *AttrSet
}

type With struct {
	exprNode
	Scope Expr
	Body  Expr
}

type Assert struct {
	exprNode
	Cond Expr
	Body Expr
}

type If struct {
	exprNode
	Cond Expr
	Then Expr
	Else Expr
}

type Lambda struct {
	exprNode
	Param Param
	Body  Expr
}

type Apply struct {
	exprNode
	Fn   Expr
	Args []Expr
}

type Select struct {
	exprNode
	Target  Expr
	Path    AttrPath
	Default Expr
}

type HasAttr struct {
	exprNode
	Target Expr
	Path   AttrPath
}

type Op uint8

const (
	OpPipeInto Op = iota
	OpPipeFrom
	OpImplies
	OpOr
	OpAnd
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpUpdate
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpConcat
)

var opNames = [...]string{
	OpPipeInto: "|>",
	OpPipeFrom: "<|",
	OpImplies:  "->",
	OpOr:       "||",
	OpAnd:      "&&",
	OpEq:       "==",
	OpNe:       "!=",
	OpLt:       "<",
	OpLe:       "<=",
	OpGt:       ">",
	OpGe:       ">=",
	OpUpdate:   "//",
	OpAdd:      "+",
	OpSub:      "-",
	OpMul:      "*",
	OpDiv:      "/",
	OpConcat:   "++",
}

func (o Op) String() string { return opNames[o] }

type Binary struct {
	exprNode
	Op     Op
	OpSpan Span
	Left   Expr
	Right  Expr
}

type UnaryOp uint8

const (
	OpNot UnaryOp = iota
	OpNegate
)

type Unary struct {
	exprNode
	Op      UnaryOp
	Operand Expr
}

type AttrPath []Attr

type IdentAttr struct {
	attrNode
	Name string
}

type StringAttr struct {
	attrNode
	Value *String
}

type DynamicAttr struct {
	attrNode
	Value  Expr
	Closed bool
}

type BadAttr struct{ attrNode }

func StaticName(a Attr) (string, Span, bool) {
	switch a := a.(type) {
	case *IdentAttr:
		return a.Name, a.Span(), true
	case *StringAttr:
		switch parts := a.Value.Parts; {
		case len(parts) == 0:
			return "", a.Value.Content, true
		case len(parts) == 1:
			if l, ok := parts[0].(*Literal); ok {
				return l.Text, l.Span(), true
			}
		}
	}
	return "", Span{}, false
}

func StaticAttr(a Attr) bool {
	switch a := a.(type) {
	case *StringAttr:
		return !interpolated(a.Value)
	case *DynamicAttr:
		s, ok := Unparenthesized(a.Value).(*String)
		return ok && !interpolated(s)
	}
	return true
}

func AttrName(a Attr) (string, bool) {
	switch a := a.(type) {
	case *IdentAttr:
		return a.Name, true
	case *StringAttr:
		return StringValue(a.Value)
	case *DynamicAttr:
		if s, ok := Unparenthesized(a.Value).(*String); ok {
			return StringValue(s)
		}
	}
	return "", false
}

func Unparenthesized(e Expr) Expr {
	for {
		p, ok := e.(*Paren)
		if !ok {
			return e
		}
		e = p.Inner
	}
}

func interpolated(s *String) bool {
	return slices.ContainsFunc(s.Parts, func(p Part) bool {
		_, ok := p.(*Interpolation)
		return ok
	})
}

func StringValue(s *String) (string, bool) {
	var value strings.Builder
	for _, part := range s.Parts {
		switch part := part.(type) {
		case *Literal:
			value.WriteString(part.Text)
		case *Escape:
			value.WriteString(part.Text)
		default:
			return "", false
		}
	}
	if s.Kind == Quoted {
		return value.String(), true
	}
	text := value.String()
	if strings.Contains(text, "\n") {
		return "", false
	}
	return strings.TrimLeft(text, " "), true
}

type Assign struct {
	bindingNode
	Path  AttrPath
	Value Expr
}

type Inherit struct {
	bindingNode
	From  *Paren
	Attrs []Attr
}

type IdentParam struct {
	paramNode
	Name *Ident
}

type PatternParam struct {
	paramNode
	Formals    []*Formal
	Ellipsis   bool
	Alias      *Ident
	AliasFirst bool
	Closed     bool
}

type Formal struct {
	node
	Name    *Ident
	Default Expr
}
