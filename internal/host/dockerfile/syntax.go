package dockerfile

import (
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type Span = parser.Span

type Word struct {
	Text string
	Span Span
}

type Directive struct {
	Name  Word
	Value Word
	Span  Span
}

type Form uint8

const (
	FormNone Form = iota
	FormShell
	FormJSON
)

type Flag struct {
	Name  Word
	Value *Word
	Span  Span
}

type Heredoc struct {
	Name   Word
	Body   Span
	Expand bool
	Chomp  bool
	Span   Span
}

type Segment struct {
	Span    Span
	Comment bool
}

type Instruction struct {
	Keyword  Word
	Name     string
	Flags    []Flag
	Args     []Word
	Form     Form
	Command  Span
	Pieces   []Segment
	JSON     []Word
	Heredocs []Heredoc
	Inner    *Instruction
	Span     Span
	Line     Span
	rest     logical
}

type Error struct {
	Span    Span
	Message string
	Kind    ErrorKind
}

type ErrorKind uint8

const (
	ErrSyntax ErrorKind = iota
	ErrUnknownInstruction
	ErrUnknownFlag
	ErrDirective
)

type File struct {
	Text         string
	Escape       byte
	Directives   []Directive
	Instructions []*Instruction
	Comments     []Span
	Empty        []Span
	Errors       []Error
}
