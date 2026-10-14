package expansion

import (
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func (e *Evaluator) modifiers(pe *syntax.ParamExp, s *state) error {
	for _, m := range pe.Modifiers {
		f, err := modifier(m.Value)
		if err != nil {
			return err
		}
		if err := e.eachWord(s, func(w string) (string, error) { return f(w), nil }); err != nil {
			return err
		}
	}
	return nil
}

func modifier(text string) (func(string) string, error) {
	if text == "" {
		return nil, unsupported("an empty modifier")
	}
	if rest, ok := strings.CutPrefix(text, "gs"); ok {
		return substitution(rest, true)
	}
	name, count := text[0], text[1:]
	n := 0
	if count != "" {
		var err error
		if n, err = strconv.Atoi(count); err != nil || n < 0 {
			if name == 's' {
				return substitution(count, false)
			}
			return nil, unsupported("the modifier :%s", text)
		}
	}
	switch name {
	case 'h':
		return func(p string) string { return head(p, n) }, nil
	case 't':
		return func(p string) string { return tail(p, n) }, nil
	case 'r':
		return root, nil
	case 'e':
		return extension, nil
	case 'l':
		return strings.ToLower, nil
	case 'u':
		return strings.ToUpper, nil
	case 's':
		return substitution(count, false)
	}
	return nil, unsupported("the modifier :%s", text)
}

func head(p string, keep int) string {
	if keep > 0 {
		parts := strings.Split(p, "/")
		if keep >= len(parts) {
			return p
		}
		return strings.Join(parts[:keep], "/")
	}
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		if p == "" {
			return "."
		}
		return "/"
	}
	i := strings.LastIndexByte(trimmed, '/')
	if i < 0 {
		return "."
	}
	dir := trimmed[:i+1]
	if stripped := strings.TrimRight(dir, "/"); stripped != "" {
		return stripped
	}
	return dir
}

func tail(p string, keep int) string {
	trimmed := strings.TrimRight(p, "/")
	if keep > 1 {
		parts := strings.Split(trimmed, "/")
		if keep >= len(parts) {
			return trimmed
		}
		return strings.Join(parts[len(parts)-keep:], "/")
	}
	return trimmed[strings.LastIndexByte(trimmed, '/')+1:]
}

func extensionDot(p string) int {
	i := strings.LastIndexByte(p, '.')
	if i < 0 || strings.ContainsRune(p[i:], '/') {
		return -1
	}
	return i
}

func root(p string) string {
	if i := extensionDot(p); i >= 0 {
		return p[:i]
	}
	return p
}

func extension(p string) string {
	if i := extensionDot(p); i >= 0 {
		return p[i+1:]
	}
	return ""
}

func substitution(spec string, all bool) (func(string) string, error) {
	if spec == "" {
		return nil, unsupported("a substitution without a delimiter")
	}
	delim := spec[:1]
	fields := strings.SplitN(spec[1:], delim, 3)
	if len(fields) < 2 || fields[0] == "" {
		return nil, unsupported("a substitution that reuses the previous one")
	}
	from := fields[0]
	var to strings.Builder
	for i := 0; i < len(fields[1]); i++ {
		switch c := fields[1][i]; {
		case c == '\\' && i+1 < len(fields[1]):
			i++
			to.WriteByte(fields[1][i])
		case c == '&':
			to.WriteString(from)
		default:
			to.WriteByte(c)
		}
	}
	n := 1
	if all {
		n = -1
	}
	return func(w string) string { return strings.Replace(w, from, to.String(), n) }, nil
}
