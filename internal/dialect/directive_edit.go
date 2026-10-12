package dialect

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func HasShellDirective(text string) bool {
	_, ok := findShellDirective(text)
	return ok
}

func ShellDirectiveEdits(text string, shell *Dialect) []textedit.Edit {
	found, ok := findShellDirective(text)
	if shell == nil {
		if !ok {
			return nil
		}
		return []textedit.Edit{found.removal()}
	}
	name := shell.String()
	if ok {
		if found.pair.Value == name {
			return nil
		}
		start := found.line.Start
		return []textedit.Edit{{Start: start + found.pair.ValueOffset, End: start + found.pair.ValueEnd, NewText: name}}
	}
	return []textedit.Edit{directive.HeaderInsert(text, directive.Shell, name)}
}

func (f shellDirective) removal() textedit.Edit {
	start, pairs := f.line.Start, f.line.Directive.Pairs
	if len(pairs) == 1 {
		return textedit.Edit{Start: start, End: f.line.Next}
	}
	i := slices.Index(pairs, f.pair)
	if i == len(pairs)-1 {
		return textedit.Edit{Start: start + pairs[i-1].ValueEnd, End: start + f.pair.ValueEnd}
	}
	return textedit.Edit{Start: start + f.pair.KeyOffset, End: start + pairs[i+1].KeyOffset}
}
