package glob

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/prose"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
)

type Piece struct {
	Span parser.Span
	Text string
	Doc  string
}

func (p Pattern) Pieces(text string) []Piece {
	d := describer{text: text, syn: p.Syntax}
	items := []Node{p.Root}
	if seq, ok := p.Root.(*Seq); ok {
		items = seq.Items
	}
	pieces := make([]Piece, 0, len(items))
	for _, n := range items {
		pieces = append(pieces, Piece{Span: n.Span(), Text: d.source(n), Doc: d.say(n)})
	}
	return pieces
}

type describer struct {
	text string
	syn  Syntax
}

func (d describer) source(n Node) string {
	at := n.Span()
	if at.Start < 0 || at.End > len(d.text) || at.Start > at.End {
		return ""
	}
	return d.text[at.Start:at.End]
}

func (d describer) quote(n Node) string {
	return prose.Code(d.source(n))
}

func (d describer) say(n Node) string {
	switch n := n.(type) {
	case *Literal:
		return d.literal(n)
	case *AnyChar:
		if d.syn.Filenames {
			return "any one character except `/`"
		}
		return "any one character"
	case *AnyString:
		if d.syn.Filenames {
			return "any characters within one path component; a leading `.` must be matched explicitly"
		}
		return "any text, including none"
	case *Recursive:
		if n.Follow {
			return "any number of directories, recursively, following symbolic links"
		}
		return "any number of directories, recursively"
	case *Bracket:
		return d.bracket(n)
	case *Seq:
		return d.seq(n.Items)
	case *Alternatives:
		return d.alternatives(n.Choices)
	case *Group:
		return d.group(n)
	case *Repeat:
		return repeatCountText(n.Min, n.Max) + " " + d.operand(n.X)
	case *Not:
		return "anything that does not match " + d.quote(n.X)
	case *Exclude:
		excepts := make([]string, len(n.Except))
		for i, e := range n.Except {
			excepts[i] = d.quote(e)
		}
		return d.say(n.Base) + ", except what matches " + strings.Join(excepts, " or ")
	case *NumRange:
		return numRangeText(n)
	case *Anchor:
		if n.Start {
			return "the start of the value"
		}
		return "the end of the value"
	case *Flags:
		return d.flags(n)
	case *Qualifiers:
		return d.qualifiers(n)
	case *Unknown:
		if n.Glob && d.syn.Regex != NotRegex {
			return "the value of " + prose.Code(n.Source) + ", used as part of the regular expression"
		}
		if n.Glob {
			return "the value of " + prose.Code(n.Source) + ", used as a pattern"
		}
		return "the value of " + prose.Code(n.Source) + ", matched literally"
	case *Escape:
		return regexEscapeText(n.Kind, d.syn.Dialect)
	case *Backref:
		return backrefText(n.N)
	case *KshOptions:
		if n.X == nil {
			return kshOptionsText(n.Flags) + ", for the rest of the pattern"
		}
		return kshOptionsText(n.Flags) + ", for " + d.quote(n.X) + ": " + d.say(n.X)
	case *Search:
		return d.search(n)
	case *Foreign:
		return foreignNames[n.Mode] + ", " + prose.Code(n.Text) + ", which shell-lsp does not take apart"
	}
	panic(fmt.Sprintf("glob: cannot describe %T", n))
}

func (d describer) literal(n *Literal) string {
	text := prose.Code(n.Text)
	switch n.Case {
	case CaseInsensitive:
		return "the text " + text + ", in either case"
	case CaseLowerFolds:
		return "the text " + text + ", lowercase letters matching either case"
	case CaseSensitive:
	}
	return "the text " + text
}

func (d describer) seq(items []Node) string {
	if len(items) == 0 {
		return "nothing"
	}
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = d.say(item)
	}
	return strings.Join(parts, ", then ")
}

func (d describer) operand(n Node) string {
	switch n.(type) {
	case *Literal:
		return d.quote(n)
	}
	return d.quote(n) + " (" + d.say(n) + ")"
}

func (d describer) alternatives(choices []Node) string {
	quoted := make([]string, len(choices))
	for i, c := range choices {
		quoted[i] = d.quote(c)
		if d.source(c) == "" {
			quoted[i] = "nothing"
		}
	}
	return "either " + prose.List(quoted, "or")
}

func (d describer) inner(g *Group) string {
	if alts, ok := g.X.(*Alternatives); ok {
		return d.alternatives(alts.Choices)
	}
	return d.say(g.X)
}

func (d describer) group(g *Group) string {
	switch g.Op {
	case GroupPlain:
		text := d.inner(g)
		if g.Capture > 0 {
			text += "; captured in " + d.captureVariable(g.Capture)
		}
		return text
	case GroupOne:
		return d.inner(g)
	case GroupZeroOrOne:
		return "optionally " + d.inner(g)
	case GroupZeroOrMore:
		return "zero or more repetitions of " + d.inner(g) + lazyText(g)
	case GroupOneOrMore:
		return "one or more repetitions of " + d.inner(g) + lazyText(g)
	case GroupCounted:
		return repeatCountText(g.Min, g.Max) + " repetitions of " + d.inner(g) + lazyText(g)
	case GroupNot:
		return "anything that does not match " + d.quote(g.X)
	}
	panic(fmt.Sprintf("glob: unknown group %d", g.Op))
}

func lazyText(g *Group) string {
	if g.Lazy {
		return ", as few as possible"
	}
	return ""
}

func (d describer) captureVariable(n int) string {
	switch {
	case d.syn.Regex == NotRegex || d.syn.Dialect == dialect.Zsh:
		return "`$match[" + strconv.Itoa(n) + "]`"
	case d.syn.Dialect == dialect.Ksh:
		return "`${.sh.match[" + strconv.Itoa(n) + "]}`"
	}
	return "`${BASH_REMATCH[" + strconv.Itoa(n) + "]}`"
}

func (d describer) search(n *Search) string {
	where := "found anywhere in the rest of the value"
	switch {
	case n.Left && n.Right:
		where = "matching the whole rest of the value"
	case n.Left:
		where = "found at the start of the rest of the value"
	case n.Right:
		where = "found at the end of the value"
	}
	return kshOptionsText(n.Flags) + "; " + where + ": " + d.say(n.X)
}

func repeatCountText(lo, hi int) string {
	switch {
	case lo == 0 && hi == unbounded:
		return "zero or more of"
	case lo == 1 && hi == unbounded:
		return "one or more of"
	case lo == 0 && hi == 1:
		return "optionally"
	case hi == unbounded:
		return "at least " + strconv.Itoa(lo) + " of"
	case lo == hi:
		return "exactly " + strconv.Itoa(lo) + " of"
	}
	return strconv.Itoa(lo) + " to " + strconv.Itoa(hi) + " of"
}

func numRangeText(n *NumRange) string {
	lo, hi := strconv.FormatUint(n.Lo, 10), strconv.FormatUint(n.Hi, 10)
	switch {
	case n.HasLo && n.HasHigh:
		return "a number from " + lo + " to " + hi
	case n.HasLo:
		return "a number of at least " + lo
	case n.HasHigh:
		return "a number up to " + hi
	}
	return "any number"
}

var classNames = map[string]string{
	"alpha": "a letter", "digit": "a digit", "alnum": "a letter or digit", "upper": "an uppercase letter",
	"lower": "a lowercase letter", "space": "whitespace", "blank": "a space or tab", "punct": "punctuation",
	"print": "a printable character", "graph": "a visible character", "cntrl": "a control character",
	"xdigit": "a hexadecimal digit", "word": "a letter, digit or underscore", "IDENT": "a character allowed in an identifier",
	"IFS": "a character in `$IFS`", "IFSSPACE": "whitespace in `$IFS`", "WORD": "a character that `$WORDCHARS` counts as part of a word",
	"INCOMPLETE": "an incomplete multibyte character", "INVALID": "an invalid multibyte character",
}

func (d describer) bracket(n *Bracket) string {
	members := make([]string, 0, len(n.Items))
	for _, item := range n.Items {
		switch {
		case item.Class != "":
			name, ok := classNames[item.Class]
			if !ok {
				name = "a character of the unknown class `[:" + item.Class + ":]`"
			}
			members = append(members, name)
		case item.Lo == item.Hi:
			members = append(members, charName(item.Lo))
		default:
			members = append(members, charName(item.Lo)+" to "+charName(item.Hi))
		}
	}
	list := prose.List(members, "or")
	text := "one character: " + list
	if n.Negated {
		text = "one character other than " + list
	}
	if n.Case == CaseInsensitive {
		text += ", in either case"
	}
	if d.syn.Filenames && n.Negated {
		text += " (never `/`)"
	}
	return text
}

var charNames = map[rune]string{' ': "a space", '\t': "a tab", '\n': "a newline", 0: "a NUL byte"}

func charName(r rune) string {
	if name, ok := charNames[r]; ok {
		return name
	}
	return prose.Code(string(r))
}

func (d describer) flags(n *Flags) string {
	docs := make([]string, 0, len(n.Flags))
	for _, f := range n.Flags {
		e, ok := shelldocs.Term(shelldocs.ZshGlobFlags, string(f.Name), dialect.Zsh)
		if !ok {
			continue
		}
		docs = append(docs, strings.TrimSuffix(e.Doc, "."))
	}
	return strings.Join(docs, "; ")
}

var sortKeys = map[byte]string{
	'n': "name", 'L': "size", 'l': "number of links", 'a': "access time", 'm': "modification time",
	'c': "inode change time", 'd': "subdirectories first", 'N': "no sorting",
}

func (d describer) qualifiers(n *Qualifiers) string {
	docs := make([]string, 0, len(n.List))
	for _, q := range n.List {
		docs = append(docs, prose.Code(q.Name+q.Arg)+": "+qualifierText(q))
	}
	return "glob qualifiers: " + strings.Join(docs, "; ")
}

func qualifierText(q Qualifier) string {
	switch q.Name {
	case "o", "O":
		order := "ascending"
		if q.Name == "O" {
			order = "descending"
		}
		if key, ok := sortKeys[firstByte(q.Arg)]; ok {
			return "sort " + order + " by " + key
		}
	case "[":
		return selectionText(strings.TrimSuffix(q.Arg, "]"))
	case "L":
		return "files by size: " + sizeText(q.Arg)
	case "m", "a", "c":
		what := map[string]string{"m": "modified", "a": "accessed", "c": "changed"}[q.Name]
		return "files " + what + " " + ageText(q.Arg)
	case ":":
		return modifiersText(q.Arg)
	}
	e, ok := shelldocs.Term(shelldocs.ZshGlobQualifiers, q.Name, dialect.Zsh)
	if !ok {
		return "unknown qualifier"
	}
	return strings.TrimSuffix(e.Doc, ".")
}

func firstByte(s string) byte {
	if s == "" {
		return 0
	}
	return s[0]
}

func selectionText(arg string) string {
	from, to, ranged := strings.Cut(arg, ",")
	if !ranged {
		return "only match number " + from + " after sorting (negative numbers count from the end)"
	}
	return "only matches " + from + " to " + to + " after sorting (negative numbers count from the end)"
}

var (
	sizeUnits = map[byte]string{'k': "KiB", 'K': "KiB", 'm': "MiB", 'M': "MiB", 'p': "512-byte blocks", 'P': "512-byte blocks"}
	ageUnits  = map[byte]string{'M': "months", 'w': "weeks", 'h': "hours", 'm': "minutes", 's': "seconds", 'd': "days"}
)

func amount(arg string, units map[byte]string, unit string) (byte, string) {
	if name, ok := units[firstByte(arg)]; ok {
		unit, arg = name, arg[1:]
	}
	sign := firstByte(arg)
	if sign != '+' && sign != '-' {
		return 0, arg + " " + unit
	}
	return sign, arg[1:] + " " + unit
}

func sizeText(arg string) string {
	sign, text := amount(arg, sizeUnits, "bytes")
	switch sign {
	case '+':
		return "more than " + text
	case '-':
		return "less than " + text
	}
	return "exactly " + text
}

func ageText(arg string) string {
	sign, text := amount(arg, ageUnits, "days")
	switch sign {
	case '+':
		return "more than " + text + " ago"
	case '-':
		return "within the last " + text
	}
	return "exactly " + text + " ago"
}

func modifiersText(arg string) string {
	var docs []string
	for m := range strings.SplitSeq(arg, ":") {
		if e, ok := shelldocs.ZshModifier(m); ok {
			docs = append(docs, prose.Code(":"+m)+" "+strings.TrimSuffix(e.Doc, "."))
		}
	}
	return "apply modifiers to every match: " + strings.Join(docs, "; ")
}
