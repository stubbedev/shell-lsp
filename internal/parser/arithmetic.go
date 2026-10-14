package parser

import (
	"math"
	"strconv"
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

func ArithmeticConstant(expr syntax.ArithmExpr) (int64, bool) {
	switch e := expr.(type) {
	case *syntax.ParenArithm:
		return ArithmeticConstant(e.X)
	case *syntax.UnaryArithm:
		if e.Op != syntax.Minus || e.Post {
			return 0, false
		}
		v, ok := ArithmeticConstant(e.X)
		return -v, ok
	case *syntax.Word:
		return arithmeticNumber(e.Lit())
	}
	return 0, false
}

func arithmeticNumber(s string) (int64, bool) {
	base := 10
	switch {
	case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
		base, s = 16, s[2:]
	case strings.Contains(s, "#"):
		prefix, digits, _ := strings.Cut(s, "#")
		b, err := strconv.Atoi(prefix)
		if err != nil || b < 2 || b > 64 {
			return 0, false
		}
		base, s = b, digits
	case len(s) > 1 && s[0] == '0':
		base, s = 8, s[1:]
	}
	if s == "" {
		return 0, false
	}
	var v int64
	for i := range len(s) {
		d, ok := arithmeticDigit(s[i], base)
		if !ok || v > (math.MaxInt64-d)/int64(base) {
			return 0, false
		}
		v = v*int64(base) + d
	}
	return v, true
}

func arithmeticDigit(c byte, base int) (int64, bool) {
	var d int64
	switch {
	case c >= '0' && c <= '9':
		d = int64(c - '0')
	case c >= 'a' && c <= 'z':
		d = int64(c-'a') + 10
	case c >= 'A' && c <= 'Z' && base > 36:
		d = int64(c-'A') + 36
	case c >= 'A' && c <= 'Z':
		d = int64(c-'A') + 10
	case c == '@':
		d = 62
	case c == '_':
		d = 63
	default:
		return 0, false
	}
	return d, d < int64(base)
}
