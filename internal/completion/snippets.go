package completion

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
)

var keywordSnippets = map[string]string{
	"if":       "if ${1:condition}; then\n\t$0\nfi",
	"for":      "for ${1:name} in ${2:words}; do\n\t$0\ndone",
	"select":   "select ${1:name} in ${2:words}; do\n\t$0\ndone",
	"while":    "while ${1:condition}; do\n\t$0\ndone",
	"until":    "until ${1:condition}; do\n\t$0\ndone",
	"case":     "case ${1:word} in\n\t${2:pattern})\n\t\t$0\n\t\t;;\nesac",
	"function": "function ${1:name} {\n\t$0\n}",
}

type idiom struct {
	label string
	doc   string
	body  func(dialect.Dialect) string
}

var (
	pipefailShells = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Busybox)
	idioms         = []idiom{
		{
			label: "getopts loop",
			doc:   "Parse single-letter options with `getopts`, then drop them from the positional parameters.",
			body: func(dialect.Dialect) string {
				return "while getopts '${1:ab:h}' opt; do\n\tcase \\$opt in\n\t\t${2:a}) $3 ;;\n\t\t\\?) exit 2 ;;\n\tesac\ndone\nshift \\$((OPTIND - 1))\n$0"
			},
		},
		{
			label: "trap cleanup EXIT",
			doc:   "Run a cleanup function however the script exits.",
			body: func(dialect.Dialect) string {
				return "cleanup() {\n\t${1::}\n}\ntrap cleanup EXIT\n$0"
			},
		},
		{
			label: "mktemp with cleanup",
			doc:   "Create a temporary directory and remove it when the script exits.",
			body: func(dialect.Dialect) string {
				return "${1:tmp}=\\$(mktemp -d) || exit\ntrap 'rm -rf -- \"\\$${1:tmp}\"' EXIT\n$0"
			},
		},
		{
			label: "strict mode",
			doc:   "Exit on errors and on unset variables, and make pipelines fail when any command in them fails, where the shell supports it.",
			body: func(d dialect.Dialect) string {
				if pipefailShells.Has(d) {
					return "set -euo pipefail\n$0"
				}
				return "set -eu\n$0"
			},
		},
		{
			label: "main function",
			doc:   "Keep the script's logic in a function called with every argument.",
			body: func(dialect.Dialect) string {
				return "main() {\n\t$0\n}\n\nmain \"\\$@\""
			},
		},
		{
			label: "read lines",
			doc:   "Read a file line by line, keeping leading blanks and backslashes.",
			body: func(dialect.Dialect) string {
				return "while IFS= read -r ${1:line} || [ -n \"\\$${1:line}\" ]; do\n\t$0\ndone < ${2:file}"
			},
		},
		{
			label: "script directory",
			doc:   "Store the directory that contains the script, resolved to an absolute path.",
			body: func(d dialect.Dialect) string {
				switch d {
				case dialect.Bash:
					return "${1:dir}=\\$(cd -- \"\\$(dirname -- \"\\${BASH_SOURCE[0]}\")\" && pwd)\n$0"
				case dialect.Zsh:
					return "${1:dir}=\\${0:A:h}\n$0"
				}
				return "${1:dir}=\\$(cd -- \"\\$(dirname -- \"\\$0\")\" && pwd)\n$0"
			},
		},
	}
)

var snippetMarkup = regexp.MustCompile(`\$\{[0-9]+:([^}]*)\}|\$[0-9]+|\\(.)`)

func snippetPreview(body string) string {
	return snippetMarkup.ReplaceAllStringFunc(body, func(m string) string {
		sub := snippetMarkup.FindStringSubmatch(m)
		switch {
		case strings.HasPrefix(m, "${"):
			return sub[1]
		case strings.HasPrefix(m, `\`):
			return sub[2]
		}
		return ""
	})
}

func (c completer) idioms() []Item {
	if !c.Snippets {
		return nil
	}
	var items []Item
	for _, id := range idioms {
		if !strings.HasPrefix(id.label, c.site.prefix) {
			continue
		}
		body := id.body(c.dialect)
		it := c.item(id.label, KindSnippet, "4")
		it.Detail = "snippet"
		it.Documentation = fmt.Sprintf("%s\n\n```%s\n%s\n```", id.doc, shelldocs.FenceLanguage(c.dialect), strings.TrimRight(snippetPreview(body), "\n"))
		it.InsertText = body
		it.Snippet = true
		items = append(items, it)
	}
	return items
}
