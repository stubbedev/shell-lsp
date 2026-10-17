package dockerfile

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

const preamble model.Scope = 1

type Stage struct {
	From         *Instruction
	Index        int
	Name         *Word
	Image        *Word
	Scope        model.Scope
	Span         Span
	Parent       int
	Instructions []*Instruction
}

func (s Stage) Key() string {
	if s.Name == nil {
		return ""
	}
	return strings.ToLower(s.Name.Text)
}

func stageScope(index int) model.Scope { return model.Scope(index + 2) }

func (f *File) Preamble() []*Instruction {
	var out []*Instruction
	for _, in := range f.Instructions {
		if in.Name == "FROM" {
			break
		}
		out = append(out, in)
	}
	return out
}

func (f *File) Stages() []Stage {
	var out []Stage
	for _, in := range f.Instructions {
		if in.Name == "FROM" {
			s := Stage{From: in, Index: len(out), Scope: stageScope(len(out)), Span: Span{Start: in.Span.Start, End: len(f.Text)}, Parent: -1}
			if len(in.Args) > 0 {
				s.Image = &in.Args[0]
			}
			if len(in.Args) == 3 && strings.EqualFold(in.Args[1].Text, "AS") {
				s.Name = &in.Args[2]
			}
			if s.Image != nil {
				image := strings.ToLower(s.Image.Text)
				for _, earlier := range out {
					if earlier.Key() != "" && earlier.Key() == image {
						s.Parent = earlier.Index
					}
				}
			}
			if n := len(out); n > 0 {
				out[n-1].Span.End = in.Span.Start
			}
			out = append(out, s)
			continue
		}
		if n := len(out); n > 0 {
			out[n-1].Instructions = append(out[n-1].Instructions, in)
		}
	}
	return out
}

func (f *File) StageNamed(name string, before int) (Stage, bool) {
	key := strings.ToLower(name)
	for _, s := range f.Stages() {
		if s.Index < before && s.Key() == key {
			return s, true
		}
	}
	return Stage{}, false
}
