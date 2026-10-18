package shlex

import (
	"fmt"
	"iter"
	"strings"
)

type Kind uint8

const (
	Word Kind = iota + 1
	Comment
	Newline
	HeredocBody
	AndIf
	OrIf
	Pipe
	PipeAll
	Semi
	DSemi
	SemiAnd
	DSemiAnd
	SemiPipe
	Amp
	LParen
	RParen
	DLParen
	DRParen
	LBrace
	RBrace
	Less
	Great
	DGreat
	DLess
	DLessDash
	TLess
	LessAnd
	GreatAnd
	AndGreat
	AndDGreat
	Clobber
	LessGreat
)

var kindText = [...]string{
	Word:        "word",
	Comment:     "comment",
	Newline:     "newline",
	HeredocBody: "here-document body",
	AndIf:       "&&",
	OrIf:        "||",
	Pipe:        "|",
	PipeAll:     "|&",
	Semi:        ";",
	DSemi:       ";;",
	SemiAnd:     ";&",
	DSemiAnd:    ";;&",
	SemiPipe:    ";|",
	Amp:         "&",
	LParen:      "(",
	RParen:      ")",
	DLParen:     "((",
	DRParen:     "))",
	LBrace:      "{",
	RBrace:      "}",
	Less:        "<",
	Great:       ">",
	DGreat:      ">>",
	DLess:       "<<",
	DLessDash:   "<<-",
	TLess:       "<<<",
	LessAnd:     "<&",
	GreatAnd:    ">&",
	AndGreat:    "&>",
	AndDGreat:   "&>>",
	Clobber:     ">|",
	LessGreat:   "<>",
}

func (k Kind) String() string {
	if int(k) < len(kindText) && kindText[k] != "" {
		return kindText[k]
	}
	return fmt.Sprintf("Kind(%d)", k)
}

func (k Kind) isRedirect() bool {
	return Less <= k && k <= LessGreat
}

type Flags uint8

const (
	CommandStart Flags = 1 << iota
	Keyword
	Arithmetic
	Unterminated
)

type Token struct {
	Kind  Kind
	Flags Flags
	Start int
	End   int
	Parts []Part
}

func (t Token) Has(f Flags) bool {
	return t.Flags&f != 0
}

func (t Token) Text(src string) string {
	return src[t.Start:t.End]
}

func (t Token) Literal(src string) (string, bool) {
	if t.Kind != Word {
		return "", false
	}
	if len(t.Parts) == 1 && t.Parts[0].Kind == Literal {
		return t.Parts[0].Text(src), true
	}
	var b strings.Builder
	for _, p := range t.Parts {
		switch p.Kind {
		case Literal:
			b.WriteString(p.Text(src))
		case Continuation:
		default:
			return "", false
		}
	}
	return b.String(), true
}

type PartKind uint8

const (
	Literal PartKind = iota + 1
	Escaped
	Continuation
	SingleQuoted
	DollarSingleQuoted
	DoubleQuoted
	DollarDoubleQuoted
	Param
	CommandSubst
	ArithmeticExpansion
	Backquote
	ProcessSubst
	Array
)

type Part struct {
	Kind         PartKind
	Unterminated bool
	Start        int
	End          int
	Parts        []Part
	Tokens       []Token
}

func (p Part) Text(src string) string {
	return src[p.Start:p.End]
}

func All(tokens []Token) iter.Seq2[[]Token, int] {
	return func(yield func([]Token, int) bool) {
		allTokens(tokens, yield)
	}
}

func allTokens(tokens []Token, yield func([]Token, int) bool) bool {
	for i := range tokens {
		if !yield(tokens, i) || !allInParts(tokens[i].Parts, yield) {
			return false
		}
	}
	return true
}

func allInParts(parts []Part, yield func([]Token, int) bool) bool {
	for _, p := range parts {
		if !allTokens(p.Tokens, yield) || !allInParts(p.Parts, yield) {
			return false
		}
	}
	return true
}

func IsNameChar(c byte) bool {
	return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func AssignmentPrefix(word string) (int, bool) {
	i := 0
	for i < len(word) && IsNameChar(word[i]) {
		i++
	}
	if i == 0 || isDigit(word[0]) {
		return 0, false
	}
	if i < len(word) && word[i] == '[' {
		end, ok := subscriptEnd(word, i)
		if !ok {
			return 0, false
		}
		i = end
	}
	switch {
	case strings.HasPrefix(word[i:], "+="):
		return i + 2, true
	case strings.HasPrefix(word[i:], "="):
		return i + 1, true
	}
	return 0, false
}

func subscriptEnd(word string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(word); i++ {
		switch word[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}
