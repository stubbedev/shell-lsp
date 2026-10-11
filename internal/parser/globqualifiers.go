package parser

import (
	"strings"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const explicitQualifierPrefix = "(#q"

func GlobQualifiers(w *syntax.Word, bare bool) int {
	start := len(w.Parts)
	if bare && start > 0 && bareQualifier(w.Parts[start-1]) {
		start--
	}
	for start > 0 && explicitQualifier(w.Parts[start-1]) {
		start--
	}
	return start
}

func group(part syntax.WordPart) (string, bool) {
	lit, ok := part.(*syntax.Lit)
	if !ok || len(lit.Value) < 2 || lit.Value[0] != '(' || lit.Value[len(lit.Value)-1] != ')' {
		return "", false
	}
	return lit.Value, true
}

func explicitQualifier(part syntax.WordPart) bool {
	value, ok := group(part)
	return ok && strings.HasPrefix(value, explicitQualifierPrefix)
}

func bareQualifier(part syntax.WordPart) bool {
	value, ok := group(part)
	if !ok {
		return false
	}
	var quote byte
	for i := 1; i < len(value)-1; i++ {
		switch c := value[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			quote = c
		case c == '|' || c == '(' || c == '~':
			return false
		}
	}
	return true
}
