package parser

import (
	"regexp"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

type typo struct {
	pattern *regexp.Regexp
	message func(match []string) string
	atError bool
	notZsh  bool
}

var arithmeticComparisons = map[string]string{"lt": "<", "gt": ">", "le": "<=", "ge": ">=", "eq": "==", "ne": "!="}

const commandStart = `(?:^|[;&|({]|\bthen|\bdo|\belse)\s*`

var typoHints = []typo{
	{
		pattern: regexp.MustCompile(`^\s*(&&|\|\||\|)`),
		message: func(m []string) string {
			return "a line cannot start with `" + m[1] + "`; put it at the end of the previous line"
		},
	},
	{
		pattern: regexp.MustCompile(commandStart + `(if|then|else|elif|fi|do|done|while|until)([!:#])`),
		message: func(m []string) string {
			return "`" + m[1] + m[2] + "` is one word, so `" + m[1] + "` is not a keyword here; put a space after `" + m[1] + "`"
		},
	},
	{
		pattern: regexp.MustCompile(commandStart + `(?i)(elseif|elsif)`),
		message: func(m []string) string { return "the shell has no `" + m[1] + "`; write `elif`" },
	},
	{
		pattern: regexp.MustCompile(commandStart + `(?i)(if|then|else|elif|fi|for|while|until|do|done|case|esac|select)\b`),
		message: func(m []string) string {
			if m[1] == strings.ToLower(m[1]) {
				return ""
			}
			return "keywords are lower case, so `" + m[1] + "` is not a keyword; write `" + strings.ToLower(m[1]) + "`"
		},
	},
	{
		pattern: regexp.MustCompile(`\]\]\s*(-o|-a|or|and)\b`),
		message: func(m []string) string {
			join := "||"
			if m[1] == "-a" || m[1] == "and" {
				join = "&&"
			}
			return "join test commands with `" + join + "`, not `" + m[1] + "`"
		},
	},
	{
		pattern: regexp.MustCompile(`(\]\]|\}|\)|\bdone|\bfi|\besac)[ \t]+([^\s;&|)<>]\S*)`),
		message: func(m []string) string {
			return "`" + m[2] + "` cannot follow `" + m[1] + "` directly; separate the commands with `;`, `&&`, `||` or a line break"
		},
		atError: true,
		notZsh:  true,
	},
	{
		pattern: regexp.MustCompile(`\bfor\s+\$\{?(\w+)`),
		message: func(m []string) string { return "the loop variable is a name; write `for " + m[1] + "` without `$`" },
	},
	{
		pattern: regexp.MustCompile(`\bfor\s*\(\s+\(|\)\s+\)\s*;?\s*do\b`),
		message: func([]string) string {
			return "the parentheses of an arithmetic for loop must be written `((` and `))` without spaces"
		},
	},
	{
		pattern: regexp.MustCompile(`\bfor\s*\([^(]`),
		message: func([]string) string { return "an arithmetic for loop needs double parentheses: `for ((...))`" },
	},
	{
		pattern: regexp.MustCompile(`\bfor\s*\(\([^)]*\)\s*;?\s*do\b`),
		message: func([]string) string { return "an arithmetic for loop needs double parentheses: `for ((...))`" },
	},
	{
		pattern: regexp.MustCompile(`\b\w+=\(\(`),
		message: func([]string) string { return "use `$((...))` to assign the result of arithmetic" },
	},
	{
		pattern: regexp.MustCompile(`\(\(.*\s-(lt|gt|le|ge|eq|ne)\s`),
		message: func(m []string) string {
			return "inside `(( ))` compare with `" + arithmeticComparisons[m[1]] + "`, not `-" + m[1] + "`"
		},
	},
	{
		pattern: regexp.MustCompile(`\bdone\s*<\(`),
		message: func([]string) string { return "to read from a process substitution write `done < <(...)`" },
	},
	{
		pattern: regexp.MustCompile(`\beval\s+\w+=\(`),
		message: func([]string) string { return "quote the arguments of eval, for example eval \"name=(...)\"" },
	},
}

const (
	hintWindow  = 2 << 10
	clippedLine = "x"
)

func hintLine(text string, at int) (string, int) {
	lineStart, lineEnd := textline.Bounds(text, at)
	from, to := max(lineStart, at-hintWindow), min(lineEnd, at+hintWindow)
	if from == lineStart {
		return text[from:to], at - from
	}
	return clippedLine + text[from:to], at - from + len(clippedLine)
}

func typoHint(text string, at int, d dialect.Dialect) string {
	line, errorAt := hintLine(text, min(at, len(text)))
	for _, h := range typoHints {
		for _, indexes := range h.pattern.FindAllStringSubmatchIndex(line, -1) {
			if (h.atError && indexes[len(indexes)-2] != errorAt) || (h.notZsh && d == dialect.Zsh) {
				continue
			}
			match := make([]string, len(indexes)/2)
			for i := range match {
				if indexes[2*i] >= 0 {
					match[i] = line[indexes[2*i]:indexes[2*i+1]]
				}
			}
			if message := h.message(match); message != "" {
				return "; " + message
			}
		}
	}
	return ""
}
