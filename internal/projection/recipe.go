package projection

import "slices"

const (
	subshellOpen  = "(\n"
	subshellClose = ")\n"
)

type RecipeLine struct {
	Start, Command, End int
	Comment             bool
}

func (l RecipeLine) Empty() bool { return l.Command == l.End }

type LineShells struct {
	b         *Builder
	prelude   string
	commented bool
}

func (b *Builder) LineShells(prelude string) *LineShells {
	return &LineShells{b: b, prelude: prelude}
}

func (s *LineShells) Add(l RecipeLine, body func(RecipeLine)) {
	if l.Empty() {
		return
	}
	if !s.commented {
		s.b.Script()
	}
	s.commented = l.Comment
	s.b.Opaque(l.Start, l.Command, "")
	if l.Comment {
		body(l)
		s.b.Opaque(l.End, l.End, "\n")
		return
	}
	s.b.Opaque(l.Command, l.Command, subshellOpen+s.prelude)
	body(l)
	s.b.Opaque(l.End, l.End, "\n"+subshellClose)
}

func (b *Builder) OneShell(prelude string, lines []RecipeLine, body func(RecipeLine)) {
	b.Script()
	wrapped := slices.ContainsFunc(lines, func(l RecipeLine) bool { return !l.Empty() && !l.Comment })
	for i, l := range lines {
		b.Opaque(l.Start, l.Command, "")
		if wrapped && i == 0 {
			b.Opaque(l.Command, l.Command, subshellOpen+prelude)
		}
		body(l)
		b.Opaque(l.End, l.End, "\n")
	}
	if wrapped {
		last := lines[len(lines)-1].End
		b.Opaque(last, last, subshellClose)
	}
}
