package parser

import (
	"strings"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type ZshFlag struct {
	Name       byte
	Args       []FlagArgument
	Arithmetic bool
}

type FlagArgument struct {
	Value string
	Span  Span
}

type flagArguments struct {
	most       int
	arithmetic bool
}

var (
	parameterFlagArgs = map[byte]flagArguments{
		'I': {most: 1, arithmetic: true}, 'l': {most: 3, arithmetic: true}, 'r': {most: 3, arithmetic: true},
		'Z': {most: 1}, '_': {most: 1}, 'g': {most: 1}, 'j': {most: 1}, 's': {most: 1},
	}
	subscriptFlagArgs = map[byte]flagArguments{'b': {most: 1, arithmetic: true}, 'n': {most: 1, arithmetic: true}, 's': {most: 1}}
	patternSubscripts = "rRiIkK"
)

const flagParameterArguments = 'p'

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

func zshFlags(flags *syntax.Lit, arguments map[byte]flagArguments) []ZshFlag {
	text, base := flags.Value, offset(flags.Pos())
	var out []ZshFlag
	for i := 0; i < len(text); i++ {
		spec := arguments[text[i]]
		flag := ZshFlag{Name: text[i], Arithmetic: spec.arithmetic}
		var open byte
		for len(flag.Args) < spec.most && i+1 < len(text) {
			if len(flag.Args) > 0 && text[i+1] != open {
				break
			}
			open = text[i+1]
			end := strings.IndexByte(text[i+2:], closingDelimiter(open))
			if end < 0 {
				break
			}
			flag.Args = append(flag.Args, FlagArgument{Value: text[i+2 : i+2+end], Span: Span{Start: base + i + 2, End: base + i + 2 + end}})
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

func ZshReads(node syntax.Node) []NameRead {
	switch n := node.(type) {
	case *syntax.ParamExp:
		var reads []NameRead
		if n.Flags != nil {
			reads = flagReads(ParameterFlags(n.Flags))
		}
		if name, ok := arrayOperand(n); ok {
			reads = append(reads, NameRead{Name: name.Value, Span: Span{Start: offset(name.Pos()), End: offset(name.End())}})
		}
		return reads
	case *syntax.FlagsArithm:
		if !n.Brackets {
			return flagReads(SubscriptFlags(n.Flags))
		}
	}
	return nil
}

func flagReads(flags []ZshFlag) []NameRead {
	var reads []NameRead
	parameterArguments := false
	for _, flag := range flags {
		parameterArguments = parameterArguments || flag.Name == flagParameterArguments
		for i, arg := range flag.Args {
			if flag.Arithmetic && i == 0 {
				found, _ := arithmeticReads(arg.Value, arg.Span.Start)
				reads = append(reads, found...)
				continue
			}
			if name, ok := strings.CutPrefix(arg.Value, "$"); ok && parameterArguments && IsName(name) {
				reads = append(reads, NameRead{Name: name, Span: Span{Start: arg.Span.Start + 1, End: arg.Span.End}})
			}
		}
	}
	return reads
}

func arrayOperand(n *syntax.ParamExp) (*syntax.Lit, bool) {
	if n.Exp == nil || n.Exp.Word == nil || len(n.Exp.Word.Parts) != 1 {
		return nil, false
	}
	switch n.Exp.Op {
	case syntax.ArrayExclude, syntax.ArrayIntersect, syntax.ArrayZip, syntax.ArrayZipLongest:
	default:
		return nil, false
	}
	lit, ok := n.Exp.Word.Parts[0].(*syntax.Lit)
	return lit, ok && IsName(lit.Value)
}
