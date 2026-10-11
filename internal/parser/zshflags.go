package parser

import (
	"strings"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type ZshFlag struct {
	Name byte
	Args []Span
}

var (
	parameterFlagArgs = map[byte]int{'I': 1, 'Z': 1, '_': 1, 'g': 1, 'j': 1, 'l': 3, 'r': 3, 's': 1}
	subscriptFlagArgs = map[byte]int{'b': 1, 'n': 1, 's': 1}
	patternSubscripts = "rRiIkK"
)

func ParameterFlags(flags *syntax.Lit) []ZshFlag { return zshFlags(flags, parameterFlagArgs) }

func SubscriptFlags(flags *syntax.Lit) []ZshFlag { return zshFlags(flags, subscriptFlagArgs) }

func SubscriptPattern(n *syntax.FlagsArithm) bool {
	if n.Brackets {
		return false
	}
	for _, flag := range SubscriptFlags(n.Flags) {
		if strings.IndexByte(patternSubscripts, flag.Name) >= 0 {
			return true
		}
	}
	return false
}

func zshFlags(flags *syntax.Lit, arguments map[byte]int) []ZshFlag {
	text, base := flags.Value, offset(flags.Pos())
	var out []ZshFlag
	for i := 0; i < len(text); i++ {
		flag := ZshFlag{Name: text[i]}
		var open byte
		for len(flag.Args) < arguments[flag.Name] && i+1 < len(text) {
			if len(flag.Args) > 0 && text[i+1] != open {
				break
			}
			open = text[i+1]
			end := strings.IndexByte(text[i+2:], closingDelimiter(open))
			if end < 0 {
				break
			}
			flag.Args = append(flag.Args, Span{Start: base + i + 2, End: base + i + 2 + end})
			i += end + 2
		}
		out = append(out, flag)
	}
	return out
}

func closingDelimiter(open byte) byte {
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
