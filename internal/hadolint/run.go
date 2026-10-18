package hadolint

import (
	"path"
	"slices"
	"sort"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type Word struct {
	Text       string
	Literal    bool
	Expandable bool
	Span       parser.Span
}

type Command struct {
	Words   []Word
	Start   int
	Assigns []string
	Span    parser.Span
}

func (c Command) Name() string {
	w := c.Words[c.Start]
	if !w.Literal {
		return ""
	}
	return path.Base(w.Text)
}

func (c Command) Args() []Word { return c.Words[c.Start+1:] }

func (c Command) Wrappers() []Word { return c.Words[:c.Start] }

type Run struct {
	Instruction *dockerfile.Instruction
	Trigger     *dockerfile.Instruction
	Stage       int
	Commands    []Command
	Pipes       []parser.Span
	Exec        bool
	Read        bool
}

func (r *Run) Reported() *dockerfile.Instruction {
	if r.Trigger != nil {
		return r.Trigger
	}
	return r.Instruction
}

type Script struct {
	Tree *parser.Tree
	Doc  *docstore.Document
}

var runners = parser.Wrappers{Programs: true}

func Runs(f *dockerfile.File, scripts []Script, read []parser.Span) []*Run {
	var runs []*Run
	for _, s := range f.Stages() {
		for _, in := range s.Instructions {
			run := &Run{Instruction: in, Stage: s.Index}
			if in.Name == "ONBUILD" && in.Inner != nil {
				run.Instruction, run.Trigger = in.Inner, in
			}
			if run.Instruction.Name != "RUN" {
				continue
			}
			if run.Instruction.Form == dockerfile.FormJSON {
				run.Exec, run.Read = true, true
				run.Commands = execCommands(run.Instruction)
			}
			run.Read = run.Read || slices.ContainsFunc(read, func(s parser.Span) bool {
				return run.Instruction.Span.Start <= s.Start && s.Start < run.Instruction.Span.End
			})
			runs = append(runs, run)
		}
	}
	for _, s := range scripts {
		collect(runs, s)
	}
	for _, run := range runs {
		slices.SortFunc(run.Commands, func(a, b Command) int { return a.Span.Start - b.Span.Start })
		slices.SortFunc(run.Pipes, func(a, b parser.Span) int { return a.Start - b.Start })
	}
	return runs
}

func execCommands(in *dockerfile.Instruction) []Command {
	if len(in.JSON) == 0 {
		return nil
	}
	words := make([]Word, len(in.JSON))
	texts := make([]string, len(in.JSON))
	for i, w := range in.JSON {
		words[i] = Word{Text: w.Text, Literal: true, Span: w.Span}
		texts[i] = w.Text
	}
	start := 0
	for layer := range runners.Layers(texts[0], texts[1:]) {
		start = layer.Offset
	}
	return []Command{{Words: words, Start: start, Span: parser.Span{Start: words[0].Span.Start, End: words[len(words)-1].Span.End}}}
}

var substitutions = []syntax.ParExpOperator{syntax.DefaultUnset, syntax.DefaultUnsetOrNull, syntax.AlternateUnset, syntax.AlternateUnsetOrNull}

func expandable(w *syntax.Word) bool {
	return w != nil && !slices.ContainsFunc(w.Parts, func(part syntax.WordPart) bool { return !expandablePart(part) })
}

func expandablePart(part syntax.WordPart) bool {
	switch p := part.(type) {
	case *syntax.Lit:
		return true
	case *syntax.SglQuoted:
		return !p.Dollar
	case *syntax.DblQuoted:
		return !slices.ContainsFunc(p.Parts, func(inner syntax.WordPart) bool { return !expandablePart(inner) })
	case *syntax.ParamExp:
		if p.Exp == nil {
			return parser.IsPlainParam(p)
		}
		bare := *p
		bare.Exp = nil
		return parser.IsPlainParam(&bare) && slices.Contains(substitutions, p.Exp.Op) && (p.Exp.Word == nil || expandable(p.Exp.Word))
	}
	return false
}

func containing(runs []*Run, offset int) *Run {
	i := sort.Search(len(runs), func(i int) bool { return runs[i].Instruction.Span.End > offset })
	if i < len(runs) && runs[i].Instruction.Span.Start <= offset && !runs[i].Exec {
		return runs[i]
	}
	return nil
}

func collect(runs []*Run, s Script) {
	host := func(span parser.Span) parser.Span {
		return parser.Span{Start: s.Doc.SourceOffset(span.Start), End: s.Doc.SourceOffset(span.End)}
	}
	syntax.Walk(s.Tree.File, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				return true
			}
			span, ok := s.Tree.NodeSpan(n)
			if !ok || s.Doc.Synthetic(span.Start) {
				return true
			}
			run := containing(runs, s.Doc.SourceOffset(span.Start))
			if run == nil {
				return true
			}
			start := runners.Start(n.Args)
			if start < 0 {
				return true
			}
			words := make([]Word, len(n.Args))
			for i, arg := range n.Args {
				text, literal := parser.Text(arg, "")
				ws, _ := s.Tree.NodeSpan(arg)
				words[i] = Word{Text: text, Literal: literal, Expandable: expandable(arg), Span: host(ws)}
			}
			assigns := make([]string, 0, len(n.Assigns))
			for _, a := range n.Assigns {
				value, _ := parser.Text(a.Value, "")
				assigns = append(assigns, a.Name.Value+"="+value)
			}
			run.Commands = append(run.Commands, Command{Words: words, Start: start, Assigns: assigns, Span: host(span)})
		case *syntax.BinaryCmd:
			if n.Op != syntax.Pipe && n.Op != syntax.PipeAll {
				return true
			}
			span, ok := s.Tree.NodeSpan(n)
			if !ok || s.Doc.Synthetic(span.Start) {
				return true
			}
			if run := containing(runs, s.Doc.SourceOffset(span.Start)); run != nil {
				run.Pipes = append(run.Pipes, host(span))
			}
		}
		return true
	})
}
