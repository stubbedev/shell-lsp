package navigation

import (
	"context"
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func Programs(file *workspace.File) []embedded.Program {
	return workspace.Memo(file, "embedded.programs", 0, func() []embedded.Program { return embedded.Find(file.Tree) })
}

type programName struct {
	program *embedded.Program
	binding embedded.Binding
	span    parser.Span
}

func programNameAt(file *workspace.File, offset int) (programName, bool) {
	programs := Programs(file)
	for i := range programs {
		p := &programs[i]
		name, ok := p.VariableAt(offset)
		if !ok {
			b, at := p.BindingAt(offset)
			name, ok = b.Name, at
		}
		if !ok || p.Shadows(name) {
			continue
		}
		b, bound := p.Binding(name)
		if !bound {
			continue
		}
		found := programName{program: p, binding: b, span: b.Span}
		for _, read := range p.Reads(name) {
			if read.Start-1 <= offset && offset <= read.End {
				found.span = read
			}
		}
		return found, true
	}
	return programName{}, false
}

func (n programName) occurrences(file *workspace.File) []Occurrence {
	out := []Occurrence{{File: file, Span: n.binding.Span, Write: true, Declaration: true}}
	for _, read := range n.program.Reads(n.binding.Name) {
		out = append(out, Occurrence{File: file, Span: read})
	}
	return out
}

func (n programName) refusal(file *workspace.File) string {
	occs := n.occurrences(file)
	spans := make([]parser.Span, len(occs))
	for i, o := range occs {
		spans[i] = o.Span
	}
	return n.program.RenameRefusal(n.binding.Name, file.Doc.Text(), spans)
}

func programDefinition(file *workspace.File, n programName, env Env, mode Mode) []Location {
	out := []Location{{File: file, Span: n.binding.Span}}
	if n.binding.Kind != embedded.ValueVariable {
		return out
	}
	return append(out, variableLocations(file, n.binding.Read, file.Tree.ScopeAt(n.binding.ReadSpan.Start), env, mode)...)
}

func programReferences(ctx context.Context, file *workspace.File, n programName, env Env, files WorkspaceFiles) ([]Occurrence, error) {
	occs := n.occurrences(file)
	if n.binding.Kind != embedded.ValueVariable {
		return mergeOccurrences(occs), nil
	}
	sym, ok := SymbolAt(file, n.binding.ReadSpan.Start)
	if !ok {
		return mergeOccurrences(occs), nil
	}
	group, err := Group(ctx, file, env, files)
	if err != nil {
		return nil, err
	}
	return mergeOccurrences(append(occs, Occurrences(sym, group)...)), nil
}

func programRename(file *workspace.File, n programName, newName string) ([]Occurrence, error) {
	if refusal := n.refusal(file); refusal != "" {
		return nil, fmt.Errorf("%w: %s", ErrNotRenamable, refusal)
	}
	if !parser.IsName(newName) {
		return nil, fmt.Errorf("%w: %q is not a valid name in a %s program", ErrInvalidName, newName, n.program.Language)
	}
	if newName == n.binding.Name {
		return nil, nil
	}
	if n.program.Taken(newName) {
		return nil, fmt.Errorf("%w: this %s program already uses `%s`", ErrNameConflict, n.program.Language, newName)
	}
	return n.occurrences(file), nil
}
