package manpage

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

func validName(name string) bool {
	return name != "" && (scan.Alnum(name[0]) || name[0] == '_') && scan.All(name[1:], func(c byte) bool {
		return scan.Alnum(c) || strings.IndexByte("._+-", c) >= 0
	})
}

func isBareWord(word string) bool {
	last := len(word) - 1
	return last > 0 && scan.Lower(word[0]) && scan.LowerOrDigit(word[last]) && scan.All(word[1:last], func(c byte) bool {
		return scan.LowerOrDigit(c) || c == '_' || c == '-'
	})
}

func splitReference(word string) (string, bool) {
	name, rest, opened := strings.Cut(word, "(")
	section, closed := strings.CutSuffix(rest, ")")
	if !opened || !closed || name == "" || strings.IndexByte(name, ')') >= 0 || !manSection(section) {
		return word, false
	}
	return name, true
}

func manSection(s string) bool { return s != "" && scan.Digit(s[0]) && scan.All(s[1:], scan.Letter) }

func optionName(part string) string {
	dashes := len(part) - len(strings.TrimPrefix(strings.TrimPrefix(part, "-"), "-"))
	if dashes == 0 || len(part) == dashes || !optionStart(part[dashes]) {
		return ""
	}
	return part[:dashes+1+scan.While(part[dashes+1:], optionChar)]
}

func optionStart(c byte) bool { return scan.Alnum(c) || c == '?' || c == '#' || c == '@' }

func optionChar(c byte) bool { return scan.Alnum(c) || c == '_' || c == '-' }
