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
	for field := range strings.FieldsSeq(rest) {
		if strings.HasPrefix(field, "#") {
			break
		}
		if key, value, ok := strings.Cut(field, "="); ok {
			d = append(d, Pair{Key: key, Value: value})
		}
	}
	return d, true
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
