package actions

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var pipefailShells = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh)

func strictMode(s *site) (Change, bool) {
	text := s.Tree.Text()
	if !strings.HasPrefix(text, "#!") || s.Tree.Bats {
		return Change{}, false
	}
	pipefail := pipefailShells.Has(s.Tree.Dialect)
	set := lint.StrictModeOf(s.Tree)
	if set.Errexit && set.Nounset && (set.Pipefail || !pipefail) {
		return Change{}, false
	}
	line := "set -eu"
	if pipefail {
		line = "set -euo pipefail"
	}
	at := headerEnd(text)
	insert := line + "\n"
	switch {
	case at == len(text) && !strings.HasSuffix(text, "\n"):
		insert = "\n" + insert
	case at < len(text) && text[at] != '\n':
		insert += "\n"
	}
	return Change{Title: "Add " + line, Edits: []textedit.Edit{{Start: at, End: at, NewText: insert}}}, true
}

func headerEnd(text string) int {
	at := min(lineEnd(text, 0)+1, len(text))
	for at < len(text) {
		end := lineEnd(text, at)
		if !strings.HasPrefix(strings.TrimSpace(text[at:end]), "#") {
			break
		}
		at = min(end+1, len(text))
	}
	return at
}
