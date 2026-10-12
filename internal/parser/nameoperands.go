package parser

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type namedParameter struct {
	name  string
	span  Span
	attrs Attr
}

type nameOperands struct {
	reads, assigns []namedParameter
	command        []*syntax.Word
}

func (n *nameOperands) read(words ...*syntax.Word) {
	for _, w := range words {
		n.readArgument(whole(w))
	}
}

func (n *nameOperands) readArgument(arg *optionArgument) {
	if p, ok := parameterNamed(arg, 0); ok {
		n.reads = append(n.reads, p)
	}
}

func (n *nameOperands) assign(arg *optionArgument, attrs Attr) {
	if p, ok := parameterNamed(arg, attrs); ok {
		n.assigns = append(n.assigns, p)
	}
}

func (n *nameOperands) update(arg *optionArgument, attrs Attr) {
	n.readArgument(arg)
	n.assign(arg, attrs)
}

func whole(w *syntax.Word) *optionArgument {
	if w == nil {
		return nil
	}
	return &optionArgument{Word: w}
}

func parameterNamed(arg *optionArgument, attrs Attr) (namedParameter, bool) {
	if arg == nil {
		return namedParameter{}, false
	}
	var sb strings.Builder
	for _, part := range arg.Word.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			return namedParameter{}, false
		}
		sb.WriteString(lit.Value)
	}
	return parameterAt(sb.String()[arg.Offset:], offset(arg.Word.Pos())+arg.Offset, attrs)
}

func parameterAt(value string, start int, attrs Attr) (namedParameter, bool) {
	name := value
	if base, subscript, found := strings.Cut(value, "["); found && strings.HasSuffix(subscript, "]") {
		name = base
		attrs |= AttrArray
	}
	if !IsName(name) {
		return namedParameter{}, false
	}
	return namedParameter{name: name, span: Span{Start: start, End: start + len(name)}, attrs: attrs}, true
}

type zshOptions = getopt.Parsed[*syntax.Word]

func parseZsh(short string, args []*syntax.Word) zshOptions {
	return getopt.Parse(getopt.Grammar{Short: short, StopAtOperand: true}, args, Literal)
}

func operandAt(words []*syntax.Word, i int) *syntax.Word {
	if i < len(words) {
		return words[i]
	}
	return nil
}

func withoutOptions(words []*syntax.Word) []*syntax.Word {
	var out []*syntax.Word
	for _, w := range words {
		if value, ok := Literal(w); !ok || !strings.HasPrefix(value, "-") {
			out = append(out, w)
		}
	}
	return out
}

func labelName(short string) func(args []*syntax.Word) nameOperands {
	return func(args []*syntax.Word) nameOperands {
		var n nameOperands
		rest := parseZsh(short, args).Operands
		n.update(whole(operandAt(rest, 1)), AttrArray)
		if len(rest) > 3 {
			n.command = rest[3:]
		}
		return n
	}
}

func assignedOptions(short string, attrs map[string]Attr) func(args []*syntax.Word) nameOperands {
	return func(args []*syntax.Word) nameOperands {
		var n nameOperands
		n.assignOptions(parseZsh(short, args), attrs)
		return n
	}
}

func (n *nameOperands) assignOptions(parsed zshOptions, attrs map[string]Attr) {
	for _, o := range parsed.Options {
		if a, ok := attrs[o.Name]; ok {
			n.assign(o.Argument, a)
		}
	}
}

func describedArrays(args []*syntax.Word) nameOperands {
	var n nameOperands
	if rest := parseZsh("t:", args).Operands; len(rest) > 1 {
		n.read(withoutOptions(rest[1:])...)
	}
	return n
}

func addedMatches(args []*syntax.Word) nameOperands {
	var n nameOperands
	parsed := parseZsh("F:P:S:p:s:i:I:W:d:J:X:x:V:r:R:D:O:A:E:M:", args)
	for _, o := range parsed.Options {
		switch o.Name {
		case "-a", "-k":
			n.read(parsed.Operands...)
		case "-F", "-d":
			n.readArgument(o.Argument)
		case "-D":
			n.update(o.Argument, AttrArray)
		case "-O", "-A":
			n.assign(o.Argument, AttrArray)
		}
	}
	return n
}

func cachedParameters(args []*syntax.Word) nameOperands {
	var n nameOperands
	if len(args) > 1 {
		n.read(args[1:]...)
	}
	return n
}

func styleValue(args []*syntax.Word) nameOperands {
	var n nameOperands
	if len(args) == 0 {
		return n
	}
	switch args[0].Lit() {
	case "-s", "-b":
		n.assign(whole(operandAt(args, 3)), 0)
	case "-a":
		n.assign(whole(operandAt(args, 3)), AttrArray)
	case "-g":
		n.assign(whole(operandAt(args, 1)), AttrArray)
	}
	return n
}

func editedVariable(args []*syntax.Word) nameOperands {
	var n nameOperands
	if rest := parseZsh("p:r:M:m:i:f:t:", args).Operands; len(rest) > 0 {
		n.update(whole(rest[len(rest)-1]), 0)
	}
	return n
}

func parsedOptions(args []*syntax.Word) nameOperands {
	var n nameOperands
	parsed := parseZsh("a:A:", args)
	n.assignOptions(parsed, map[string]Attr{"-a": AttrArray, "-A": AttrAssoc})
	for _, spec := range parsed.Operands {
		lit, ok := spec.Parts[len(spec.Parts)-1].(*syntax.Lit)
		if !ok {
			continue
		}
		i := strings.LastIndexByte(lit.Value, '=')
		if i < 0 {
			continue
		}
		if p, ok := parameterAt(lit.Value[i+1:], offset(lit.Pos())+i+1, AttrArray); ok {
			n.assigns = append(n.assigns, p)
		}
	}
	return n
}

func systemRead(args []*syntax.Word) nameOperands {
	var n nameOperands
	parsed := parseZsh("c:i:o:s:t:", args)
	n.assignOptions(parsed, map[string]Attr{"-c": 0})
	if len(parsed.Operands) > 0 {
		n.assign(whole(parsed.Operands[0]), 0)
	}
	return n
}

var dialectNameCommands = map[dialect.Dialect]map[string]func(args []*syntax.Word) nameOperands{
	dialect.Zsh: {
		"_describe":    describedArrays,
		"compadd":      addedMatches,
		"_store_cache": cachedParameters,
		"_wanted":      labelName("C:"),
		"_description": labelName(""),
		"_all_labels":  labelName(""),
		"_next_label":  labelName(""),
		"_requested":   labelName(""),
		"zstyle":       styleValue,
		"vared":        editedVariable,
		"print":        assignedOptions("u:v:f:C:x:X:", map[string]Attr{"-v": 0}),
		"strftime":     assignedOptions("s:", map[string]Attr{"-s": 0}),
		"zparseopts":   parsedOptions,
		"zformat":      assignedOptions("f:F:a:", map[string]Attr{"-f": 0, "-F": 0, "-a": AttrArray}),
		"zstat":        assignedOptions("f:H:A:F:", map[string]Attr{"-H": AttrAssoc, "-A": AttrArray}),
		"zselect":      assignedOptions("t:a:A:", map[string]Attr{"-a": AttrArray, "-A": AttrAssoc}),
		"sysread":      systemRead,
	},
}
