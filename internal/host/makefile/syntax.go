package makefile

import "github.com/stubbedev/shell-lsp/internal/parser"

type Span = parser.Span

type Word struct {
	Text string
	Span Span
	Expr Expr
}

type Assignment struct {
	Name      Word
	Op        string
	Keyword   Span
	Value     Expr
	Modifiers []Word
	Targets   []Word
	Span      Span
	Define    bool
	End       Span
}

type RecipeLine struct {
	Span   Span
	Inline bool
	Expr   Expr
}

type Rule struct {
	Targets     []Word
	Prereqs     []Word
	OrderOnly   []Word
	Patterns    []Word
	DoubleColon bool
	Colon       int
	Header      Span
	Recipe      []RecipeLine
	Span        Span
}

type IncludeKind uint8

const (
	IncludeRequired IncludeKind = iota
	IncludeOptional
)

type Include struct {
	Keyword Word
	Files   []Word
	Kind    IncludeKind
	Span    Span
}

type Directive struct {
	Keyword Word
	Names   []Word
	Args    Expr
	Span    Span
}

type Conditional struct {
	Open   Directive
	Else   []Directive
	End    *Directive
	Span   Span
	Broken bool
}

type Error struct {
	Span    Span
	Message string
	Kind    ErrorKind
}

type ErrorKind uint8

const (
	ErrSyntax ErrorKind = iota
	ErrConditional
	ErrDefine
	ErrRecipePrefix
	ErrRecipeOutsideRule
	ErrUnterminated
)

type File struct {
	Text         string
	Assignments  []*Assignment
	Rules        []*Rule
	Includes     []*Include
	Directives   []*Directive
	Conditionals []*Conditional
	Expressions  []Expr
	Comments     []Span
	Errors       []Error
	ExportAll    bool
}
