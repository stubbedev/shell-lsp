package lang

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

type Emitted struct {
	index int
	ok    bool
}

func (r *Result) Emit(start, end int, kind Kind, mods Modifier) Emitted {
	if end <= start {
		return Emitted{}
	}
	r.Tokens = append(r.Tokens, Token{Start: start, End: end, Kind: kind, Modifiers: mods})
	return Emitted{index: len(r.Tokens) - 1, ok: true}
}

func (r *Result) Reclassify(e Emitted, kind Kind, mods Modifier) {
	if !e.ok {
		return
	}
	r.Tokens[e.index].Kind = kind
	r.Tokens[e.index].Modifiers = mods
}

type Lexeme[K ~uint8] struct {
	Kind       K
	Start, End int
	Sem        Emitted
}

type operator[K ~uint8] struct {
	text        string
	kind        K
	punctuation bool
}

type Operators[K ~uint8] struct {
	byFirst [256][]operator[K]
}

func NewOperators[K ~uint8](operators, punctuation map[string]K) *Operators[K] {
	o := &Operators[K]{}
	add := func(texts map[string]K, isPunctuation bool) {
		for text, kind := range texts {
			if text == "" {
				panic("lang: an operator needs text")
			}
			if _, both := operators[text]; both && isPunctuation {
				panic(fmt.Sprintf("lang: %q is both an operator and punctuation", text))
			}
			o.byFirst[text[0]] = append(o.byFirst[text[0]], operator[K]{text: text, kind: kind, punctuation: isPunctuation})
		}
	}
	add(operators, false)
	add(punctuation, true)
	for _, candidates := range o.byFirst {
		slices.SortFunc(candidates, func(a, b operator[K]) int {
			return cmp.Or(cmp.Compare(len(b.text), len(a.text)), strings.Compare(a.text, b.text))
		})
	}
	return o
}

func (o *Operators[K]) Scan(r *Result, src string, at int) (Lexeme[K], bool) {
	for _, op := range o.byFirst[src[at]] {
		if !strings.HasPrefix(src[at:], op.text) {
			continue
		}
		end := at + len(op.text)
		l := Lexeme[K]{Kind: op.kind, Start: at, End: end}
		if !op.punctuation {
			l.Sem = r.Emit(at, end, Operator, 0)
		}
		return l, true
	}
	return Lexeme[K]{}, false
}

func StartsNumber(src string, at int) bool {
	return scan.Digit(src[at]) || (src[at] == '.' && at+1 < len(src) && scan.Digit(src[at+1]))
}

func ScanNumber(src string, at int) int {
	at = scanDigits(src, at)
	if at < len(src) && src[at] == '.' {
		at = scanDigits(src, at+1)
	}
	if at < len(src) && src[at]|0x20 == 'e' {
		exponent := at + 1
		if exponent < len(src) && (src[exponent] == '+' || src[exponent] == '-') {
			exponent++
		}
		if exponent < len(src) && scan.Digit(src[exponent]) {
			at = scanDigits(src, exponent)
		}
	}
	return at
}

func scanDigits(src string, at int) int {
	for at < len(src) && scan.Digit(src[at]) {
		at++
	}
	return at
}

func ScanName(src string, at int) int {
	for at < len(src) && scan.NameChar(src[at]) {
		at++
	}
	return at
}

func ScanQualifiedName(src string, at int) int {
	at = ScanName(src, at)
	for strings.HasPrefix(src[at:], "::") && at+2 < len(src) && scan.NameStart(src[at+2]) {
		at = ScanName(src, at+2)
	}
	return at
}
