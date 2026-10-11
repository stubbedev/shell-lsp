package dialect

import (
	"slices"
	"strings"

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
		return []textedit.Edit{{Start: found.line + found.pair.ValueOffset, End: found.line + found.pair.ValueEnd, NewText: name}}
	}
	line := "# " + directive.Word + " " + directive.Shell + "=" + name + "\n"
	if !strings.HasPrefix(text, "#!") {
		return []textedit.Edit{{NewText: line}}
	}
	end := strings.IndexByte(text, '\n')
	if end < 0 {
		return []textedit.Edit{{Start: len(text), End: len(text), NewText: "\n" + line}}
	}
	return []textedit.Edit{{Start: end + 1, End: end + 1, NewText: line}}
}

func (f shellDirective) removal() textedit.Edit {
	pairs := f.directive.Pairs
	if len(pairs) == 1 {
		return textedit.Edit{Start: f.line, End: f.lineEnd}
	}
	i := slices.Index(pairs, f.pair)
	if i == len(pairs)-1 {
		return textedit.Edit{Start: f.line + pairs[i-1].ValueEnd, End: f.line + f.pair.ValueEnd}
	}
	return textedit.Edit{Start: f.line + f.pair.KeyOffset, End: f.line + pairs[i+1].KeyOffset}
}
