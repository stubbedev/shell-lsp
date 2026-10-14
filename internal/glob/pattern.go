package glob

import (
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
)

type Syntax struct {
	Dialect   dialect.Dialect
	Filenames bool
	KshGlob   bool
	Regex     RegexFlavor
}

type RegexFlavor uint8

const (
	NotRegex RegexFlavor = iota
	ExtendedRegex
	BasicRegex
)

type Option uint8

const (
	ExtendedGlob Option = iota + 1
	ExtGlob
	GlobStar
)

func (o Option) String() string {
	switch o {
	case ExtendedGlob:
		return "extendedglob"
	case ExtGlob:
		return "extglob"
	case GlobStar:
		return "globstar"
	}
	panic("glob: unknown option")
}

func (o Option) On(opts shelloptions.Options) bool {
	switch o {
	case ExtendedGlob:
		return opts.ExtendedGlob
	case ExtGlob:
		return opts.ExtGlob
	case GlobStar:
		return opts.GlobStar
	}
	panic("glob: unknown option")
}

func (o Option) Enable(d dialect.Dialect) string {
	if d == dialect.Zsh {
		return "setopt " + o.String()
	}
	return "shopt -s " + o.String()
}

func (p Pattern) Unmet(opts shelloptions.Options) []Need {
	var unmet []Need
	for _, n := range p.Needs {
		if !n.Option.On(opts) {
			unmet = append(unmet, n)
		}
	}
	return unmet
}

type Need struct {
	Span   parser.Span
	Option Option
}

type Error struct {
	Span    parser.Span
	Message string
}

type Pattern struct {
	Root        Node
	Syntax      Syntax
	Captures    int
	SetsMatch   bool
	Unevaluable string
	Lazy        bool
	Needs       []Need
	Errors      []Error
}

type Node interface {
	Span() parser.Span
	node()
}

type Case uint8

const (
	CaseSensitive Case = iota
	CaseInsensitive
	CaseLowerFolds
)

type Literal struct {
	At   parser.Span
	Text string
	Case Case
}

type AnyChar struct{ At parser.Span }

type AnyString struct{ At parser.Span }

type Recursive struct {
	At     parser.Span
	Follow bool
}

type Bracket struct {
	At      parser.Span
	Negated bool
	Items   []BracketItem
	Case    Case
}

type BracketItem struct {
	Lo, Hi rune
	Class  string
}

type GroupOp uint8

const (
	GroupPlain GroupOp = iota
	GroupZeroOrOne
	GroupZeroOrMore
	GroupOneOrMore
	GroupOne
	GroupNot
	GroupCounted
)

type Group struct {
	At       parser.Span
	Op       GroupOp
	Min, Max int
	X        Node
	Capture  int
	Lazy     bool
}

type Alternatives struct {
	At      parser.Span
	Choices []Node
}

type Seq struct {
	At    parser.Span
	Items []Node
}

type Repeat struct {
	At       parser.Span
	X        Node
	Min, Max int
}

type Not struct {
	At parser.Span
	X  Node
}

type Exclude struct {
	At     parser.Span
	Base   Node
	Except []Node
}

type NumRange struct {
	At             parser.Span
	Lo, Hi         uint64
	HasLo, HasHigh bool
}

type Anchor struct {
	At    parser.Span
	Start bool
}

type Flags struct {
	At    parser.Span
	Flags []Flag
}

type Flag struct {
	Name byte
	Arg  string
}

type Qualifiers struct {
	At       parser.Span
	Explicit bool
	List     []Qualifier
}

type Qualifier struct {
	At   parser.Span
	Name string
	Arg  string
}

type Escape struct {
	At   parser.Span
	Kind byte
}

type Backref struct {
	At parser.Span
	N  int
}

type KshOptions struct {
	At    parser.Span
	Flags string
	X     Node
}

type Search struct {
	At          parser.Span
	Flags       string
	Mode        byte
	X           Node
	Left, Right bool
}

type Foreign struct {
	At   parser.Span
	Mode byte
	Text string
}

type Unknown struct {
	At     parser.Span
	Source string
	Glob   bool
}

const unbounded = -1

func (n *Literal) Span() parser.Span      { return n.At }
func (n *AnyChar) Span() parser.Span      { return n.At }
func (n *AnyString) Span() parser.Span    { return n.At }
func (n *Recursive) Span() parser.Span    { return n.At }
func (n *Bracket) Span() parser.Span      { return n.At }
func (n *Group) Span() parser.Span        { return n.At }
func (n *Alternatives) Span() parser.Span { return n.At }
func (n *Seq) Span() parser.Span          { return n.At }
func (n *Repeat) Span() parser.Span       { return n.At }
func (n *Not) Span() parser.Span          { return n.At }
func (n *Exclude) Span() parser.Span      { return n.At }
func (n *NumRange) Span() parser.Span     { return n.At }
func (n *Anchor) Span() parser.Span       { return n.At }
func (n *Flags) Span() parser.Span        { return n.At }
func (n *Qualifiers) Span() parser.Span   { return n.At }
func (n *Unknown) Span() parser.Span      { return n.At }
func (n *Escape) Span() parser.Span       { return n.At }
func (n *Backref) Span() parser.Span      { return n.At }
func (n *KshOptions) Span() parser.Span   { return n.At }
func (n *Search) Span() parser.Span       { return n.At }
func (n *Foreign) Span() parser.Span      { return n.At }

func (*Literal) node()      {}
func (*AnyChar) node()      {}
func (*AnyString) node()    {}
func (*Recursive) node()    {}
func (*Bracket) node()      {}
func (*Group) node()        {}
func (*Alternatives) node() {}
func (*Seq) node()          {}
func (*Repeat) node()       {}
func (*Not) node()          {}
func (*Exclude) node()      {}
func (*NumRange) node()     {}
func (*Anchor) node()       {}
func (*Flags) node()        {}
func (*Qualifiers) node()   {}
func (*Unknown) node()      {}
func (*Escape) node()       {}
func (*Backref) node()      {}
func (*KshOptions) node()   {}
func (*Search) node()       {}
func (*Foreign) node()      {}

func Children(n Node) []Node {
	switch n := n.(type) {
	case *Group:
		return []Node{n.X}
	case *Alternatives:
		return n.Choices
	case *Seq:
		return n.Items
	case *Repeat:
		return []Node{n.X}
	case *Not:
		return []Node{n.X}
	case *Exclude:
		return append([]Node{n.Base}, n.Except...)
	case *KshOptions:
		if n.X != nil {
			return []Node{n.X}
		}
	case *Search:
		return []Node{n.X}
	}
	return nil
}

func Walk(n Node, visit func(Node) bool) {
	if !visit(n) {
		return
	}
	for _, child := range Children(n) {
		Walk(child, visit)
	}
}

func (p Pattern) Static() bool {
	static := true
	Walk(p.Root, func(n Node) bool {
		switch n.(type) {
		case *Literal, *Seq:
			return true
		}
		static = false
		return false
	})
	return static
}

func (p Pattern) Literal() (string, bool) {
	if !p.Static() {
		return "", false
	}
	var text []byte
	Walk(p.Root, func(n Node) bool {
		if lit, ok := n.(*Literal); ok {
			text = append(text, lit.Text...)
		}
		return true
	})
	return string(text), true
}
