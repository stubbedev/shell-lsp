package justfile

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type Span = parser.Span

type Name struct {
	Text string
	Span Span
}

type Path []Name

func (p Path) String() string {
	names := make([]string, len(p))
	for i, n := range p {
		names[i] = n.Text
	}
	return strings.Join(names, "::")
}

func (p Path) Span() Span {
	if len(p) == 0 {
		return Span{}
	}
	return Span{Start: p[0].Span.Start, End: p[len(p)-1].Span.End}
}

type ExprKind uint8

const (
	ExprString ExprKind = iota
	ExprBacktick
	ExprVariable
	ExprCall
	ExprBinary
	ExprNot
	ExprRoot
	ExprIf
	ExprList
	ExprGroup
	ExprBad
)

type Expr struct {
	Kind    ExprKind
	Span    Span
	Name    Name
	Op      string
	Args    []*Expr
	Str     *StringLit
	Else    Span
	OpSpan  Span
	Starred bool
}

type StringLit struct {
	Prefix   byte
	Quote    string
	Value    string
	Inner    Span
	Segments []Segment
	Unclosed bool
}

type Segment struct {
	Kind   FragmentKind
	Span   Span
	Interp *Expr
}

func (s *StringLit) Interps() []*Expr {
	var out []*Expr
	for _, seg := range s.Segments {
		if seg.Kind == FragmentInterpolation {
			out = append(out, seg.Interp)
		}
	}
	return out
}

func (s *StringLit) Indented() bool { return len(s.Quote) == 3 }

func (s *StringLit) Raw() bool { return s.Quote[0] == '\'' }

type Attribute struct {
	Name    Name
	Span    Span
	Args    []AttributeArg
	Partial bool
}

type AttributeArg struct {
	Key   Name
	Value *Expr
	Span  Span
}

func (a Attribute) keyed(key string) *Expr {
	for _, arg := range a.Args {
		if arg.Key.Text == key {
			return arg.Value
		}
	}
	return nil
}

func (a Attribute) first() string {
	for _, arg := range a.Args {
		if arg.Key.Text == "" {
			return literal(arg.Value)
		}
	}
	return ""
}

func (a Attribute) long() (string, bool) {
	for _, arg := range a.Args {
		if arg.Key.Text != "long" {
			continue
		}
		if arg.Value == nil {
			return a.first(), true
		}
		return literal(arg.Value), true
	}
	return "", false
}

func literal(e *Expr) string {
	if e == nil || e.Kind != ExprString {
		return ""
	}
	return e.Str.Value
}

type Comment struct {
	Span Span
	Text string
}

type Setting struct {
	Attributes []Attribute
	Name       Name
	Value      *Expr
	Span       Span
}

type Assignment struct {
	Attributes []Attribute
	Doc        string
	Name       Name
	Export     bool
	Eager      bool
	Keyword    Span
	Value      *Expr
	Span       Span
}

type Unexport struct {
	Name Name
	Span Span
}

type Alias struct {
	Attributes []Attribute
	Name       Name
	Target     Path
	Span       Span
}

type Import struct {
	Attributes []Attribute
	Optional   bool
	Path       *Expr
	Span       Span
}

type Module struct {
	Attributes []Attribute
	Doc        string
	Optional   bool
	Name       Name
	Path       *Expr
	Span       Span
}

type Function struct {
	Attributes []Attribute
	Name       Name
	Params     []Name
	Body       *Expr
	Span       Span
}

type Variadic uint8

const (
	Singular Variadic = iota
	Plus
	Star
)

type Param struct {
	Name     Name
	Export   bool
	Variadic Variadic
	Default  *Expr
	Span     Span
}

type Dependency struct {
	Target Path
	Args   []*Expr
	Paren  bool
	Star   bool
	Span   Span
}

type FragmentKind uint8

const (
	FragmentText FragmentKind = iota
	FragmentEscape
	FragmentInterpolation
)

type Fragment struct {
	Kind   FragmentKind
	Span   Span
	Interp *Expr
	Closed bool
}

type Line struct {
	Span      Span
	Indent    Span
	Sigils    Span
	Fragments []Fragment
	Continued bool
}

type Recipe struct {
	Attributes []Attribute
	Doc        string
	Quiet      bool
	Name       Name
	Params     []Param
	Deps       []Dependency
	Subsequent []Dependency
	Header     Span
	Body       []Line
	Indent     string
	Shebang    bool
	Span       Span
}

type ItemKind uint8

const (
	ItemSetting ItemKind = iota
	ItemAssignment
	ItemUnexport
	ItemAlias
	ItemImport
	ItemModule
	ItemFunction
	ItemRecipe
)

type Item struct {
	Kind       ItemKind
	Setting    *Setting
	Assignment *Assignment
	Unexport   *Unexport
	Alias      *Alias
	Import     *Import
	Module     *Module
	Function   *Function
	Recipe     *Recipe
}

func (it Item) Span() Span {
	switch it.Kind {
	case ItemSetting:
		return it.Setting.Span
	case ItemAssignment:
		return it.Assignment.Span
	case ItemUnexport:
		return it.Unexport.Span
	case ItemAlias:
		return it.Alias.Span
	case ItemImport:
		return it.Import.Span
	case ItemModule:
		return it.Module.Span
	case ItemFunction:
		return it.Function.Span
	case ItemRecipe:
		return it.Recipe.Span
	}
	panic("justfile: unknown item kind")
}

type Error struct {
	Code    lint.Code
	Span    Span
	Message string
}

type File struct {
	Text     string
	Items    []Item
	Stray    []Attribute
	Comments []Comment
	Errors   []Error
}

func (f *File) Recipes() []*Recipe {
	var out []*Recipe
	for _, it := range f.Items {
		if it.Kind == ItemRecipe {
			out = append(out, it.Recipe)
		}
	}
	return out
}

func (f *File) Settings() []*Setting {
	var out []*Setting
	for _, it := range f.Items {
		if it.Kind == ItemSetting {
			out = append(out, it.Setting)
		}
	}
	return out
}

func (f *File) Assignments() []*Assignment {
	var out []*Assignment
	for _, it := range f.Items {
		if it.Kind == ItemAssignment {
			out = append(out, it.Assignment)
		}
	}
	return out
}

func Walk(e *Expr, visit func(*Expr)) {
	if e == nil {
		return
	}
	visit(e)
	for _, a := range e.Args {
		Walk(a, visit)
	}
	if e.Str != nil {
		for _, i := range e.Str.Interps() {
			Walk(i, visit)
		}
	}
}
