package completion

import (
	"fmt"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const plainPattern = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-+:=@,%/"

type caseValue struct {
	value, detail string
	getopts       bool
}

func (c completer) casePatterns() []Item {
	var items []Item
	for _, v := range caseValues(c.Tree, c.site.name, c.Offset) {
		if !strings.HasPrefix(v.value, c.site.prefix) {
			continue
		}
		sort := "1"
		if v.getopts {
			sort = "0"
		}
		it := c.item(v.value, KindValue, sort)
		it.Detail = v.detail
		items = append(items, it)
	}
	return items
}

func caseValues(tree *parser.Tree, name string, offset int) []caseValue {
	used := map[string]bool{}
	for _, p := range currentPatterns(tree, offset) {
		used[p] = true
	}
	var values []caseValue
	add := func(value, detail string, getopts bool) {
		if value == "" || used[value] || (!getopts && strings.Trim(value, plainPattern) != "") {
			return
		}
		used[value] = true
		values = append(values, caseValue{value: value, detail: detail, getopts: getopts})
	}
	getoptsValues(tree, name, add)
	parser.Walk(tree.File, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.BinaryTest:
			if n.Op == syntax.TsMatchShort || n.Op == syntax.TsMatch || n.Op == syntax.TsNoMatch {
				x, _ := n.X.(*syntax.Word)
				y, _ := n.Y.(*syntax.Word)
				addComparison(tree, name, x, y, add)
			}
		case *syntax.CallExpr:
			if len(n.Args) == 0 || (!parser.LiteralIs(n.Args[0], "[") && !parser.LiteralIs(n.Args[0], "test")) {
				return true
			}
			for i := 1; i+2 < len(n.Args); i++ {
				if op, _ := parser.Literal(n.Args[i+1]); op == "=" || op == "==" || op == "!=" {
					addComparison(tree, name, n.Args[i], n.Args[i+2], add)
				}
			}
		case *syntax.CaseClause:
			if !refersTo(n.Word, name) {
				return true
			}
			line := tree.Line(parser.Offset(n.Pos()))
			for _, item := range n.Items {
				for _, pattern := range item.Patterns {
					if value, ok := parser.Literal(pattern); ok {
						add(value, fmt.Sprintf("pattern of the case on line %d", line), false)
					}
				}
			}
		}
		return true
	})
	return values
}

func getoptsValues(tree *parser.Tree, name string, add func(value, detail string, getopts bool)) {
	for _, cmd := range tree.Commands {
		if cmd.Name != "getopts" || len(cmd.Args) < 2 || !parser.LiteralIs(cmd.Args[1], name) {
			continue
		}
		optstring, ok := parser.Literal(cmd.Args[0])
		if !ok {
			continue
		}
		line := tree.Line(cmd.Span.Start)
		letters, silent := strings.CutPrefix(optstring, ":")
		for i := range len(letters) {
			if letters[i] == ':' {
				continue
			}
			detail := fmt.Sprintf("option -%c of getopts on line %d", letters[i], line)
			if i+1 < len(letters) && letters[i+1] == ':' {
				detail += "; its argument is in $OPTARG"
			}
			add(letters[i:i+1], detail, true)
		}
		add(`\?`, fmt.Sprintf("unknown option, reported by getopts on line %d", line), true)
		if silent {
			add(":", fmt.Sprintf("option missing its argument, reported by getopts on line %d", line), true)
		}
	}
}

func addComparison(tree *parser.Tree, name string, x, y *syntax.Word, add func(value, detail string, getopts bool)) {
	if x == nil || y == nil {
		return
	}
	if refersTo(y, name) {
		x, y = y, x
	}
	if !refersTo(x, name) {
		return
	}
	if value, ok := parser.Literal(y); ok {
		add(value, fmt.Sprintf("compared on line %d", tree.Line(parser.Offset(y.Pos()))), false)
	}
}

func refersTo(w *syntax.Word, name string) bool {
	if w == nil || len(w.Parts) != 1 {
		return false
	}
	part := w.Parts[0]
	if dq, ok := part.(*syntax.DblQuoted); ok && len(dq.Parts) == 1 {
		part = dq.Parts[0]
	}
	pe, ok := part.(*syntax.ParamExp)
	return ok && pe.Param != nil && pe.Param.Value == name && pe.Exp == nil && pe.Repl == nil && pe.Slice == nil && pe.Index == nil && !pe.Length && !pe.Excl
}

func currentPatterns(tree *parser.Tree, offset int) []string {
	var patterns []string
	for _, node := range tree.PathAt(offset) {
		clause, ok := node.(*syntax.CaseClause)
		if !ok {
			continue
		}
		patterns = patterns[:0]
		for _, item := range clause.Items {
			for _, pattern := range item.Patterns {
				if parser.Offset(pattern.Pos()) <= offset && offset <= parser.Offset(pattern.End()) {
					continue
				}
				if value, ok := parser.Literal(pattern); ok {
					patterns = append(patterns, value)
				}
			}
		}
	}
	return patterns
}
