package parser

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
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
		if p, ok := parameterNamed(w, 0); ok {
			n.reads = append(n.reads, p)
		}
	}
}

func (n *nameOperands) assign(w *syntax.Word, attrs Attr) {
	if p, ok := parameterNamed(w, attrs); ok {
		n.assigns = append(n.assigns, p)
	}
}

func (n *nameOperands) update(w *syntax.Word, attrs Attr) {
	n.read(w)
	n.assign(w, attrs)
}

func parameterNamed(w *syntax.Word, attrs Attr) (namedParameter, bool) {
	if w == nil {
		return namedParameter{}, false
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			return namedParameter{}, false
		}
		sb.WriteString(lit.Value)
	}
	return parameterAt(sb.String(), offset(w.Pos()), attrs)
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

type option struct {
	letter   byte
	argument *syntax.Word
}

func splitOptions(args []*syntax.Word, withArgument string) ([]option, []*syntax.Word) {
	var opts []option
	for i := 0; i < len(args); i++ {
		value, ok := Literal(args[i])
		if !ok || len(value) < 2 || value[0] != '-' {
			return opts, args[i:]
		}
		if value == "--" {
			return opts, args[i+1:]
		}
		for j := 1; j < len(value); j++ {
			opt := option{letter: value[j]}
			if strings.IndexByte(withArgument, value[j]) < 0 {
				opts = append(opts, opt)
				continue
			}
			if j+1 == len(value) && i+1 < len(args) {
				i++
				opt.argument = args[i]
			}
			opts = append(opts, opt)
			break
		}
	}
	return opts, nil
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

func labelName(withArgument string) func(args []*syntax.Word) nameOperands {
	return func(args []*syntax.Word) nameOperands {
		var n nameOperands
		_, rest := splitOptions(args, withArgument)
		n.update(operandAt(rest, 1), AttrArray)
		if len(rest) > 3 {
			n.command = rest[3:]
		}
		return n
	}
}

func assignedOptions(withArgument string, attrs map[byte]Attr) func(args []*syntax.Word) nameOperands {
	return func(args []*syntax.Word) nameOperands {
		var n nameOperands
		opts, _ := splitOptions(args, withArgument)
		for _, o := range opts {
			if a, ok := attrs[o.letter]; ok {
				n.assign(o.argument, a)
			}
		}
		return n
	}
}

func describedArrays(args []*syntax.Word) nameOperands {
	var n nameOperands
	if _, rest := splitOptions(args, "t"); len(rest) > 1 {
		n.read(withoutOptions(rest[1:])...)
	}
	return n
}

func addedMatches(args []*syntax.Word) nameOperands {
	var n nameOperands
	opts, rest := splitOptions(args, "FPSpsiIWdJXxVrRDOAEM")
	for _, o := range opts {
		switch o.letter {
		case 'a', 'k':
			n.read(rest...)
		case 'F', 'd':
			n.read(o.argument)
		case 'D':
			n.update(o.argument, AttrArray)
		case 'O', 'A':
			n.assign(o.argument, AttrArray)
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
		n.assign(operandAt(args, 3), 0)
	case "-a":
		n.assign(operandAt(args, 3), AttrArray)
	case "-g":
		n.assign(operandAt(args, 1), AttrArray)
	}
	return n
}

func editedVariable(args []*syntax.Word) nameOperands {
	var n nameOperands
	if _, rest := splitOptions(args, "prMmift"); len(rest) > 0 {
		n.update(rest[len(rest)-1], 0)
	}
	return n
}

func parsedOptions(args []*syntax.Word) nameOperands {
	n := assignedOptions("aA", map[byte]Attr{'a': AttrArray, 'A': AttrAssoc})(args)
	_, rest := splitOptions(args, "aA")
	for _, spec := range rest {
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

var dialectNameCommands = map[dialect.Dialect]map[string]func(args []*syntax.Word) nameOperands{
	dialect.Zsh: {
		"_describe":    describedArrays,
		"compadd":      addedMatches,
		"_store_cache": cachedParameters,
		"_wanted":      labelName("C"),
		"_description": labelName(""),
		"_all_labels":  labelName(""),
		"_next_label":  labelName(""),
		"_requested":   labelName(""),
		"zstyle":       styleValue,
		"vared":        editedVariable,
		"print":        assignedOptions("uvfCxX", map[byte]Attr{'v': 0}),
		"strftime":     assignedOptions("s", map[byte]Attr{'s': 0}),
		"zparseopts":   parsedOptions,
	},
}
