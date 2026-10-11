package actions

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type Kind string

const (
	QuickFix         Kind = "quickfix"
	RefactorExtract  Kind = "refactor.extract"
	RefactorInline   Kind = "refactor.inline"
	RefactorRewrite  Kind = "refactor.rewrite"
	SourceFixAll     Kind = "source.fixAll.shellLsp"
	SourceStrictMode Kind = "source.strictMode"
)

type Diagnostic struct {
	Index   int
	Code    string
	Message string
	Span    parser.Span
	Fix     []textedit.Edit
}

type RefactorKind string

const (
	ExtractVariable  RefactorKind = "extractVariable"
	ExtractFunction  RefactorKind = "extractFunction"
	ToggleQuotes     RefactorKind = "toggleQuotes"
	DeclareLocal     RefactorKind = "declareLocal"
	ConvertBackticks RefactorKind = "convertBackticks"
	DoubleBrackets   RefactorKind = "doubleBrackets"
	InlineVariable   RefactorKind = "inlineVariable"
	FunctionKeyword  RefactorKind = "functionKeyword"
	StrictMode       RefactorKind = "strictMode"
	IfToCase         RefactorKind = "ifToCase"
)

type Refactor struct {
	Kind  RefactorKind `json:"kind"`
	Start int          `json:"start"`
	End   int          `json:"end"`
}

type Action struct {
	Title       string
	Kind        Kind
	Diagnostics []int
	Preferred   bool
	Edits       []textedit.Edit
	Refactor    *Refactor
}

type Document struct {
	Tree *parser.Tree
}

type Input struct {
	Document
	Selection      parser.Span
	Diagnostics    []Diagnostic
	AllDiagnostics []Diagnostic
}

type Change struct {
	Title string
	Edits []textedit.Edit
}

type refactoring struct {
	kind    RefactorKind
	action  Kind
	compute func(*site) (Change, bool)
}

var refactorings = []refactoring{
	{ExtractVariable, RefactorExtract, extractVariable},
	{ExtractFunction, RefactorExtract, extractFunction},
	{ToggleQuotes, RefactorRewrite, toggleQuotes},
	{DeclareLocal, RefactorRewrite, declareLocal},
	{ConvertBackticks, RefactorRewrite, convertBackticks},
	{DoubleBrackets, RefactorRewrite, doubleBrackets},
	{InlineVariable, RefactorInline, inlineVariable},
	{FunctionKeyword, RefactorRewrite, toggleFunctionKeyword},
	{StrictMode, SourceStrictMode, strictMode},
	{IfToCase, RefactorRewrite, ifToCase},
}

func Refactorings() []RefactorKind {
	kinds := make([]RefactorKind, len(refactorings))
	for i, r := range refactorings {
		kinds[i] = r.kind
	}
	return kinds
}

func Actions(in Input) []Action {
	var out []Action
	for _, d := range in.Diagnostics {
		out = append(out, quickFixes(in.Tree, d)...)
	}
	s := newSite(in.Document, in.Selection)
	for _, ref := range refactorings {
		change, ok := ref.compute(s)
		if !ok {
			continue
		}
		r := Refactor{Kind: ref.kind, Start: in.Selection.Start, End: in.Selection.End}
		out = append(out, Action{Title: change.Title, Kind: ref.action, Edits: change.Edits, Refactor: &r})
	}
	if fixAll := FixAll(in.AllDiagnostics); len(fixAll) > 0 {
		out = append(out, Action{Title: "Apply all automatic fixes", Kind: SourceFixAll, Edits: fixAll})
	}
	return out
}

func FixAll(diags []Diagnostic) []textedit.Edit {
	var groups [][]textedit.Edit
	for _, d := range diags {
		if len(d.Fix) > 0 {
			groups = append(groups, d.Fix)
		}
	}
	return textedit.NonOverlapping(groups)
}

func quickFixes(tree *parser.Tree, d Diagnostic) []Action {
	var out []Action
	if len(d.Fix) > 0 {
		out = append(out, Action{Title: fmt.Sprintf("Fix %s: %s", d.Code, d.Message), Kind: QuickFix, Diagnostics: []int{d.Index}, Preferred: true, Edits: d.Fix})
	}
	if d.Code == "SC2164" {
		if edit, ok := exitOnFailedCd(tree, d.Span); ok {
			out = append(out, Action{Title: "Exit when cd fails", Kind: QuickFix, Diagnostics: []int{d.Index}, Preferred: len(d.Fix) == 0, Edits: []textedit.Edit{edit}})
		}
	}
	if !strings.HasPrefix(d.Code, "SC") {
		return out
	}
	if edit, ok := disableForStatement(tree, d); ok {
		out = append(out, Action{Title: fmt.Sprintf("Ignore %s for this command", d.Code), Kind: QuickFix, Diagnostics: []int{d.Index}, Edits: []textedit.Edit{edit}})
	}
	out = append(out, Action{Title: fmt.Sprintf("Ignore %s in this file", d.Code), Kind: QuickFix, Diagnostics: []int{d.Index}, Edits: []textedit.Edit{disableForFile(tree, d.Code)}})
	return out
}

func exitOnFailedCd(tree *parser.Tree, at parser.Span) (textedit.Edit, bool) {
	for _, cmd := range tree.Commands {
		if cmd.Name == "cd" && cmd.Span.Start <= at.Start && at.Start <= cmd.Span.End {
			return textedit.Edit{Start: cmd.Span.End, End: cmd.Span.End, NewText: " || exit"}, true
		}
	}
	return textedit.Edit{}, false
}

const directivePrefix = "# shellcheck disable="

func disableForStatement(tree *parser.Tree, d Diagnostic) (textedit.Edit, bool) {
	stmt, ok := outermostStmtOnLine(tree, d.Span.Start)
	if !ok {
		return textedit.Edit{}, false
	}
	text := tree.Text()
	start := lineStart(text, stmt.Start)
	return directiveEdit(text, start, leadingSpace(text, start), d.Code), true
}

func disableForFile(tree *parser.Tree, code string) textedit.Edit {
	text := tree.Text()
	at := 0
	if strings.HasPrefix(text, "#!") {
		at = lineEnd(text, 0) + 1
		at = min(at, len(text))
	}
	return directiveEdit(text, at, "", code)
}

func directiveEdit(text string, lineAt int, indent, code string) textedit.Edit {
	if lineAt > 0 {
		prevStart := lineStart(text, lineAt-1)
		prev := text[prevStart : lineAt-1]
		if rest, ok := strings.CutPrefix(strings.TrimSpace(prev), directivePrefix); ok {
			codes := strings.Fields(rest)
			if len(codes) > 0 && !slices.Contains(strings.Split(codes[0], ","), code) {
				end := prevStart + strings.Index(prev, directivePrefix) + len(directivePrefix) + len(codes[0])
				return textedit.Edit{Start: end, End: end, NewText: "," + code}
			}
		}
	}
	if lineAt == len(text) && text != "" && !strings.HasSuffix(text, "\n") {
		return textedit.Edit{Start: lineAt, End: lineAt, NewText: "\n" + indent + directivePrefix + code + "\n"}
	}
	return textedit.Edit{Start: lineAt, End: lineAt, NewText: indent + directivePrefix + code + "\n"}
}

type site struct {
	Document
	sel    parser.Span
	path   []syntax.Node
	walked bool
}

func newSite(doc Document, sel parser.Span) *site {
	return &site{Document: doc, sel: sel}
}

func (s *site) pathAt() []syntax.Node {
	if !s.walked {
		s.path, s.walked = s.Tree.PathAt(s.sel.Start), true
	}
	return s.path
}

func Compute(doc Document, r Refactor) (Change, bool) {
	i := slices.IndexFunc(refactorings, func(ref refactoring) bool { return ref.kind == r.Kind })
	if i < 0 {
		return Change{}, false
	}
	return refactorings[i].compute(newSite(doc, parser.Span{Start: r.Start, End: r.End}))
}

func extractVariable(s *site) (Change, bool) {
	tree, sel := s.Tree, s.sel
	text := tree.Text()
	if sel.End <= sel.Start || strings.ContainsRune(text[sel.Start:sel.End], '\n') || inHeredoc(tree, sel) {
		return Change{}, false
	}
	path := s.pathAt()
	word, inQuotes, ok := selectedWord(tree, path, sel)
	if !ok {
		return Change{}, false
	}
	stmt, ok := innermostStmt(tree, path)
	if !ok || !startsLine(text, stmt.Start) {
		return Change{}, false
	}
	name := uniqueName(tree, "value")
	selected := text[sel.Start:sel.End]
	rhs, replacement := selected, `"${`+name+`}"`
	switch {
	case inQuotes:
		rhs, replacement = `"`+selected+`"`, "${"+name+"}"
	case word == sel:
		replacement = `"$` + name + `"`
	}
	lineAt := lineStart(text, stmt.Start)
	indent := leadingSpace(text, lineAt)
	return Change{Title: "Extract to variable " + name, Edits: []textedit.Edit{
		{Start: lineAt, End: lineAt, NewText: indent + name + "=" + rhs + "\n"},
		{Start: sel.Start, End: sel.End, NewText: replacement},
	}}, true
}

func inHeredoc(tree *parser.Tree, sel parser.Span) bool {
	text := tree.Text()
	for _, h := range tree.Heredocs {
		region := parser.Span{Start: h.Span.Start, End: lineEnd(text, min(h.Body.End, len(text)))}
		if sel.Start < region.End && region.Start < sel.End {
			return true
		}
	}
	return false
}

func selectedWord(tree *parser.Tree, path []syntax.Node, sel parser.Span) (parser.Span, bool, bool) {
	for i, node := range slices.Backward(path) {
		w, ok := node.(*syntax.Word)
		if !ok {
			continue
		}
		span, ok := tree.NodeSpan(w)
		if !ok || sel.End > span.End {
			return parser.Span{}, false, false
		}
		if span == sel {
			return span, false, true
		}
		if i+1 < len(path) {
			if dq, ok := path[i+1].(*syntax.DblQuoted); ok {
				dqSpan, _ := tree.NodeSpan(dq)
				return span, true, sel.Start > dqSpan.Start && sel.End < dqSpan.End && partsAligned(tree, dq.Parts, sel)
			}
		}
		return span, false, partsAligned(tree, w.Parts, sel)
	}
	return parser.Span{}, false, false
}

func partsAligned(tree *parser.Tree, parts []syntax.WordPart, sel parser.Span) bool {
	startOK, endOK := false, false
	for _, part := range parts {
		span, ok := tree.NodeSpan(part)
		if !ok {
			return false
		}
		if lit, isLit := part.(*syntax.Lit); isLit && span.Start <= sel.Start && sel.End <= span.End && lit.Value != "" {
			return !strings.ContainsAny(tree.Text()[sel.Start:sel.End], `\`)
		}
		startOK = startOK || span.Start == sel.Start
		endOK = endOK || span.End == sel.End
	}
	return startOK && endOK
}

func extractFunction(s *site) (Change, bool) {
	tree, sel := s.Tree, s.sel
	text := tree.Text()
	stmts, ok := selectedStatements(tree, sel)
	if !ok {
		return Change{}, false
	}
	first, last := stmts[0], stmts[len(stmts)-1]
	body := text[lineStart(text, first.Start):lineEnd(text, last.End)]
	if usesControlFlow(tree, parser.Span{Start: first.Start, End: last.End}) {
		return Change{}, false
	}
	name := uniqueName(tree, "extracted")
	call := name
	if usesPositional(tree, parser.Span{Start: first.Start, End: last.End}) {
		call += ` "$@"`
	}
	indent := leadingSpace(text, lineStart(text, first.Start))
	outer := topLevelStart(tree, first.Start)
	insertAt := lineStart(text, outer)
	fn := leadingSpace(text, insertAt) + name + "() {\n" + reindent(body, leadingSpace(text, insertAt)+"\t") + leadingSpace(text, insertAt) + "}\n\n"
	return Change{Title: "Extract to function " + name, Edits: []textedit.Edit{
		{Start: insertAt, End: insertAt, NewText: fn},
		{Start: lineStart(text, first.Start), End: lineEnd(text, last.End), NewText: indent + call},
	}}, true
}

func selectedStatements(tree *parser.Tree, sel parser.Span) ([]parser.Span, bool) {
	text := tree.Text()
	trimmed := trimSpan(text, sel)
	if trimmed.End <= trimmed.Start {
		return nil, false
	}
	var stmts []parser.Span
	var parent syntax.Node
	var stack []syntax.Node
	parser.Walk(tree.File, func(node syntax.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if span, ok := tree.NodeSpan(node); ok && (span.End < trimmed.Start || span.Start > trimmed.End) {
			if _, isFile := node.(*syntax.File); !isFile {
				return false
			}
		}
		if stmt, ok := node.(*syntax.Stmt); ok {
			if span, ok := tree.NodeSpan(stmt); ok && trimmed.Start <= span.Start && span.End <= trimmed.End {
				owner := stack[len(stack)-1]
				if parent == nil {
					parent = owner
				}
				if owner == parent {
					stmts = append(stmts, span)
				}
				return false
			}
		}
		stack = append(stack, node)
		return true
	})
	if len(stmts) == 0 || stmts[0].Start != trimmed.Start || stmts[len(stmts)-1].End != trimmed.End {
		return nil, false
	}
	if !startsLine(text, stmts[0].Start) || strings.TrimSpace(text[stmts[len(stmts)-1].End:lineEnd(text, stmts[len(stmts)-1].End)]) != "" {
		return nil, false
	}
	return stmts, true
}

func usesControlFlow(tree *parser.Tree, span parser.Span) bool {
	for _, cmd := range tree.Commands {
		if span.Start <= cmd.Span.Start && cmd.Span.End <= span.End {
			switch cmd.Name {
			case "return", "break", "continue", "local", "declare", "typeset", "shift":
				return true
			}
		}
	}
	return false
}

func usesPositional(tree *parser.Tree, span parser.Span) bool {
	found := false
	parser.Walk(tree.File, func(node syntax.Node) bool {
		if node == nil {
			return !found
		}
		if nodeSpan, ok := tree.NodeSpan(node); ok && (nodeSpan.End < span.Start || nodeSpan.Start > span.End) {
			_, isFile := node.(*syntax.File)
			return isFile
		}
		exp, ok := node.(*syntax.ParamExp)
		if !ok || exp.Param == nil || !exp.Param.ValuePos.IsValid() {
			return !found
		}
		at := parser.Offset(exp.Param.ValuePos)
		name := exp.Param.Value
		if span.Start <= at && at < span.End && (name == "@" || name == "*" || name == "#" || (name != "0" && parser.IsDigits(name))) {
			found = true
		}
		return !found
	})
	return found
}

func topLevelStart(tree *parser.Tree, offset int) int {
	for _, stmt := range tree.File.Stmts {
		if span, ok := tree.NodeSpan(stmt); ok && span.Start <= offset && offset < span.End {
			return span.Start
		}
	}
	return offset
}

func toggleQuotes(s *site) (Change, bool) {
	for _, node := range slices.Backward(s.pathAt()) {
		switch q := node.(type) {
		case *syntax.SglQuoted:
			if q.Dollar || !q.Right.IsValid() {
				return Change{}, false
			}
			span, _ := s.Tree.NodeSpan(q)
			return Change{Title: "Convert to double quotes", Edits: []textedit.Edit{{Start: span.Start, End: span.End, NewText: `"` + backslashEscaped(q.Value, doubleQuoteSpecials) + `"`}}}, true
		case *syntax.DblQuoted:
			if q.Dollar || !q.Right.IsValid() {
				return Change{}, false
			}
			value, ok := parser.Literal(&syntax.Word{Parts: []syntax.WordPart{q}})
			if !ok {
				return Change{}, false
			}
			span, _ := s.Tree.NodeSpan(q)
			return Change{Title: "Convert to single quotes", Edits: []textedit.Edit{{Start: span.Start, End: span.End, NewText: singleQuoted(value)}}}, true
		}
	}
	return Change{}, false
}

func singleQuoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const (
	doubleQuoteSpecials = "\\\"$`"
	heredocSpecials     = "\\$`"
)

func backslashEscaped(s, specials string) string {
	var sb strings.Builder
	for _, r := range s {
		if strings.ContainsRune(specials, r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func declareLocal(s *site) (Change, bool) {
	tree, at := s.Tree, s.sel.Start
	if !tree.Dialect.Features().Local {
		return Change{}, false
	}
	for _, def := range tree.Definitions {
		if !def.NameSpan.Covers(at) || def.Kind != parser.DefAssign || def.Local || def.Within == parser.FileScope {
			continue
		}
		if !startsLine(tree.Text(), def.Span.Start) {
			return Change{}, false
		}
		return Change{Title: fmt.Sprintf("Declare %s as local", def.Name), Edits: []textedit.Edit{{Start: def.Span.Start, End: def.Span.Start, NewText: "local "}}}, true
	}
	return Change{}, false
}

func innermostStmt(tree *parser.Tree, path []syntax.Node) (parser.Span, bool) {
	for _, node := range slices.Backward(path) {
		if stmt, ok := node.(*syntax.Stmt); ok {
			return tree.NodeSpan(stmt)
		}
	}
	return parser.Span{}, false
}

func outermostStmtOnLine(tree *parser.Tree, offset int) (parser.Span, bool) {
	text := tree.Text()
	line := lineStart(text, offset)
	var best parser.Span
	found := false
	for _, node := range tree.PathAt(offset) {
		stmt, ok := node.(*syntax.Stmt)
		if !ok {
			continue
		}
		span, ok := tree.NodeSpan(stmt)
		if ok && lineStart(text, span.Start) == line {
			return span, true
		}
		if ok {
			best, found = span, true
		}
	}
	return best, found
}

func uniqueName(tree *parser.Tree, base string) string {
	taken := map[string]bool{}
	for _, d := range tree.Definitions {
		taken[d.Name] = true
	}
	for _, f := range tree.Functions {
		taken[f.Name] = true
	}
	for _, c := range tree.Commands {
		taken[c.Name] = true
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	return name
}

func reindent(body, indent string) string {
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	common := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if common < 0 || n < common {
			common = n
		}
	}
	var sb strings.Builder
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			sb.WriteString(indent)
			sb.WriteString(l[max(common, 0):])
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func trimSpan(text string, s parser.Span) parser.Span {
	for s.Start < s.End && strings.ContainsRune(" \t\n", rune(text[s.Start])) {
		s.Start++
	}
	for s.End > s.Start && strings.ContainsRune(" \t\n;", rune(text[s.End-1])) {
		s.End--
	}
	return s
}

func lineStart(text string, offset int) int {
	return strings.LastIndexByte(text[:offset], '\n') + 1
}

func lineEnd(text string, offset int) int {
	if i := strings.IndexByte(text[offset:], '\n'); i >= 0 {
		return offset + i
	}
	return len(text)
}

func startsLine(text string, offset int) bool {
	return strings.TrimSpace(text[lineStart(text, offset):offset]) == ""
}

func leadingSpace(text string, lineAt int) string {
	end := lineAt
	for end < len(text) && (text[end] == ' ' || text[end] == '\t') {
		end++
	}
	return text[lineAt:end]
}
