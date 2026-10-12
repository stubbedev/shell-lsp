package getopt

import (
	"iter"
	"slices"
	"strings"
)

type Arity uint8

const (
	Unknown Arity = iota
	Flag
	Valued
	Optional
)

const EndOfOptions = "--"

func IsOption(word string) bool {
	return len(word) > 1 && word[0] == '-' && word != EndOfOptions
}

func Words(option string, arity func(name string) Arity) (int, bool) {
	if strings.HasPrefix(option, EndOfOptions) {
		if strings.Contains(option, "=") {
			return 1, true
		}
		return span(arity(option))
	}
	if a := arity(option); a != Unknown || len(option) == 2 {
		return span(a)
	}
	words, known := 1, true
	for at, a := range letters(option, arity) {
		switch {
		case a == Unknown:
			known = false
		case a == Valued && at == len(option)-1:
			words = 2
		}
	}
	return words, known
}

func span(a Arity) (int, bool) {
	switch a {
	case Valued:
		return 2, true
	case Flag, Optional:
		return 1, true
	case Unknown:
	}
	return 1, false
}

func letters(option string, arity func(name string) Arity) iter.Seq2[int, Arity] {
	return func(yield func(int, Arity) bool) {
		for i := 1; i < len(option); i++ {
			a := arity(option[:1] + option[i:i+1])
			if !yield(i, a) || a == Valued || a == Optional {
				return
			}
		}
	}
}

type Grammar struct {
	Short         string
	Long          map[string]Arity
	StopAtOperand bool
	Plus          bool
}

func (g Grammar) isOption(word string) bool {
	return IsOption(word) || g.Plus && len(word) > 1 && word[0] == '+'
}

func (g Grammar) Arity(option string) Arity {
	if name, long := strings.CutPrefix(option, EndOfOptions); long {
		return g.Long[name]
	}
	if len(option) != 2 || (option[0] != '-' && option[0] != '+') || option[1] == ':' {
		return Unknown
	}
	at := strings.IndexByte(g.Short, option[1])
	if at < 0 {
		return Unknown
	}
	switch rest := g.Short[at+1:]; {
	case strings.HasPrefix(rest, "::"):
		return Optional
	case strings.HasPrefix(rest, ":"):
		return Valued
	}
	return Flag
}

type Argument[W any] struct {
	Word   W
	Offset int
}

func (a *Argument[W]) Text(literal func(W) (string, bool)) (string, bool) {
	if a == nil {
		return "", false
	}
	text, ok := literal(a.Word)
	if !ok {
		return "", false
	}
	return text[a.Offset:], true
}

type Option[W any] struct {
	Name     string
	Word     W
	Argument *Argument[W]
}

type Parsed[W any] struct {
	Options  []Option[W]
	Operands []W
	Complete bool
	Missing  bool
}

func (p Parsed[W]) Has(names ...string) bool {
	return slices.ContainsFunc(p.Options, func(o Option[W]) bool { return slices.Contains(names, o.Name) })
}

func Parse[W any](g Grammar, words []W, literal func(W) (string, bool)) Parsed[W] {
	out := Parsed[W]{Complete: true}
	for i := 0; i < len(words); i++ {
		lit, ok := literal(words[i])
		switch {
		case ok && lit == EndOfOptions:
			out.Operands = append(out.Operands, words[i+1:]...)
			return out
		case !ok || !g.isOption(lit):
			if g.StopAtOperand {
				out.Operands = append(out.Operands, words[i:]...)
				return out
			}
			out.Operands = append(out.Operands, words[i])
		case strings.HasPrefix(lit, EndOfOptions):
			i = out.long(g, words, i, lit)
		default:
			i = out.short(g, words, i, lit)
		}
	}
	return out
}

func (p *Parsed[W]) long(g Grammar, words []W, i int, lit string) int {
	name, _, attached := strings.Cut(lit[len(EndOfOptions):], "=")
	opt := Option[W]{Name: EndOfOptions + name, Word: words[i]}
	a := g.Long[name]
	switch {
	case attached:
		opt.Argument = &Argument[W]{Word: words[i], Offset: len(EndOfOptions) + len(name) + 1}
		p.Complete = p.Complete && (a == Valued || a == Optional)
	case a == Unknown:
		p.Complete = false
	case a == Valued:
		i = p.separate(&opt, words, i)
	}
	p.Options = append(p.Options, opt)
	return i
}

func (p *Parsed[W]) short(g Grammar, words []W, i int, lit string) int {
	next := i
	for at, a := range letters(lit, g.Arity) {
		opt := Option[W]{Name: lit[:1] + lit[at:at+1], Word: words[i]}
		switch {
		case a == Unknown:
			p.Complete = false
		case a == Flag:
		case at+1 < len(lit):
			opt.Argument = &Argument[W]{Word: words[i], Offset: at + 1}
		case a == Valued:
			next = p.separate(&opt, words, i)
		}
		p.Options = append(p.Options, opt)
	}
	return next
}

func (p *Parsed[W]) separate(opt *Option[W], words []W, i int) int {
	if i+1 == len(words) {
		p.Complete, p.Missing = false, true
		return i
	}
	opt.Argument = &Argument[W]{Word: words[i+1]}
	return i + 1
}
