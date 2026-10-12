package structure

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type SymbolKind uint8

const (
	SymbolFile SymbolKind = iota
	SymbolFunction
	SymbolVariable
	SymbolArray
	SymbolConstant
	SymbolHeredoc
	SymbolTest
)

var batsHooks = map[string]string{
	"setup":         "bats hook, runs before each test",
	"teardown":      "bats hook, runs after each test",
	"setup_file":    "bats hook, runs once before the file's tests",
	"teardown_file": "bats hook, runs once after the file's tests",
}

type Symbol struct {
	Name      string
	Detail    string
	Kind      SymbolKind
	Span      parser.Span
	Selection parser.Span
	Children  []Symbol
}

func Symbols(tree *parser.Tree) []Symbol {
	byScope := map[parser.ScopeID][]Symbol{}
	type scopedName struct {
		scope parser.ScopeID
		name  string
	}
	seen := make(map[scopedName]bool, len(tree.Definitions))
	for _, def := range tree.Definitions {
		key := scopedName{scope: def.Within, name: def.Name}
		if seen[key] || def.Kind == parser.DefCommandEnv {
			continue
		}
		seen[key] = true
		byScope[def.Within] = append(byScope[def.Within], variableSymbol(def))
	}
	for _, src := range sources(tree) {
		scope := tree.ScopeAt(src.Span.Start)
		byScope[scope] = append(byScope[scope], src)
	}
	for _, h := range tree.Heredocs {
		sym := Symbol{Name: "<<" + h.Delimiter, Detail: "heredoc", Kind: SymbolHeredoc, Span: parser.Span{Start: h.Span.Start, End: max(h.Body.End, h.Span.End)}, Selection: h.Span}
		scope := tree.ScopeAt(h.Span.Start)
		byScope[scope] = append(byScope[scope], sym)
	}
	functionsByScope := map[parser.ScopeID][]parser.Function{}
	for _, fn := range tree.Functions {
		functionsByScope[fn.Within] = append(functionsByScope[fn.Within], fn)
	}
	var build func(scope parser.ScopeID) []Symbol
	build = func(scope parser.ScopeID) []Symbol {
		out := slices.Clone(byScope[scope])
		for _, fn := range functionsByScope[scope] {
			out = append(out, Symbol{
				Name:      fn.Name,
				Detail:    functionDetail(tree, fn),
				Kind:      SymbolFunction,
				Span:      fn.Span,
				Selection: fn.NameSpan,
				Children:  build(fn.Body),
			})
		}
		slices.SortStableFunc(out, func(a, b Symbol) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
		return out
	}
	top := build(parser.FileScope)
	if len(tree.Tests) == 0 {
		return top
	}
	return nestInTests(top, tree.Tests)
}

func functionDetail(tree *parser.Tree, fn parser.Function) string {
	if hook, ok := batsHooks[fn.Name]; ok && tree.Bats() && fn.Within == parser.FileScope {
		return hook
	}
	return "function"
}

func nestInTests(top []Symbol, tests []parser.Test) []Symbol {
	out := make([]Symbol, 0, len(top)+len(tests))
	next := 0
	for _, test := range tests {
		for next < len(top) && top[next].Span.Start < test.Span.Start {
			out = append(out, top[next])
			next++
		}
		sym := Symbol{Name: test.Name, Detail: "test", Kind: SymbolTest, Span: test.Span, Selection: test.NameSpan}
		for next < len(top) && top[next].Span.End <= test.Span.End {
			sym.Children = append(sym.Children, top[next])
			next++
		}
		out = append(out, sym)
	}
	return append(out, top[next:]...)
}

func variableSymbol(def parser.Definition) Symbol {
	kind, detail := SymbolVariable, "variable"
	switch {
	case def.Attrs.Has(parser.AttrAssoc):
		kind, detail = SymbolArray, "associative array"
	case def.Attrs.Has(parser.AttrArray):
		kind, detail = SymbolArray, "array"
	case def.Attrs.Has(parser.AttrReadonly):
		kind, detail = SymbolConstant, "read-only"
	}
	if def.Local {
		detail = "local " + detail
	}
	if def.Attrs.Has(parser.AttrExport) {
		detail = "exported " + detail
	}
	return Symbol{Name: def.Name, Detail: detail, Kind: kind, Span: def.Span, Selection: def.NameSpan}
}

func sources(tree *parser.Tree) []Symbol {
	var out []Symbol
	for _, cmd := range tree.Commands {
		word, ok := cmd.SourcedFile()
		if !ok {
			continue
		}
		arg, ok := tree.NodeSpan(word)
		if !ok {
			continue
		}
		out = append(out, Symbol{Name: tree.Text()[arg.Start:arg.End], Detail: cmd.Name, Kind: SymbolFile, Span: cmd.Span, Selection: arg})
	}
	return out
}

type FoldKind uint8

const (
	FoldBlock FoldKind = iota
	FoldRegion
	FoldComment
	FoldImports
)

type Fold struct {
	StartLine int
	EndLine   int
	Kind      FoldKind
}

func Folds(tree *parser.Tree, lineOf func(offset int) int) []Fold {
	var folds []Fold
	add := func(start, end int) {
		if end > start {
			folds = append(folds, Fold{StartLine: start, EndLine: end, Kind: FoldBlock})
		}
	}
	block := func(open, close syntax.Pos) {
		if open.IsValid() && close.IsValid() {
			add(lineOf(parser.Offset(open)), lineOf(parser.Offset(close))-1)
		}
	}
	parser.Walk(tree.File, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.IfClause:
			block(n.Position, ifBranchEnd(n))
		case *syntax.WhileClause:
			block(n.WhilePos, n.DonePos)
		case *syntax.ForClause:
			block(n.ForPos, n.DonePos)
		case *syntax.RepeatClause:
			block(n.RepeatPos, n.DonePos)
		case *syntax.CaseClause:
			block(n.Case, n.Esac)
		case *syntax.CaseItem:
			if span, ok := tree.NodeSpan(n); ok {
				add(lineOf(span.Start), lineOf(span.End))
			}
		case *syntax.Block:
			block(n.Lbrace, n.Rbrace)
		case *syntax.Subshell:
			block(n.Lparen, n.Rparen)
		case *syntax.CmdSubst:
			block(n.Left, n.Right)
		case *syntax.ArrayExpr:
			block(n.Lparen, n.Rparen)
		}
		return true
	})
	for _, h := range tree.Heredocs {
		add(lineOf(h.Span.Start), lineOf(h.Body.End))
	}
	folds = append(folds, commentFolds(tree, lineOf)...)
	folds = append(folds, importFolds(tree, lineOf)...)
	slices.SortFunc(folds, func(a, b Fold) int {
		return cmp.Or(cmp.Compare(a.StartLine, b.StartLine), cmp.Compare(a.EndLine, b.EndLine), cmp.Compare(a.Kind, b.Kind))
	})
	return slices.CompactFunc(folds, func(a, b Fold) bool { return a.StartLine == b.StartLine && a.EndLine == b.EndLine })
}

func commentFolds(tree *parser.Tree, lineOf func(int) int) []Fold {
	var folds []Fold
	var regions []int
	runStart, runEnd := -1, -1
	flush := func() {
		if runEnd > runStart && runStart >= 0 {
			folds = append(folds, Fold{StartLine: runStart, EndLine: runEnd, Kind: FoldComment})
		}
		runStart, runEnd = -1, -1
	}
	text := tree.Text()
	for _, c := range tree.Comments {
		line := lineOf(c.Span.Start)
		marker := strings.TrimSpace(c.Text)
		switch {
		case strings.HasPrefix(marker, "region") || strings.HasPrefix(marker, "#region"):
			regions = append(regions, line)
		case (strings.HasPrefix(marker, "endregion") || strings.HasPrefix(marker, "#endregion")) && len(regions) > 0:
			start := regions[len(regions)-1]
			regions = regions[:len(regions)-1]
			if line > start {
				folds = append(folds, Fold{StartLine: start, EndLine: line, Kind: FoldRegion})
			}
		}
		lineStart := strings.LastIndexByte(text[:c.Span.Start], '\n') + 1
		if strings.TrimSpace(text[lineStart:c.Span.Start]) != "" || strings.HasPrefix(text[c.Span.Start:], "#!") {
			flush()
			continue
		}
		if runStart >= 0 && line == runEnd+1 {
			runEnd = line
			continue
		}
		flush()
		runStart, runEnd = line, line
	}
	flush()
	return folds
}

func importFolds(tree *parser.Tree, lineOf func(int) int) []Fold {
	var folds []Fold
	runStart, runEnd := -1, -1
	flush := func() {
		if runEnd > runStart && runStart >= 0 {
			folds = append(folds, Fold{StartLine: runStart, EndLine: runEnd, Kind: FoldImports})
		}
	}
	for _, src := range sources(tree) {
		line := lineOf(src.Span.Start)
		if runStart >= 0 && line == runEnd+1 {
			runEnd = line
			continue
		}
		flush()
		runStart, runEnd = line, line
	}
	flush()
	return folds
}

func Matches(query, name string) bool {
	query, name = strings.ToLower(query), strings.ToLower(name)
	i := 0
	for j := 0; i < len(query) && j < len(name); j++ {
		if query[i] == name[j] {
			i++
		}
	}
	return i == len(query)
}

func SelectionPath(tree *parser.Tree, offset int) []parser.Span {
	var spans []parser.Span
	for _, located := range slices.Backward(tree.PathAt(offset)) {
		span := located.Span
		if n := len(spans); n > 0 && (span == spans[n-1] || span.Start > spans[n-1].Start || span.End < spans[n-1].End) {
			continue
		}
		spans = append(spans, span)
	}
	if whole := (parser.Span{Start: 0, End: len(tree.Text())}); len(spans) == 0 || spans[len(spans)-1] != whole {
		spans = append(spans, whole)
	}
	return spans
}

func LinkedDelimiters(tree *parser.Tree, offset int) ([]parser.Span, bool) {
	text := tree.Text()
	for _, h := range tree.Heredocs {
		if h.Delimiter == "" || h.Body.End >= len(text) {
			continue
		}
		opening, ok := delimiterSpan(text, h)
		if !ok {
			continue
		}
		lineEnd := strings.IndexByte(text[h.Body.End:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text) - h.Body.End
		}
		closingLine := text[h.Body.End : h.Body.End+lineEnd]
		indent := len(closingLine) - len(strings.TrimLeft(closingLine, "\t"))
		closing := parser.Span{Start: h.Body.End + indent, End: h.Body.End + indent + len(h.Delimiter)}
		if text[closing.Start:closing.End] != h.Delimiter {
			continue
		}
		if opening.Touches(offset) || closing.Touches(offset) {
			return []parser.Span{opening, closing}, true
		}
	}
	return nil, false
}

func delimiterSpan(text string, h parser.Heredoc) (parser.Span, bool) {
	word := text[h.Span.Start:h.Span.End]
	at := strings.Index(word, h.Delimiter)
	if at < 0 || strings.Count(word, h.Delimiter) != 1 {
		return parser.Span{}, false
	}
	start := h.Span.Start + at
	return parser.Span{Start: start, End: start + len(h.Delimiter)}, true
}

func ifBranchEnd(n *syntax.IfClause) syntax.Pos {
	switch {
	case n.Braces:
		return n.Rbrace
	case !n.FiPos.IsValid() && n.Else != nil:
		return n.Else.Position
	}
	return n.FiPos
}
