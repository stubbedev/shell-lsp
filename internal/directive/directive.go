package directive

import "strings"

const word = "shellcheck"

type Pair struct {
	Key, Value string
}

type Directive []Pair

func Parse(comment string) (Directive, bool) {
	body, ok := strings.CutPrefix(strings.TrimSpace(comment), "#")
	if !ok {
		return nil, false
	}
	rest, ok := strings.CutPrefix(strings.TrimLeft(body, " \t"), word)
	if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
		return nil, false
	}
	var d Directive
	for _, field := range fields(rest) {
		if key, value, ok := strings.Cut(field, "="); ok {
			d = append(d, Pair{Key: key, Value: value})
		}
	}
	return d, true
}

func fields(s string) []string {
	var out []string
	var current strings.Builder
	var quote byte
	inField := false
	for i := range len(s) {
		c := s[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			current.WriteByte(c)
		case c == ' ' || c == '\t':
			if inField {
				out = append(out, current.String())
				current.Reset()
				inField = false
			}
		case c == '#' && !inField:
			return out
		case c == '\'' || c == '"':
			quote, inField = c, true
		default:
			current.WriteByte(c)
			inField = true
		}
	}
	if inField {
		out = append(out, current.String())
	}
	return out
}

func (d Directive) Value(key string) (string, bool) {
	for _, pair := range d {
		if pair.Key == key {
			return pair.Value, true
		}
	}
	return "", false
}

func (d Directive) Values(key string) []string {
	var out []string
	for _, pair := range d {
		if pair.Key == key {
			out = append(out, pair.Value)
		}
	}
	return out
}
