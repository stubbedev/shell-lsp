package completion

import (
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type position uint8

const (
	positionNone position = iota
	positionVariable
	positionCommand
	positionArgument
	positionPath
	positionExpansionOperator
	positionParameterFlags
	positionArrayKey
	positionGlobQualifier
	positionGlobFlags
	positionCasePattern
)

type site struct {
	position position
	prefix   string
	value    string
	replace  parser.Span
	command  string
	previous []string
	name     string
	used     []string
	typed    string
	wordEnd  int
}

const placeholder = "shellLspCursor"

func analyze(tree *parser.Tree, offset int) site {
	if inComment(tree, offset) {
		return site{}
	}
	if doc, ok := heredocAt(tree, offset); ok {
		if doc.Quoted {
			return site{}
		}
		return heredocSite(tree, offset)
	}
	for _, start := range regions(tree, offset) {
		if st, ok := siteFrom(tree, parser.Span{Start: start, End: offset}); ok {
			return st
		}
	}
	return site{}
}

func heredocSite(tree *parser.Tree, offset int) site {
	body, err := tree.ParseHeredocFragment(parser.Span{Start: textline.Start(tree.Text(), offset), End: offset}, placeholder)
	if err != nil {
		return site{}
	}
	return newProbe(tree, body, offset).site()
}

func regions(tree *parser.Tree, at int) []int {
	var containing, last *syntax.Stmt
	top := tree.File.Stmts
	if i := sort.Search(len(top), func(i int) bool { return parser.Offset(top[i].Pos()) > at }); i > 0 {
		parser.Walk(top[i-1], func(n syntax.Node) bool {
			if n == nil || !n.Pos().IsValid() || parser.Offset(n.Pos()) > at {
				return false
			}
			inside := !n.End().IsValid() || at <= parser.Offset(n.End())
			if stmt, ok := n.(*syntax.Stmt); ok {
				last = stmt
				if inside {
					containing = stmt
				}
			}
			return inside
		})
	}
	var starts []int
	add := func(start int) {
		if !slices.Contains(starts, start) {
			starts = append(starts, start)
		}
	}
	switch {
	case last == nil:
		add(0)
	case last == containing || continuesAfterBlanks(last):
		add(parser.Offset(last.Pos()))
	default:
		add(parser.Offset(last.End()))
	}
	if containing != nil {
		add(parser.Offset(containing.Pos()))
	}
	add(logicalLineStart(tree.Text(), at))
	return starts
}

func continuesAfterBlanks(stmt *syntax.Stmt) bool {
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr, *syntax.DeclClause, *syntax.LetClause:
		return true
	case *syntax.BinaryCmd:
		return continuesAfterBlanks(cmd.Y)
	case *syntax.TimeClause:
		return cmd.Stmt != nil && continuesAfterBlanks(cmd.Stmt)
	case *syntax.CoprocClause:
		return continuesAfterBlanks(cmd.Stmt)
	}
	return false
}

func siteFrom(tree *parser.Tree, span parser.Span) (site, bool) {
	file, err := tree.ParseFragment(span, placeholder)
	if err == nil {
		return newProbe(tree, file, span.End).site(), true
	}
	if st, ok := casePattern(tree, span); ok {
		return st, true
	}
	return testOperator(tree, span, err)
}

type probe struct {
	tree   *parser.Tree
	text   string
	cursor int
	path   []syntax.Node
}

func newProbe(tree *parser.Tree, root syntax.Node, cursor int) probe {
	return probe{tree: tree, text: tree.Text(), cursor: cursor, path: locate(root, cursor, cursor+len(placeholder))}
}

func locate(root syntax.Node, from, to int) []syntax.Node {
	var path []syntax.Node
	parser.Walk(root, func(n syntax.Node) bool {
		switch n.(type) {
		case nil:
			return false
		case *syntax.File:
			return true
		}
		pos, end := n.Pos(), n.End()
		if !pos.IsValid() || parser.Offset(pos) > from || end.IsValid() && parser.Offset(end) < to {
			return false
		}
		path = append(path, n)
		return true
	})
	return path
}

func (p probe) site() site {
	for i, node := range slices.Backward(p.path) {
		var child syntax.Node
		if i+1 < len(p.path) {
			child = p.path[i+1]
		}
		switch n := node.(type) {
		case *syntax.Comment:
			return site{}
		case *syntax.ParamExp:
			return p.expansion(n, child)
		case *syntax.Word:
			if i == 0 || child == nil {
				return site{}
			}
			if st, decided := p.word(i, n, p.path[i-1]); decided {
				return st
			}
		case *syntax.Assign:
			if child == n.Name {
				return p.declaration(i, n)
			}
		case *syntax.WordIter:
			return site{}
		}
	}
	return site{}
}

func (p probe) word(i int, w *syntax.Word, parent syntax.Node) (site, bool) {
	if lit, ok := p.path[i+1].(*syntax.Lit); ok && p.tree.Dialect == dialect.Zsh && strings.HasPrefix(lit.Value, "(") {
		return p.group(w, lit), true
	}
	if parser.IsArithmetic(parent) {
		return p.arithmetic(w), true
	}
	switch n := parent.(type) {
	case *syntax.ParamExp:
		return site{}, false
	case *syntax.CallExpr:
		k := slices.Index(n.Args, w)
		if k < 0 {
			return site{}, true
		}
		st, ok := p.commandWord(i, w)
		switch {
		case !ok:
			return site{}, true
		case k == 0:
			st.position = positionCommand
			return st, true
		}
		st.position, st.command, st.previous = positionArgument, p.nodeText(n.Args[0]), p.texts(n.Args[1:k])
		return st, true
	case *syntax.Assign:
		if !n.Naked {
			return site{}, true
		}
		return p.declaration(i-1, n), true
	case *syntax.Redirect:
		if n.Word != w || parser.IsHeredoc(n.Op) || n.Op == syntax.DplIn || n.Op == syntax.DplOut {
			return site{}, true
		}
		return p.pathWord(i, w), true
	case *syntax.WordIter:
		if !slices.Contains(n.Items, w) {
			return site{}, true
		}
		return p.pathWord(i, w), true
	case *syntax.TestClause, *syntax.BinaryTest, *syntax.UnaryTest, *syntax.ParenTest:
		return p.testWord(i, w), true
	}
	return site{}, true
}

func (p probe) commandWord(i int, w *syntax.Word) (site, bool) {
	for _, n := range p.path[i+1:] {
		switch n.(type) {
		case *syntax.SglQuoted, *syntax.DblQuoted:
			if n != syntax.Node(w.Parts[0]) {
				return site{}, false
			}
		}
	}
	start := parser.Offset(w.Pos())
	text, _ := parser.Text(w, "_")
	value, _, _ := strings.Cut(text, placeholder)
	return site{prefix: p.text[start:p.cursor], value: value, replace: parser.Span{Start: start, End: p.cursor}}, true
}

func (p probe) pathWord(i int, w *syntax.Word) site {
	st, ok := p.commandWord(i, w)
	if !ok {
		return site{}
	}
	st.position = positionPath
	return st
}

func (p probe) declaration(i int, a *syntax.Assign) site {
	if i == 0 || !a.Naked {
		return site{}
	}
	decl, ok := p.path[i-1].(*syntax.DeclClause)
	if !ok {
		return site{}
	}
	start := parser.Offset(a.Pos())
	k := slices.Index(decl.Args, a)
	previous := make([]string, 0, k)
	for _, arg := range decl.Args[:k] {
		previous = append(previous, p.nodeText(arg))
	}
	return site{
		position: positionArgument,
		prefix:   p.text[start:p.cursor],
		replace:  parser.Span{Start: start, End: p.cursor},
		command:  decl.Variant.Value,
		previous: previous,
	}
}

func (p probe) testWord(i int, w *syntax.Word) site {
	st, ok := p.commandWord(i, w)
	if !ok {
		return site{}
	}
	for _, n := range slices.Backward(p.path[:i]) {
		if clause, ok := n.(*syntax.TestClause); ok {
			st.position, st.command, st.previous = positionArgument, testCommand, p.testWords(clause.X, parser.Offset(w.Pos()), nil)
			return st
		}
	}
	return site{}
}

const testCommand = "[["

func (p probe) testWords(x syntax.TestExpr, before int, out []string) []string {
	if x == nil || !x.Pos().IsValid() || parser.Offset(x.Pos()) >= before {
		return out
	}
	switch n := x.(type) {
	case *syntax.BinaryTest:
		out = p.testWords(n.X, before, out)
		if parser.Offset(n.OpPos) < before {
			out = append(out, n.Op.String())
		}
		return p.testWords(n.Y, before, out)
	case *syntax.UnaryTest:
		return p.testWords(n.X, before, append(out, n.Op.String()))
	case *syntax.ParenTest:
		out = p.testWords(n.X, before, append(out, "("))
		if n.Rparen.IsValid() && parser.Offset(n.Rparen) < before {
			out = append(out, ")")
		}
		return out
	}
	return append(out, p.nodeText(x))
}

func (p probe) arithmetic(w *syntax.Word) site {
	if len(w.Parts) != 1 {
		return site{}
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return site{}
	}
	start := parser.Offset(lit.Pos())
	return site{position: positionVariable, prefix: p.text[start:p.cursor], replace: parser.Span{Start: start, End: p.cursor}}
}

func (p probe) expansion(pe *syntax.ParamExp, child syntax.Node) site {
	name := ""
	if pe.Param != nil {
		name = pe.Param.Value
	}
	switch {
	case child == nil:
		if len(pe.Modifiers) > 0 && p.startsAtCursor(pe.Modifiers[0]) {
			return p.operator(name, ":")
		}
	case child == pe.Param:
		start := parser.Offset(pe.Param.Pos())
		return site{position: positionVariable, prefix: p.text[start:p.cursor], replace: parser.Span{Start: start, End: p.cursor}}
	case pe.Flags != nil && child == pe.Flags:
		return site{position: positionParameterFlags, typed: p.text[parser.Offset(pe.Flags.Pos()):p.cursor], replace: parser.Span{Start: p.cursor, End: p.cursor}}
	case child == pe.Index:
		return p.key(name, pe.Index)
	case pe.Exp != nil && child == pe.Exp.Word:
		if p.startsAtCursor(pe.Exp.Word) {
			return p.operator(name, pe.Exp.Op.String())
		}
	case pe.Slice != nil && child == pe.Slice.Offset:
		if p.startsAtCursor(pe.Slice.Offset) {
			return p.operator(name, ":")
		}
		return p.arithmeticOperand(pe.Slice.Offset)
	case pe.Slice != nil && child == pe.Slice.Length:
		return p.arithmeticOperand(pe.Slice.Length)
	case pe.Repl != nil && child == pe.Repl.Orig:
		return p.replacement(name, pe.Repl)
	}
	return site{}
}

func (p probe) startsAtCursor(n syntax.Node) bool {
	return n.Pos().IsValid() && parser.Offset(n.Pos()) == p.cursor
}

func (p probe) operator(name, op string) site {
	return site{position: positionExpansionOperator, name: name, prefix: op, replace: parser.Span{Start: p.cursor - len(op), End: p.cursor}}
}

func (p probe) replacement(name string, repl *syntax.Replace) site {
	op := "/"
	switch {
	case repl.All:
		op = "//"
	case repl.Whole:
		op = ":/"
	}
	switch start := parser.Offset(repl.Orig.Pos()); {
	case start == p.cursor:
		return p.operator(name, op)
	case op == "/" && start == p.cursor-1 && strings.IndexByte("#%", p.text[start]) >= 0:
		return p.operator(name, op+p.text[start:p.cursor])
	}
	return site{}
}

func (p probe) arithmeticOperand(x syntax.ArithmExpr) site {
	w, ok := x.(*syntax.Word)
	if !ok {
		return site{}
	}
	return p.arithmetic(w)
}

func (p probe) key(name string, index syntax.ArithmExpr) site {
	w, ok := index.(*syntax.Word)
	if !ok || name == "" || len(w.Parts) != 1 {
		return site{}
	}
	part := w.Parts[0]
	if dq, ok := part.(*syntax.DblQuoted); ok && len(dq.Parts) == 1 {
		part = dq.Parts[0]
	}
	lit, ok := part.(*syntax.Lit)
	if !ok {
		return site{}
	}
	start := parser.Offset(lit.Pos())
	return site{position: positionArrayKey, name: name, prefix: p.text[start:p.cursor], replace: parser.Span{Start: start, End: p.cursor}}
}

func (p probe) group(w *syntax.Word, lit *syntax.Lit) site {
	inner := p.text[parser.Offset(lit.Pos())+1 : p.cursor]
	st := site{replace: parser.Span{Start: p.cursor, End: p.cursor}}
	switch {
	case strings.HasPrefix(inner, "#q"):
		st.position, st.typed = positionGlobQualifier, inner[2:]
	case strings.HasPrefix(inner, "#"):
		st.position, st.typed = positionGlobFlags, inner[1:]
	case parser.HasGlob(w.Parts[:slices.Index(w.Parts, syntax.WordPart(lit))], true):
		st.position, st.typed = positionGlobQualifier, inner
	default:
		return site{}
	}
	if !parser.QualifierText(st.typed) {
		return site{}
	}
	return st
}

func (p probe) nodeText(n syntax.Node) string {
	span, ok := p.tree.NodeSpan(n)
	if !ok {
		return ""
	}
	return p.text[span.Start:min(span.End, p.cursor)]
}

func (p probe) texts(words []*syntax.Word) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, p.nodeText(w))
	}
	return out
}

func casePattern(tree *parser.Tree, span parser.Span) (site, bool) {
	file, err := tree.ParseFragment(span, placeholder+")")
	if err != nil {
		return site{}, false
	}
	var clause *syntax.CaseClause
	var pattern *syntax.Word
	syntax.Walk(file, func(n syntax.Node) bool {
		c, ok := n.(*syntax.CaseClause)
		if !ok {
			return true
		}
		for _, item := range c.Items {
			for _, w := range item.Patterns {
				if value, ok := parser.Literal(w); ok && strings.HasSuffix(value, placeholder) && parser.Offset(w.End()) == span.End+len(placeholder) {
					clause, pattern = c, w
				}
			}
		}
		return true
	})
	if clause == nil {
		return site{}, false
	}
	pe, ok := parser.SoleParam(clause.Word)
	if !ok || !parser.IsPlainParam(pe) {
		return site{}, false
	}
	var used []string
	for _, item := range clause.Items {
		for _, w := range item.Patterns {
			if value, ok := parser.Literal(w); ok && w != pattern {
				used = append(used, value)
			}
		}
	}
	start := parser.Offset(pattern.Pos())
	return site{
		position: positionCasePattern,
		prefix:   tree.Text()[start:span.End],
		replace:  parser.Span{Start: start, End: span.End},
		name:     pe.Param.Value,
		used:     used,
	}, true
}

func testOperator(tree *parser.Tree, span parser.Span, err error) (site, bool) {
	parseErr, ok := errors.AsType[syntax.ParseError](err)
	if !ok || !parseErr.Pos.IsValid() {
		return site{}, false
	}
	at := parser.Offset(parseErr.Pos)
	if at < span.Start || at > span.End {
		return site{}, false
	}
	file, err := tree.ParseFragment(parser.Span{Start: span.Start, End: at}, "")
	if err != nil {
		return site{}, false
	}
	p := probe{tree: tree, text: tree.Text(), cursor: span.End, path: locate(file, at, at)}
	for _, n := range slices.Backward(p.path) {
		if clause, ok := n.(*syntax.TestClause); ok {
			return site{
				position: positionArgument,
				prefix:   p.text[at:span.End],
				replace:  parser.Span{Start: at, End: span.End},
				command:  testCommand,
				previous: p.testWords(clause.X, at, nil),
			}, true
		}
	}
	return site{}, false
}

func tokenEnd(tree *parser.Tree, st site) int {
	at := st.replace.End
	switch st.position {
	case positionNone, positionParameterFlags, positionGlobQualifier, positionGlobFlags:
		return at
	case positionVariable, positionCommand, positionArgument, positionPath, positionExpansionOperator, positionArrayKey, positionCasePattern:
	}
	for _, located := range slices.Backward(tree.PathAt(at)) {
		switch n := located.Node.(type) {
		case *syntax.ParamExp:
			if end, ok := expansionEnd(tree, n, st.position, at); ok {
				return end
			}
		case *syntax.Lit:
			if st.position != positionVariable && st.position != positionExpansionOperator && st.position != positionArrayKey {
				return located.Span.End
			}
		}
	}
	return at
}

func expansionEnd(tree *parser.Tree, pe *syntax.ParamExp, position position, at int) (int, bool) {
	partEnd := func(n syntax.Node) (int, bool) {
		span, ok := tree.NodeSpan(n)
		return span.End, ok && span.Touches(at)
	}
	switch {
	case position == positionVariable && pe.Param != nil:
		return partEnd(pe.Param)
	case position == positionArrayKey && pe.Index != nil:
		return partEnd(pe.Index)
	case position == positionExpansionOperator && pe.Exp != nil && pe.Exp.Word != nil:
		return max(at, parser.Offset(pe.Exp.Word.Pos())), true
	case position == positionExpansionOperator && pe.Exp != nil && pe.Rbrace.IsValid():
		return max(at, parser.Offset(pe.Rbrace)), true
	}
	return 0, false
}

func inComment(tree *parser.Tree, offset int) bool {
	for _, c := range tree.Comments {
		if c.Span.HasByteBefore(offset) {
			return true
		}
	}
	return false
}

func heredocAt(tree *parser.Tree, offset int) (parser.Heredoc, bool) {
	for _, h := range tree.Heredocs {
		if h.Body.HasByteAt(offset) {
			return h, true
		}
	}
	return parser.Heredoc{}, false
}

func logicalLineStart(text string, offset int) int {
	start := textline.Start(text, offset)
	for start >= 2 && text[start-2] == '\\' {
		start = textline.Start(text, start-1)
	}
	return start
}

func CommandAt(tree *parser.Tree, offset int) (string, int, bool) {
	st := analyze(tree, offset)
	if st.position != positionArgument {
		return "", 0, false
	}
	return st.command, len(st.previous), true
}
