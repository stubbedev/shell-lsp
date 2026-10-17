package dockerfile

import "strings"

type Pair struct {
	Key    Word
	Value  *Word
	Legacy bool
}

func (f *File) quotedWords(in *Instruction) []Word {
	s := in.rest.text
	var out []Word
	for i := skipSpace(s, 0); i < len(s); {
		end := i
		var quote byte
		for end < len(s) && (quote != 0 || !spaceAt(s, end)) {
			c := s[end]
			switch {
			case c == f.Escape && quote != '\'' && end+1 < len(s):
				end++
			case c == '"' || c == '\'':
				quote = toggleQuote(quote, c)
			}
			end++
		}
		out = append(out, Word{Text: s[i:end], Span: in.rest.span(i, end)})
		i = skipSpace(s, end)
	}
	return out
}

func toggleQuote(quote, c byte) byte {
	switch quote {
	case 0:
		return c
	case c:
		return 0
	}
	return quote
}

func (f *File) Pairs(in *Instruction) []Pair {
	words := f.quotedWords(in)
	if len(words) == 0 {
		return nil
	}
	if in.Name != "ARG" && !strings.Contains(words[0].Text, "=") {
		key := words[0]
		p := Pair{Key: key, Legacy: true}
		if len(words) > 1 {
			value := in.rest.text[strings.Index(in.rest.text, words[1].Text):]
			at := len(in.rest.text) - len(value)
			p.Value = &Word{Text: value, Span: in.rest.span(at, len(in.rest.text))}
		}
		return []Pair{p}
	}
	out := make([]Pair, 0, len(words))
	for _, w := range words {
		key, value, has := strings.Cut(w.Text, "=")
		p := Pair{Key: Word{Text: key, Span: Span{Start: w.Span.Start, End: w.Span.Start + len(key)}}}
		if has {
			p.Value = &Word{Text: value, Span: Span{Start: w.Span.Start + len(key) + 1, End: w.Span.End}}
		}
		out = append(out, p)
	}
	return out
}
