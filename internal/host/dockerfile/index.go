package dockerfile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

const maxDetail = 80

var Checks = model.Checks{Of: rules, Deprecated: lint.CodeDockerMaintainerDeprecated}

func rules(kind model.Kind) model.Rules {
	switch kind {
	case model.KindVariable:
		return model.Rules{Unknown: lint.CodeDockerUndefinedVariable, Unused: lint.CodeDockerUnusedArg}
	case model.KindRecipe, model.KindAlias, model.KindParameter, model.KindFunction, model.KindModule, model.KindTarget, model.KindSetting, model.KindAttribute, model.KindKeyword, model.KindStage:
		return model.Rules{}
	}
	panic(fmt.Sprintf("dockerfile: kind %s has no rules", kind))
}

func ValidName(kind model.Kind, name string) bool {
	switch kind {
	case model.KindVariable:
		return name != "" && nameEnd(name, 0) == len(name)
	case model.KindStage:
		return validStageName(name)
	case model.KindRecipe, model.KindAlias, model.KindParameter, model.KindFunction, model.KindModule, model.KindTarget, model.KindSetting, model.KindAttribute, model.KindKeyword:
		return false
	}
	panic(fmt.Sprintf("dockerfile: kind %s has no names", kind))
}

func validStageName(name string) bool {
	if name == "" || ((name[0] < 'a' || name[0] > 'z') && (name[0] < 'A' || name[0] > 'Z')) {
		return false
	}
	return !strings.ContainsFunc(name, func(c rune) bool {
		return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.'
	})
}

type indexer struct {
	f      *File
	ix     *model.Index
	stages []Stage
}

func detail(text string) string {
	text, _, _ = strings.Cut(text, "\n")
	if len(text) > maxDetail {
		return text[:maxDetail] + "…"
	}
	return text
}

func (f *File) Index(shells []model.Shell) *model.Index {
	ix := &model.Index{Shells: shells, Extends: map[model.Scope][]model.Scope{}, Broken: len(f.Errors) > 0}
	in := &indexer{f: f, ix: ix, stages: f.Stages()}
	for _, d := range f.Directives {
		in.token(d.Name.Span, semantic.TypeDecorator, 0)
		in.token(d.Value.Span, semantic.TypeString, 0)
	}
	for _, instr := range f.Preamble() {
		in.instruction(instr, preamble, false)
	}
	for _, s := range in.stages {
		in.stage(s)
	}
	for _, c := range f.Comments {
		in.token(c, semantic.TypeComment, 0)
	}
	ix.Findings = f.check()
	slices.SortFunc(ix.Tokens, func(a, b semantic.Token) int { return a.Span.Start - b.Span.Start })
	return ix
}

func (in *indexer) token(span Span, t semantic.Type, mods semantic.Modifier) {
	if span.End > span.Start {
		in.ix.Tokens = append(in.ix.Tokens, semantic.Token{Span: span, Type: t, Modifiers: mods})
	}
}

func (in *indexer) symbol(s model.Symbol) int {
	in.ix.Symbols = append(in.ix.Symbols, s)
	return len(in.ix.Symbols) - 1
}

func (in *indexer) stage(s Stage) {
	from := s.From
	owner := model.NoSymbol
	if s.Name != nil {
		owner = in.symbol(model.Symbol{
			Name: strings.ToLower(s.Name.Text), Kind: model.KindStage, Span: s.Span, Selection: s.Name.Span, Parent: model.NoParent,
			Detail: detail(in.f.Text[from.Line.Start:from.Line.End]), Arity: model.NoArity,
		})
		in.token(s.Name.Span, semantic.TypeNamespace, semantic.ModDeclaration)
		in.token(from.Args[1].Span, semantic.TypeKeyword, 0)
	}
	if s.Parent >= 0 {
		in.ix.Extends[s.Scope] = []model.Scope{stageScope(s.Parent)}
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: strings.ToLower(s.Image.Text), Kind: model.KindStage, Span: s.Image.Span, Args: model.NoArgs, Owner: owner, Soft: true})
		in.token(s.Image.Span, semantic.TypeNamespace, 0)
	}
	in.keyword(from)
	in.flags(from, preamble)
	if s.Image != nil {
		in.refs([]Word{*s.Image}, preamble, from.Span.Start, true)
	}
	if strings.Contains(in.f.Text[s.Span.Start:s.Span.End], "\n") {
		in.ix.Folds = append(in.ix.Folds, model.Fold{Span: Span{Start: s.Span.Start, End: lastLineEnd(in.f.Text, s.Span)}})
	}
	for i, instr := range s.Instructions {
		in.instruction(instr, s.Scope, slices.ContainsFunc(s.Instructions[i+1:], func(later *Instruction) bool { return later.Name == "RUN" }))
	}
}

func lastLineEnd(text string, s Span) int {
	return s.Start + len(strings.TrimRight(text[s.Start:s.End], " \t\r\n"))
}

func (in *indexer) keyword(instr *Instruction) {
	in.token(instr.Keyword.Span, semantic.TypeKeyword, 0)
	in.ix.Refs = append(in.ix.Refs, model.Ref{Name: instr.Name, Kind: model.KindKeyword, Span: instr.Keyword.Span, Args: model.NoArgs, Owner: model.NoSymbol})
	if strings.Contains(in.f.Text[instr.Span.Start:instr.Span.End], "\n") && instr.Name != "FROM" {
		in.ix.Folds = append(in.ix.Folds, model.Fold{Span: Span{Start: instr.Span.Start, End: lastLineEnd(in.f.Text, instr.Span)}})
	}
}

func (in *indexer) flags(instr *Instruction, scope model.Scope) {
	for _, f := range instr.Flags {
		in.token(f.Name.Span, semantic.TypeParameter, 0)
		in.ix.Refs = append(in.ix.Refs, model.Ref{Name: f.Name.Text, Kind: model.KindAttribute, Span: f.Name.Span, Args: model.NoArgs, Owner: model.NoSymbol})
		if f.Value == nil {
			continue
		}
		in.words([]Word{*f.Value}, scope, instr.Span.Start)
		switch {
		case instr.Name == "COPY" && f.Name.Text == "from":
			in.stageRef(*f.Value)
		case instr.Name == "RUN" && f.Name.Text == "mount":
			for opt := range strings.SplitSeq(f.Value.Text, ",") {
				if name, ok := strings.CutPrefix(opt, "from="); ok {
					at := f.Value.Span.Start + strings.Index(f.Value.Text, opt) + len("from=")
					in.stageRef(Word{Text: name, Span: Span{Start: at, End: at + len(name)}})
				}
			}
		}
	}
}

func (in *indexer) stageRef(w Word) {
	if strings.ContainsRune(w.Text, '$') || !validStageName(w.Text) {
		return
	}
	in.ix.Refs = append(in.ix.Refs, model.Ref{Name: strings.ToLower(w.Text), Kind: model.KindStage, Span: w.Span, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true})
	in.token(w.Span, semantic.TypeNamespace, 0)
}

func (in *indexer) words(words []Word, scope model.Scope, horizon int) {
	in.refs(words, scope, horizon, false)
}

func (in *indexer) refs(words []Word, scope model.Scope, horizon int, soft bool) {
	for _, w := range words {
		for _, r := range in.f.References(w) {
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: r.Name, Kind: model.KindVariable, Span: r.Span, Scope: scope, Args: model.NoArgs, Owner: model.NoSymbol, Horizon: horizon, Soft: soft})
			in.token(r.Span, semantic.TypeVariable, 0)
		}
	}
}

func (in *indexer) instruction(instr *Instruction, scope model.Scope, run bool) {
	in.keyword(instr)
	if instr.Inner != nil {
		in.instruction(instr.Inner, scope, false)
		return
	}
	in.flags(instr, scope)
	switch instr.Name {
	case "ARG":
		in.variables(instr, scope, true, run)
	case "ENV":
		in.variables(instr, scope, false, run)
	case "LABEL":
		for _, p := range in.f.Pairs(instr) {
			if p.Value != nil {
				in.words([]Word{*p.Value}, scope, instr.Span.Start)
			}
		}
	case "RUN", "CMD", "ENTRYPOINT", "SHELL":
	case "HEALTHCHECK":
		if len(instr.Args) > 0 {
			in.token(instr.Args[0].Span, semantic.TypeKeyword, 0)
		}
	case "COPY", "ADD":
		in.words(instr.JSON, scope, instr.Span.Start)
		in.words(instr.Args, scope, instr.Span.Start)
		in.paths(instr)
	default:
		in.words(instr.JSON, scope, instr.Span.Start)
		in.words(instr.Args, scope, instr.Span.Start)
	}
	for _, h := range instr.Heredocs {
		in.token(h.Name.Span, semantic.TypeString, 0)
		if strings.Contains(in.f.Text[h.Span.Start:h.Span.End], "\n") {
			in.ix.Folds = append(in.ix.Folds, model.Fold{Span: Span{Start: h.Span.Start, End: h.Span.End}})
		}
	}
}

func (in *indexer) variables(instr *Instruction, scope model.Scope, arg, run bool) {
	for _, p := range in.f.Pairs(instr) {
		if p.Key.Text == "" || nameEnd(p.Key.Text, 0) != len(p.Key.Text) {
			continue
		}
		header := instr.Name + " " + p.Key.Text
		value := ""
		if p.Value != nil {
			value = p.Value.Text
			header += "=" + value
			in.words([]Word{*p.Value}, scope, instr.Span.Start)
		}
		in.symbol(model.Symbol{
			Name: p.Key.Text, Kind: model.KindVariable, Span: instr.Span, Selection: p.Key.Span, Scope: scope, Parent: model.NoParent,
			Detail: detail(header), Exported: scope != preamble, Unused: arg && !run && !strings.HasPrefix(p.Key.Text, buildkitArgPrefix), Local: arg, Value: value, Constant: !strings.ContainsRune(value, '$'), Arity: model.NoArity,
		})
		in.token(p.Key.Span, semantic.TypeVariable, semantic.ModDeclaration)
		if arg && p.Value == nil && scope != preamble {
			in.ix.Refs = append(in.ix.Refs, model.Ref{Name: p.Key.Text, Kind: model.KindVariable, Span: p.Key.Span, Scope: preamble, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true})
		}
	}
}

func (in *indexer) paths(instr *Instruction) {
	if slices.ContainsFunc(instr.Flags, func(f Flag) bool { return f.Name.Text == "from" }) {
		return
	}
	sources := instr.Args
	if instr.Form == FormJSON {
		sources = instr.JSON
	}
	if len(sources) < 2 {
		return
	}
	for _, w := range sources[:len(sources)-1] {
		if strings.HasPrefix(w.Text, "<<") || strings.Contains(w.Text, "://") || strings.ContainsAny(w.Text, "$*?[") {
			continue
		}
		in.ix.Paths = append(in.ix.Paths, model.PathRef{Span: w.Span, Path: w.Text})
	}
}
