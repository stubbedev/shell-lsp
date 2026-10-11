package actions

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var pipefailShells = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh)

func strictMode(s *site) (Change, bool) {
	text := s.Tree.Text()
	if !strings.HasPrefix(text, "#!") || s.Tree.Bats {
		return Change{}, false
	}
	have := lint.StrictModeOf(s.Tree)
	missing := lint.StrictMode{Errexit: !have.Errexit, Nounset: !have.Nounset, Pipefail: !have.Pipefail && pipefailShells.Has(s.Tree.Dialect)}
	if missing == (lint.StrictMode{}) {
		return Change{}, false
	}
	if c, ok := strictModeLine(s.Tree); ok {
		if edits, ok := extendStrictMode(s.Tree, c, missing); ok {
			line := text[c.Span.Start:c.Span.End]
			shifted := make([]textedit.Edit, len(edits))
			for i, e := range edits {
				shifted[i] = textedit.Edit{Start: e.Start - c.Span.Start, End: e.End - c.Span.Start, NewText: e.NewText}
			}
			return Change{Title: "Change " + line + " to " + textedit.Apply(line, shifted), Edits: edits}, true
		}
	}
	line := "set " + shortStrictFlags(missing)
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

func shortStrictFlags(m lint.StrictMode) string {
	flags := "-"
	if m.Errexit {
		flags += "e"
	}
	if m.Nounset {
		flags += "u"
	}
	if m.Pipefail {
		flags += "o pipefail"
	}
	return flags
}

func strictModeLine(tree *parser.Tree) (parser.Command, bool) {
	for _, c := range tree.Commands {
		if c.Within != parser.FileScope || lint.StrictModeSetBy(c) == (lint.StrictMode{}) {
			continue
		}
		for _, stmt := range tree.File.Stmts {
			call, ok := stmt.Cmd.(*syntax.CallExpr)
			if !ok || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) > 0 {
				continue
			}
			if span, ok := tree.NodeSpan(call); ok && span == c.Span {
				return c, true
			}
		}
	}
	return parser.Command{}, false
}

func extendStrictMode(tree *parser.Tree, c parser.Command, missing lint.StrictMode) ([]textedit.Edit, bool) {
	switch c.Name {
	case "set":
		return extendSet(tree, c, missing)
	case "setopt":
		return extendSetopt(c, missing)
	}
	return nil, false
}

func extendSet(tree *parser.Tree, c parser.Command, missing lint.StrictMode) ([]textedit.Edit, bool) {
	cluster := -1
	optionName := false
	for i, arg := range c.Args {
		lit, ok := parser.Literal(arg)
		switch {
		case !ok:
			return nil, false
		case optionName:
			optionName = false
		case len(lit) > 1 && lit[0] == '-' && strings.Trim(lit[1:], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") == "":
			switch o := strings.IndexByte(lit, 'o'); {
			case o == len(lit)-1:
				optionName = true
			case o > 0:
				return nil, false
			case cluster < 0:
				cluster = i
			}
		default:
			return nil, false
		}
	}
	if optionName {
		return nil, false
	}
	var letters, long string
	if missing.Errexit {
		letters, long = letters+"e", long+" -o errexit"
	}
	if missing.Nounset {
		letters, long = letters+"u", long+" -o nounset"
	}
	var edits []textedit.Edit
	tail := ""
	if cluster >= 0 {
		span, _ := tree.NodeSpan(c.Args[cluster])
		at := span.End
		if missing.Pipefail && cluster == len(c.Args)-1 {
			letters += "o pipefail"
			missing.Pipefail = false
		}
		if letters != "" {
			edits = append(edits, textedit.Edit{Start: at, End: at, NewText: letters})
		}
	} else {
		tail = long
	}
	if missing.Pipefail {
		tail += " -o pipefail"
	}
	if tail != "" {
		edits = append(edits, textedit.Edit{Start: c.Span.End, End: c.Span.End, NewText: tail})
	}
	return edits, true
}

func extendSetopt(c parser.Command, missing lint.StrictMode) ([]textedit.Edit, bool) {
	for _, arg := range c.Args {
		if lit, ok := parser.Literal(arg); !ok || strings.HasPrefix(lit, "-") || strings.HasPrefix(lit, "+") {
			return nil, false
		}
	}
	var tail string
	if missing.Errexit {
		tail += " err_exit"
	}
	if missing.Nounset {
		tail += " no_unset"
	}
	if missing.Pipefail {
		tail += " pipe_fail"
	}
	return []textedit.Edit{{Start: c.Span.End, End: c.Span.End, NewText: tail}}, true
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
