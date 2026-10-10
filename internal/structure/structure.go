package structure

import (
	"cmp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

type SymbolKind uint8

const (
	SymbolFile SymbolKind = iota
	SymbolFunction
	SymbolVariable
	SymbolArray
	SymbolConstant
	SymbolHeredoc
)

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
	seen := map[parser.ScopeID]map[string]bool{}
	for _, def := range tree.Definitions {
		scope := def.Within
		if seen[scope] == nil {
			seen[scope] = map[string]bool{}
		}
		if seen[scope][def.Name] || def.Kind == parser.DefCommandEnv {
			continue
		}
		seen[scope][def.Name] = true
		byScope[scope] = append(byScope[scope], variableSymbol(def))
	}
	for _, src := range sources(tree) {
		byScope[tree.ScopeAt(src.Span.Start)] = append(byScope[tree.ScopeAt(src.Span.Start)], src)
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
				Detail:    "function",
				Kind:      SymbolFunction,
				Span:      fn.Span,
				Selection: fn.NameSpan,
				Children:  build(fn.Body),
			})
		}
		slices.SortStableFunc(out, func(a, b Symbol) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
		return out
	}
	return build(parser.FileScope)
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
		if (cmd.Name != "source" && cmd.Name != ".") || len(cmd.Args) == 0 {
			continue
		}
		arg, ok := tree.NodeSpan(cmd.Args[0])
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
	syntax.Walk(tree.File, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.IfClause:
			block(n.Position, n.FiPos)
		case *syntax.WhileClause:
			block(n.WhilePos, n.DonePos)
		case *syntax.ForClause:
			block(n.ForPos, n.DonePos)
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
