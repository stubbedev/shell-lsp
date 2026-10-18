package glob

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/scan"
)

type qualifierArg uint8

const (
	argNone qualifierArg = iota
	argSortKey
	argAmount
	argDelimited
	argIdentifier
	argDigits
	argUserOrDigits
	argModeOrDigits
	argRange
	argModifiers
	argDeviceKind
)

var qualifierArgs = map[byte]qualifierArg{
	'o': argSortKey, 'O': argSortKey,
	'L': argAmount, 'a': argAmount, 'm': argAmount, 'c': argAmount, 'l': argAmount,
	'e': argDelimited, 'P': argDelimited,
	'+': argIdentifier,
	'd': argDigits, 'Y': argDigits,
	'u': argUserOrDigits, 'g': argUserOrDigits,
	'f': argModeOrDigits,
	'[': argRange,
	':': argModifiers,
	'%': argDeviceKind,
}

const amountUnits = "kKmMpPwhsd"

func parseQualifiers(text string, offsets []int, explicit bool) *Qualifiers {
	q := &Qualifiers{At: parser.Span{Start: offsets[0], End: offsets[len(offsets)-1]}, Explicit: explicit}
	first := len("(")
	if explicit {
		first = len("(#q")
	}
	end := len(text) - len(")")
	for i := first; i < end; {
		start := i
		c := text[i]
		i++
		switch qualifierArgs[c] {
		case argNone:
		case argSortKey:
			if i < end {
				i++
			}
		case argAmount:
			if c != 'l' && i < end && strings.IndexByte(amountUnits, text[i]) >= 0 {
				i++
			}
			if i < end && (text[i] == '+' || text[i] == '-') {
				i++
			}
			i = digits(text, i, end)
		case argDelimited:
			i = delimited(text, i, end)
		case argIdentifier:
			for i < end && (scan.NameChar(text[i]) || text[i] == ':') {
				i++
			}
		case argDigits:
			i = digits(text, i, end)
		case argUserOrDigits, argModeOrDigits:
			if i < end && !scan.Digit(text[i]) {
				i = delimited(text, i, end)
				break
			}
			i = digits(text, i, end)
		case argRange:
			if close := strings.IndexByte(text[i:end], ']'); close >= 0 {
				i += close + 1
			}
		case argModifiers:
			i = end
		case argDeviceKind:
			if i < end && (text[i] == 'b' || text[i] == 'c') {
				i++
			}
		}
		q.List = append(q.List, Qualifier{
			At:   parser.Span{Start: offsets[start], End: offsets[i]},
			Name: string(c),
			Arg:  text[start+1 : i],
		})
	}
	return q
}

func digits(text string, i, end int) int {
	for i < end && scan.Digit(text[i]) {
		i++
	}
	return i
}

func delimited(text string, i, end int) int {
	if i >= end {
		return i
	}
	closing := closer(text[i])
	if close := strings.IndexByte(text[i+1:end], closing); close >= 0 {
		return i + 1 + close + 1
	}
	return end
}

func closer(open byte) byte {
	switch open {
	case '(':
		return ')'
	case '[':
		return ']'
	case '{':
		return '}'
	case '<':
		return '>'
	}
	return open
}
