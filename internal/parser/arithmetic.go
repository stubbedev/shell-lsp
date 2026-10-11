package parser

import (
	"strings"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type NameRead struct {
	Name string
	Span Span
}

var arithmeticStatusBuiltins = map[string]bool{"break": true, "continue": true, "exit": true, "return": true}

func ArithmeticStatusBuiltin(name string) bool { return arithmeticStatusBuiltins[name] }

func LiteralArithmeticReads(w *syntax.Word) ([]NameRead, bool) {
	if w == nil || len(w.Parts) != 1 {
		return nil, false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return nil, false
	}
	return arithmeticReads(lit.Value, offset(lit.Pos()))
}

func arithmeticReads(text string, base int) ([]NameRead, bool) {
	if IsDigits(text) {
		return nil, true
	}
	expr, err := syntax.NewParser(syntax.Variant(syntax.LangZsh)).Arithmetic(strings.NewReader(text))
	if err != nil || expr == nil {
		return nil, false
	}
	var reads []NameRead
	at := func(pos syntax.Pos, name string) Span {
		start := base + offset(pos)
		return Span{Start: start, End: start + len(name)}
	}
	stack := []syntax.Node{&syntax.ArithmExp{}}
	Walk(expr, func(n syntax.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch n := n.(type) {
		case *syntax.Word:
			if name, ok := ArithmeticRead(stack[len(stack)-1], n); ok {
				reads = append(reads, NameRead{Name: name, Span: at(n.Pos(), name)})
			}
		case *syntax.ParamExp:
			if n.Param != nil && IsName(n.Param.Value) {
				reads = append(reads, NameRead{Name: n.Param.Value, Span: at(n.Param.Pos(), n.Param.Value)})
			}
		}
		stack = append(stack, n)
		return true
	})
	return reads, true
}
