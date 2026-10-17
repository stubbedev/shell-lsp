package dockerfile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

const (
	tool          = "Dockerfile"
	defaultShell  = "/bin/sh -c"
	subshellOpen  = "(\n"
	subshellClose = "\n)\n"
)

var defaultDetection = dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostDefault, Evidence: tool}

type Analysis struct {
	File      *File
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
}

func Analyze(text string) Analysis {
	f := Parse(text)
	m, detection, shells := f.project()
	return Analysis{File: f, Map: m, Detection: detection, Index: f.Index(shells)}
}

func Project(text string) (*projection.Map, dialect.Detection) {
	m, detection, _ := Parse(text).project()
	return m, detection
}

type projector struct {
	f      *File
	b      *projection.Builder
	shells []model.Shell
}

func lineEncoder(_ string, _, _ int, text string) (string, bool) {
	return text, !strings.ContainsRune(text, '\n')
}

func bodyEncoder(_ string, _, _ int, text string) (string, bool) { return text, true }

func (f *File) project() (*projection.Map, dialect.Detection, []model.Shell) {
	p := &projector{f: f, b: projection.NewBuilder(f.Text)}
	p.b.Shell(defaultDetection)
	var detections []dialect.Detection
	type stageShell struct {
		detection dialect.Detection
		analysed  bool
	}
	stages := f.Stages()
	ends := make([]stageShell, len(stages))
	for i, s := range stages {
		shell, analysed := defaultDetection, true
		if s.Parent >= 0 {
			shell, analysed = ends[s.Parent].detection, ends[s.Parent].analysed
		}
		declared := f.inheritedNames(s)
		for _, in := range s.Instructions {
			if in.Name == "SHELL" && in.Form == FormJSON {
				shell, analysed = shellOf(f, in)
			}
			for _, target := range []*Instruction{in, in.Inner} {
				if target != nil && analysed && p.script(target, shell, declared, s.Scope) {
					detections = append(detections, shell)
				}
			}
			declared = append(declared, f.declares(in)...)
		}
		ends[i] = stageShell{detection: shell, analysed: analysed}
	}
	detection, _ := dialect.Choose(detections, func(d dialect.Detection) dialect.Detection { return d }, tool)
	return p.b.Map(), detection, p.shells
}

func shellOf(f *File, in *Instruction) (dialect.Detection, bool) {
	words := make([]string, len(in.JSON))
	for i, w := range in.JSON {
		words[i] = w.Text
	}
	d, ok := dialect.FromShebang("#!" + strings.Join(words, " "))
	return dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: f.Text[in.Line.Start:in.Line.End]}, ok
}

func (f *File) declares(in *Instruction) []string {
	if in.Name != "ARG" && in.Name != "ENV" {
		return nil
	}
	var out []string
	for _, p := range f.Pairs(in) {
		out = append(out, p.Key.Text)
	}
	return out
}

func (f *File) inheritedNames(s Stage) []string {
	var out []string
	stages := f.Stages()
	for parent := s.Parent; parent >= 0; parent = stages[parent].Parent {
		for _, in := range stages[parent].Instructions {
			if in.Name == "ENV" {
				out = append(out, f.declares(in)...)
			}
		}
	}
	return out
}

func prelude(names []string) string {
	var sb strings.Builder
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	for _, name := range slices.Compact(sorted) {
		if nameEnd(name, 0) == len(name) && name != "" {
			sb.WriteString(name + "=" + projection.Dynamic + "\n")
		}
	}
	return sb.String()
}

func (p *projector) script(in *Instruction, shell dialect.Detection, declared []string, scope model.Scope) bool {
	if in.Form != FormShell {
		return false
	}
	command := strings.TrimSpace(in.Logical(p.f.Text, in.Command))
	if len(in.Heredocs) == 1 && bareHeredoc(command) {
		return p.heredocScript(in.Heredocs[0], shell, declared, scope)
	}
	p.b.Shell(shell)
	p.b.Region(lineEncoder)
	p.b.Script()
	p.b.Opaque(in.Command.Start, in.Command.Start, subshellOpen+prelude(declared))
	p.command(in)
	end := in.Command.End
	for _, h := range in.Heredocs {
		p.b.Opaque(end, end, "\n")
		p.b.Region(bodyEncoder)
		p.b.Verbatim(h.Body.Start, h.Span.End)
		end = h.Span.End
	}
	p.b.Opaque(end, end, subshellClose)
	p.shells = append(p.shells, model.Shell{Span: Span{Start: in.Command.Start, End: end}, Scope: scope})
	return true
}

func bareHeredoc(command string) bool {
	_, _, _, ok := heredocMarker(command)
	return ok
}

func (p *projector) command(in *Instruction) {
	at := in.Command.Start
	for _, piece := range in.Pieces {
		if piece.Comment {
			continue
		}
		from, to := max(piece.Span.Start, in.Command.Start), min(piece.Span.End, in.Command.End)
		if from >= to {
			continue
		}
		if at < from {
			p.b.Opaque(at, from, "\\\n")
		}
		p.b.Verbatim(from, to)
		at = to
	}
}

func (p *projector) heredocScript(h Heredoc, shell dialect.Detection, declared []string, scope model.Scope) bool {
	body := p.f.Text[h.Body.Start:h.Body.End]
	if first, _, _ := strings.Cut(body, "\n"); strings.HasPrefix(first, "#!") {
		d, ok := dialect.FromShebang(first)
		if !ok {
			return false
		}
		shell = dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: first}
	}
	p.b.Shell(shell)
	p.b.Region(bodyEncoder)
	p.b.Script()
	p.b.Opaque(h.Body.Start, h.Body.Start, subshellOpen+prelude(declared))
	p.b.Verbatim(h.Body.Start, h.Body.End)
	p.b.Opaque(h.Body.End, h.Body.End, subshellClose)
	p.shells = append(p.shells, model.Shell{Span: h.Body, Scope: scope})
	return true
}
