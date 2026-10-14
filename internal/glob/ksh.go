package glob

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const (
	kshShellMode   = 'K'
	kshOptionChars = "+-EFGKNPVXiglr"
)

type kshOptionSet struct {
	mode        byte
	fold        *bool
	left, right bool
}

func parseKshOptions(flags string) kshOptionSet {
	set := kshOptionSet{mode: kshShellMode}
	on := true
	for i := range len(flags) {
		switch c := flags[i]; c {
		case '+', '-':
			on = c == '+'
		case 'E', 'F', 'G', 'K', 'P', 'V', 'X':
			set.mode = c
		case 'i':
			fold := on
			set.fold = &fold
		case 'l':
			set.left = on
		case 'r':
			set.right = on
		}
	}
	return set
}

func (p *parse) kshOptionsAt() bool {
	return p.syn.Dialect == dialect.Ksh && p.activeAt(0, '~') && p.activeAt(1, '(')
}

func (p *parse) kshOptions(depth int) (Node, bool) {
	start := p.pos
	i := 2
	for {
		it, ok := p.peek(i)
		if !ok || it.unknown != nil || it.quals != nil || !it.active {
			return nil, false
		}
		if it.c == ')' || it.c == ':' {
			break
		}
		if strings.IndexByte(kshOptionChars, it.c) < 0 {
			return nil, false
		}
		i++
	}
	flags := p.flagText(i)
	set := parseKshOptions(flags)
	scoped := p.items[p.pos+i].c == ':'
	p.pos += i + 1
	from := p.pos
	to, closed := p.scopeEnd(scoped, depth)
	if scoped && !closed {
		p.fail(p.items[start].at, "unclosed ~( group")
	}
	outer := p.cs
	if set.fold != nil {
		p.cs = CaseSensitive
		if *set.fold {
			p.cs = CaseInsensitive
		}
	}
	if scoped {
		defer func() { p.cs = outer }()
	}
	if set.mode == kshShellMode {
		if set.left || set.right {
			p.out.Unevaluable = "a ksh pattern that is not anchored"
		}
		if !scoped {
			return &KshOptions{At: p.span(start), Flags: flags}, true
		}
		x := p.inGroup(depth)
		if closed {
			p.pos++
		}
		return &KshOptions{At: p.span(start), Flags: flags, X: x}, true
	}
	x := p.kshForeign(set, from, to)
	p.pos = to
	if scoped && closed {
		p.pos++
	}
	return &Search{At: p.span(start), Flags: flags, Mode: set.mode, X: x, Left: set.left, Right: set.right}, true
}

func (p *parse) scopeEnd(scoped bool, depth int) (int, bool) {
	level := 0
	for i := p.pos; i < len(p.items); i++ {
		it := p.items[i]
		if !it.active || it.unknown != nil {
			continue
		}
		switch it.c {
		case '(':
			level++
		case ')':
			if level == 0 && (scoped || depth > 0) {
				return i, true
			}
			level--
		}
	}
	return len(p.items), false
}

func (p *parse) kshForeign(set kshOptionSet, from, to int) Node {
	sub := &parse{items: p.items[from:to], syn: p.syn, out: p.out, cs: p.cs}
	switch set.mode {
	case 'E', 'G':
		sub.syn.Regex = ExtendedRegex
		if set.mode == 'G' {
			sub.syn.Regex = BasicRegex
		}
		return sub.regexAlternation(0)
	case 'F':
		var items []Node
		for sub.pos < len(sub.items) {
			items = append(items, sub.literal())
		}
		return &Seq{At: sub.span(0), Items: mergeLiterals(items)}
	}
	var text strings.Builder
	for _, it := range sub.items {
		text.WriteByte(it.c)
	}
	p.out.Unevaluable = "a ksh ~(" + string(set.mode) + ") pattern"
	at := parser.Span{}
	if len(sub.items) > 0 {
		at = parser.Span{Start: sub.items[0].at.Start, End: sub.items[len(sub.items)-1].at.End}
	}
	return &Foreign{At: at, Mode: set.mode, Text: text.String()}
}

var kshOptionDocs = map[byte]string{
	'E': "the rest is a POSIX extended regular expression",
	'F': "the rest is a fixed string",
	'G': "the rest is a POSIX basic regular expression",
	'K': "the rest is a shell pattern",
	'P': "the rest is a perl regular expression",
	'V': "the rest is a System V regular expression",
	'X': "the rest is an augmented regular expression",
	'N': "a file name pattern that matches nothing expands to nothing",
	'i': "letters match in either case",
	'g': "the longest match is taken",
	'l': "the match is anchored at the start",
	'r': "the match is anchored at the end",
}

func kshOptionsText(flags string) string {
	var docs []string
	on := true
	for i := range len(flags) {
		c := flags[i]
		if c == '+' || c == '-' {
			on = c == '+'
			continue
		}
		doc := kshOptionDocs[c]
		if !on {
			doc = "not: " + doc
		}
		docs = append(docs, "`"+string(c)+"` "+doc)
	}
	return "ksh93 pattern options: " + strings.Join(docs, "; ")
}

var foreignNames = map[byte]string{'P': "a perl regular expression", 'V': "a System V regular expression", 'X': "an augmented regular expression"}
