package actions

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type movable struct {
	name string
	stmt parser.Span
}

func movableFunction(s *site) (movable, bool) {
	decl, ok := functionHeaderAt(s)
	if !ok {
		return movable{}, false
	}
	path := s.pathAt()
	i := slices.Index(path, syntax.Node(decl))
	if i < 2 {
		return movable{}, false
	}
	if _, top := path[i-2].(*syntax.File); !top {
		return movable{}, false
	}
	defined := 0
	for _, fn := range s.Tree.Functions {
		if fn.Name == decl.Name.Value {
			defined++
		}
	}
	stmt, _ := s.Tree.NodeSpan(path[i-1])
	text := s.Tree.Text()
	if defined != 1 || !startsLine(text, stmt.Start) || strings.TrimSpace(text[stmt.End:lineEnd(text, stmt.End)]) != "" {
		return movable{}, false
	}
	return movable{name: decl.Name.Value, stmt: stmt}, true
}

func moveTargets(s *site) []string {
	if _, ok := movableFunction(s); !ok {
		return nil
	}
	var uris []string
	for _, src := range s.sources() {
		if !slices.Contains(uris, src.URI) {
			uris = append(uris, src.URI)
		}
	}
	return uris
}

func moveFunction(s *site) (Change, bool) {
	m, ok := movableFunction(s)
	if !ok {
		return Change{}, false
	}
	i := slices.IndexFunc(s.sources(), func(src Source) bool {
		_, defined := src.Tree.FunctionNamed(m.name)
		return src.URI == s.target && !defined && sourcedBefore(s.Tree, src.Command, m.stmt.Start)
	})
	if i < 0 {
		return Change{}, false
	}
	src := s.sources()[i]
	text := s.Tree.Text()
	start := parser.CommentBlockStart(text, lineStart(text, m.stmt.Start))
	end := lineEnd(text, m.stmt.End)
	if !runsIn(src.Tree.Dialect, s.Tree.Dialect, text[start:end]) {
		return Change{}, false
	}
	removeEnd := min(end+1, len(text))
	if blankLineAt(text, removeEnd) && (start == 0 || blankLineAt(text, lineStart(text, start-1))) {
		removeEnd = min(lineEnd(text, removeEnd)+1, len(text))
	}
	target := src.Tree.Text()
	insert := text[start:end] + "\n"
	if target != "" && !strings.HasSuffix(target, "\n\n") {
		insert = "\n" + insert
		if !strings.HasSuffix(target, "\n") {
			insert = "\n" + insert
		}
	}
	return Change{
		Title:  "Move " + m.name + " to " + src.Name,
		Edits:  []textedit.Edit{{Start: start, End: removeEnd}},
		Others: []FileEdits{{URI: src.URI, Edits: []textedit.Edit{{Start: len(target), End: len(target), NewText: insert}}}},
	}, true
}

func runsIn(target, from dialect.Dialect, code string) bool {
	if target == from {
		return true
	}
	tree := parser.Parse(code, target)
	if len(tree.Errors) > 0 {
		return false
	}
	return !slices.ContainsFunc(lint.Check(tree, nil, lint.Options{DialectSource: lint.DialectForced}), func(f lint.Finding) bool { return f.Code.Portability() })
}

func blankLineAt(text string, at int) bool {
	return at < len(text) && strings.TrimSpace(text[at:lineEnd(text, at)]) == ""
}

func sourcedBefore(tree *parser.Tree, cmd parser.Command, at int) bool {
	if cmd.Span.End > at {
		return false
	}
	for _, stmt := range tree.File.Stmts {
		first := stmt
		for {
			bin, ok := first.Cmd.(*syntax.BinaryCmd)
			if !ok || (bin.Op != syntax.AndStmt && bin.Op != syntax.OrStmt) {
				break
			}
			first = bin.X
		}
		call, ok := first.Cmd.(*syntax.CallExpr)
		if !ok || stmt.Background || stmt.Coprocess || first.Background {
			continue
		}
		if span, ok := tree.NodeSpan(call); ok && span == cmd.Span {
			return true
		}
	}
	return false
}
