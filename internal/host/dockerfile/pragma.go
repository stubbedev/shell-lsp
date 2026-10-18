package dockerfile

import (
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

const pragmaTool = "hadolint"

type PragmaKind uint8

const (
	PragmaIgnore PragmaKind = iota + 1
	PragmaGlobalIgnore
	PragmaShell
)

type Pragma struct {
	Kind  PragmaKind
	Span  Span
	Codes []lint.Code
	Shell string
}

func blankRune(r rune) bool { return r == ' ' || r == '\t' }

func cutWord(s, word string) (string, bool) {
	rest, ok := strings.CutPrefix(s, word)
	if !ok || rest != "" && !blankRune(rune(rest[0])) && rest[0] != '=' {
		return s, false
	}
	return strings.TrimLeftFunc(rest, blankRune), true
}

func ParsePragma(comment string) (Pragma, bool) {
	body, ok := strings.CutPrefix(comment, "#")
	if !ok {
		return Pragma{}, false
	}
	body = strings.TrimLeftFunc(body, blankRune)
	rest, ok := strings.CutPrefix(body, pragmaTool)
	if !ok || rest == "" || !blankRune(rune(rest[0])) {
		return Pragma{}, false
	}
	rest = strings.TrimLeftFunc(rest, blankRune)
	if value, ok := strings.CutPrefix(rest, "shell="); ok {
		value = strings.TrimRightFunc(value, blankRune)
		if value == "" || strings.ContainsFunc(value, blankRune) {
			return Pragma{}, false
		}
		return Pragma{Kind: PragmaShell, Shell: value}, true
	}
	kind := PragmaIgnore
	if after, global := cutWord(rest, "global"); global {
		kind, rest = PragmaGlobalIgnore, after
	}
	rest, ok = cutWord(rest, "ignore")
	if !ok {
		return Pragma{}, false
	}
	list, ok := strings.CutPrefix(rest, "=")
	if !ok {
		return Pragma{}, false
	}
	if i := strings.Index(list, "#"); i >= 0 {
		list = list[:i]
	}
	var codes []lint.Code
	for item := range strings.SplitSeq(list, ",") {
		code, err := lint.ParseCode(strings.TrimFunc(item, blankRune))
		if err != nil {
			return Pragma{}, false
		}
		codes = append(codes, code)
	}
	return Pragma{Kind: kind, Codes: codes}, true
}

func (f *File) Pragmas() []Pragma {
	var out []Pragma
	for _, c := range f.Comments {
		i := sort.Search(len(f.Instructions), func(i int) bool { return f.Instructions[i].Span.End > c.Start })
		if i < len(f.Instructions) && f.Instructions[i].Span.Start <= c.Start {
			continue
		}
		if p, ok := ParsePragma(f.Text[c.Start:c.End]); ok {
			p.Span = c
			out = append(out, p)
		}
	}
	return out
}
