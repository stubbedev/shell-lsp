package dockerfile

import "strings"

type VarRef struct {
	Name string
	Span Span
}

func nameStart(c byte) bool { return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

func nameChar(c byte) bool { return nameStart(c) || c >= '0' && c <= '9' }

func nameEnd(s string, i int) int {
	if i >= len(s) || !nameStart(s[i]) {
		return i
	}
	for i < len(s) && nameChar(s[i]) {
		i++
	}
	return i
}

func digit(c byte) bool { return c >= '0' && c <= '9' }

func paramEnd(s string, i int, braced bool) int {
	switch {
	case i >= len(s) || !digit(s[i]):
		return nameEnd(s, i)
	case !braced:
		return i + 1
	}
	for i < len(s) && digit(s[i]) {
		i++
	}
	return i
}

func (f *File) References(w Word) []VarRef {
	if f.Text[w.Span.Start:w.Span.End] == w.Text {
		return substitutions(w.Text, w.Span.Start, f.Escape)
	}
	refs := substitutions(w.Text, 0, f.Escape)
	for i := range refs {
		refs[i].Span = w.Span
	}
	return refs
}

func substitutions(s string, base int, escape byte) []VarRef {
	var out []VarRef
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == escape && quote != '\'' && i+1 < len(s):
			i++
		case c == '\'' && quote != '"':
			quote = toggle(quote, '\'')
		case c == '"' && quote != '\'':
			quote = toggle(quote, '"')
		case c == '$' && quote != '\'' && i+1 < len(s):
			refs, end := substitution(s, i, base, escape)
			out = append(out, refs...)
			i = end - 1
		}
	}
	return out
}

func toggle(quote, q byte) byte {
	if quote == q {
		return 0
	}
	return q
}

func substitution(s string, dollar, base int, escape byte) ([]VarRef, int) {
	if s[dollar+1] != '{' {
		end := paramEnd(s, dollar+1, false)
		if end == dollar+1 {
			return nil, dollar + 1
		}
		return []VarRef{{Name: s[dollar+1 : end], Span: Span{Start: base + dollar + 1, End: base + end}}}, end
	}
	start := dollar + 2
	end := paramEnd(s, start, true)
	if end == start {
		return nil, dollar + 2
	}
	out := []VarRef{{Name: s[start:end], Span: Span{Start: base + start, End: base + end}}}
	depth, i := 1, end
	for i < len(s) && depth > 0 {
		switch {
		case s[i] == escape && i+1 < len(s):
			i++
		case s[i] == '{':
			depth++
		case s[i] == '}':
			depth--
		}
		i++
	}
	if word := s[end:max(end, i-1)]; strings.ContainsRune(word, '$') {
		out = append(out, substitutions(word, base+end, escape)...)
	}
	return out, i
}
