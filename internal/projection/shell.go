package projection

import (
	"cmp"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func (m *Map) ArgumentWords() []parser.Span {
	wrappers := parser.Wrappers{Programs: true}
	var out []parser.Span
	m.eachScript("", func(file *syntax.File, _ string, base int) {
		parser.Walk(file, func(n syntax.Node) bool {
			call, ok := n.(*syntax.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			commands := []*syntax.Word{call.Args[0]}
			for run := range wrappers.Invocations(call.Args) {
				if len(run.Args) > 0 {
					commands = append(commands, run.Args[0])
				}
			}
			for _, w := range call.Args {
				if !slices.Contains(commands, w) {
					out = append(out, parser.Span{Start: base + parser.Offset(w.Pos()), End: base + parser.Offset(w.End())})
				}
			}
			return true
		})
	})
	slices.SortFunc(out, func(a, b parser.Span) int { return cmp.Compare(a.Start, b.Start) })
	return out
}

func (m *Map) eachScript(mentioning string, visit func(file *syntax.File, text string, base int)) {
	parsers := map[dialect.Shell]parser.SyntaxParser{}
	for i, script := range m.scripts {
		text := m.text[script.Start:script.End]
		if !strings.Contains(text, mentioning) {
			continue
		}
		shell := m.shells[i].Shell
		p, ok := parsers[shell]
		if !ok {
			p = parser.NewSyntaxParser(shell)
			parsers[shell] = p
		}
		visit(p.Parse(text), text, script.Start)
	}
}

type Heredoc struct {
	Delimiter string
	Body      parser.Span
	Comment   string
	Stmt      *syntax.Stmt
}

const heredocOperator = "<<"

func (m *Map) Heredocs() []Heredoc {
	var out []Heredoc
	m.eachScript(heredocOperator, func(file *syntax.File, text string, base int) {
		out = append(out, heredocsOf(file, text, base)...)
	})
	return out
}

func heredocsOf(file *syntax.File, text string, base int) []Heredoc {
	var out []Heredoc
	var comments []syntax.Comment
	parser.Walk(file, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Comment:
			comments = append(comments, *n)
		case *syntax.Stmt:
			for _, r := range n.Redirs {
				if h, ok := heredocOf(r, n, text, base); ok {
					out = append(out, h)
				}
			}
		}
		return true
	})
	for i, h := range out {
		line := h.Stmt.Pos().Line()
		for _, r := range h.Stmt.Redirs {
			if r.Op == syntax.Hdoc || r.Op == syntax.DashHdoc {
				line = r.OpPos.Line()
			}
		}
		for _, c := range comments {
			if c.Pos().Line() == line {
				out[i].Comment = c.Text
			}
		}
	}
	return out
}

func heredocOf(r *syntax.Redirect, stmt *syntax.Stmt, text string, base int) (Heredoc, bool) {
	if (r.Op != syntax.Hdoc && r.Op != syntax.DashHdoc) || r.Hdoc == nil {
		return Heredoc{}, false
	}
	delimiter, _ := parser.HeredocDelimiter(r.Word)
	start := parser.Offset(r.Hdoc.Pos())
	end := max(start, textline.Start(text, parser.Offset(r.Hdoc.End())))
	if delimiter == "" || !closedBy(text[end:], delimiter, r.Op == syntax.DashHdoc) {
		return Heredoc{}, false
	}
	return Heredoc{Delimiter: delimiter, Body: parser.Span{Start: base + start, End: base + end}, Stmt: stmt}, true
}

func closedBy(rest, delimiter string, stripTabs bool) bool {
	if stripTabs {
		rest = strings.TrimLeft(rest, "\t")
	}
	after, found := strings.CutPrefix(rest, delimiter)
	return found && (after == "" || after[0] == '\n')
}

func (m *Map) Literal(start, end int) bool {
	for i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > ProjOffset(start) }); i < len(m.spans) && m.spans[i].proj < ProjOffset(end); i++ {
		if s := m.spans[i]; s.projLen > 0 && s.kind != verbatim {
			return false
		}
	}
	return true
}

func SingleQuotes(file *syntax.File) func(at int) bool {
	quotes := singleQuotes(file, 0)
	return func(at int) bool {
		_, inside := enclosing(quotes, ProjOffset(at))
		return inside
	}
}
