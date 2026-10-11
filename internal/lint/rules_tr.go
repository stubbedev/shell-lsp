package lint

import (
	"strings"
	"unicode"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var trFamily = family{
	codes: map[Code]meta{
		2018: {severity: SeverityInfo, bug: true, summary: "The range a-z misses accented and other non-ASCII lowercase letters."},
		2019: {severity: SeverityInfo, bug: true, summary: "The range A-Z misses accented and other non-ASCII uppercase letters."},
		2020: {severity: SeverityInfo, bug: true, summary: "tr maps single characters, not words, so a repeated letter only uses its last mapping."},
		2021: {severity: SeverityInfo, bug: true, summary: "Brackets around a tr range are matched as literal characters."},
		2060: {severity: SeverityWarning, summary: "An unquoted tr set may be expanded as a glob before tr sees it."},
	},
	commands: []commandRule{
		{match: matchBase, names: []string{"tr"}, check: checkTr},
	},
}

func checkTr(p *pass, c command) {
	for _, arg := range c.args {
		if isGlob(arg) {
			p.reportNode(2060, arg, "Quote this tr set; unquoted, the shell may expand it as a glob before tr sees it.")
			continue
		}
		set, literal := parser.Text(arg, "")
		if !literal {
			continue
		}
		switch set {
		case "a-z":
			p.reportNode(2018, arg, "The range a-z misses accented and other non-ASCII lowercase letters; use '[:lower:]'.")
			continue
		case "A-Z":
			p.reportNode(2019, arg, "The range A-Z misses accented and other non-ASCII uppercase letters; use '[:upper:]'.")
			continue
		}
		if !strings.HasPrefix(set, "-") && !strings.Contains(set, "[:") && repeatsLetter(set) {
			p.reportNode(2020, arg, "tr maps single characters, not words; a letter repeated in "+set+" only uses its last mapping. Use sed to replace words.")
		}
		if isBracketedSet(set) {
			p.reportNode(2021, arg, "tr takes ranges without brackets; the brackets in "+set+" are matched as literal characters.")
		}
	}
}

func repeatsLetter(set string) bool {
	seen := map[rune]bool{}
	for _, r := range set {
		if !unicode.IsLetter(r) {
			continue
		}
		if seen[r] {
			return true
		}
		seen[r] = true
	}
	return false
}

func isBracketedSet(set string) bool {
	return len(set) > 2 && strings.HasPrefix(set, "[") && strings.HasSuffix(set, "]") &&
		!strings.Contains(set, "*") && !strings.HasPrefix(set, "[:") && !strings.HasPrefix(set, "[=")
}
