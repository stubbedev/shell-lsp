package hadolint

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/bake"
	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func mustCode(text string) lint.Code {
	code, err := lint.ParseCode(text)
	if err != nil {
		panic(err)
	}
	return code
}

var aliases = map[lint.Code][]lint.Code{
	mustCode("DL1000"): {lint.CodeDockerSyntax, lint.CodeDockerUnknownInstruction, lint.CodeDockerUnknownFlag},
	mustCode("DL3000"): {lint.CodeDockerWorkdirRelative},
	mustCode("DL3012"): {lint.CodeDockerMultipleInstructions},
	mustCode("DL3022"): {lint.CodeDockerUnknownStage},
	mustCode("DL3023"): {lint.CodeDockerStageUsedBeforeDefinition},
	mustCode("DL3024"): {lint.CodeDockerDuplicateStage},
	mustCode("DL3025"): {lint.CodeDockerJSONArgsRecommended},
	mustCode("DL3029"): {lint.CodeDockerConstantPlatform},
	mustCode("DL3043"): {lint.CodeDockerSyntax},
	mustCode("DL3061"): {lint.CodeDockerSyntax},
	mustCode("DL4000"): {lint.CodeDockerMaintainerDeprecated},
	mustCode("DL4003"): {lint.CodeDockerMultipleInstructions},
	mustCode("DL4004"): {lint.CodeDockerMultipleInstructions},
}

func Covered(code lint.Code) []lint.Code { return slices.Clone(aliases[code]) }

func expand(codes []lint.Code) []lint.Code {
	out := slices.Clone(codes)
	for _, code := range codes {
		out = append(out, aliases[code]...)
	}
	return out
}

type scoped struct {
	span  parser.Span
	codes []lint.Code
}

type Suppression struct {
	global    []lint.Code
	scoped    []scoped
	overrides map[lint.Code]lint.Severity
}

func NewSuppression(f *dockerfile.File, cfg Config, contexts bake.Contexts) Suppression {
	s := Suppression{global: expand(cfg.Ignored), overrides: map[lint.Code]lint.Severity{}, scoped: namedContextCopies(f, contexts)}
	for code, severity := range cfg.Overrides {
		for _, c := range expand([]lint.Code{code}) {
			s.overrides[c] = severity
		}
	}
	if cfg.DisableIgnorePragma {
		return s
	}
	for _, p := range f.Pragmas() {
		switch p.Kind {
		case dockerfile.PragmaGlobalIgnore:
			s.global = append(s.global, expand(p.Codes)...)
		case dockerfile.PragmaIgnore:
			next := lineAfter(f.Text, p.Span.End)
			for _, in := range f.Instructions {
				if lineStart(f.Text, in.Span.Start) == next {
					s.scoped = append(s.scoped, scoped{span: in.Span, codes: expand(p.Codes)})
				}
			}
		case dockerfile.PragmaShell:
		}
	}
	return s
}

func (s Suppression) Apply(findings []lint.Finding, hostOffset func(int) int) []lint.Finding {
	out := make([]lint.Finding, 0, len(findings))
	for _, f := range findings {
		at := hostOffset(f.Span.Start)
		if slices.Contains(s.global, f.Code) || slices.ContainsFunc(s.scoped, func(sc scoped) bool {
			return sc.span.Start <= at && at < sc.span.End && slices.Contains(sc.codes, f.Code)
		}) {
			continue
		}
		if severity, ok := s.overrides[f.Code]; ok {
			f.Severity = severity
		}
		out = append(out, f)
	}
	return out
}

func namedContextCopies(f *dockerfile.File, contexts bake.Contexts) []scoped {
	var out []scoped
	for _, in := range f.Instructions {
		if from, _, named := copiedContext(in, contexts); named {
			out = append(out, scoped{span: from.Span, codes: []lint.Code{lint.CodeDockerUnknownStage}})
		}
	}
	return out
}

func copiedContext(in *dockerfile.Instruction, contexts bake.Contexts) (dockerfile.Word, []bake.Value, bool) {
	if in.Name != "COPY" {
		return dockerfile.Word{}, nil, false
	}
	for _, flag := range in.Flags {
		if flag.Name.Text != stageCopyKey || flag.Value == nil {
			continue
		}
		ref, valid := dockerfile.ParseReference(flag.Value.Text)
		if values, named := contexts[ref.ContextName()]; valid && named {
			return *flag.Value, values, true
		}
	}
	return dockerfile.Word{}, nil, false
}
