package lint

import (
	"path"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var grepFamily = family{
	codes: map[Code]meta{
		2022: {severity: SeverityInfo, bug: true, summary: "In a regex, x* repeats the character before it rather than matching anything after it."},
		2062: {severity: SeverityWarning, summary: "An unquoted grep pattern may be expanded as a glob before grep sees it."},
		2063: {severity: SeverityWarning, summary: "grep is given a glob, but it takes a regular expression."},
		2196: {severity: SeverityInfo, tag: TagDeprecated, summary: "egrep is deprecated and missing from some systems."},
		2197: {severity: SeverityInfo, tag: TagDeprecated, summary: "fgrep is deprecated and missing from some systems."},
		2337: {severity: SeverityWarning, summary: "A command that may exit before reading all its input can make the writer upstream fail with SIGPIPE under pipefail."},
	},
	commands: []commandRule{
		{match: matchBase, names: []string{"grep"}, check: checkGrepPattern},
		{match: matchBase, names: []string{"egrep"}, check: deprecatedGrep(2196, "grep -E")},
		{match: matchBase, names: []string{"fgrep"}, check: deprecatedGrep(2197, "grep -F")},
		{match: matchBase, names: []string{"grep", "egrep", "fgrep"}, check: checkEarlyExitGrep},
	},
}

func deprecatedGrep(code Code, replacement string) func(p *pass, c command) {
	return func(p *pass, c command) {
		var fixes []Fix
		if lit, ok := c.word.Parts[0].(*syntax.Lit); ok && len(c.word.Parts) == 1 {
			base := path.Base(lit.Value)
			end := parser.Offset(lit.ValueEnd)
			fixes = append(fixes, safeFix("Replace "+base+" with "+replacement, textedit.Edit{Start: end - len(base), End: end, NewText: replacement}))
		}
		p.reportNode(code, c.word, c.name+" is deprecated and missing from some systems; use "+replacement+" instead.", fixes...)
	}
}

func grepPattern(args []*syntax.Word) (*syntax.Word, bool) {
	for i, arg := range args {
		text, _ := parser.Text(arg, "_")
		switch {
		case text == "--" || text == "-e" || text == "--regex" || text == "--regexp":
			if i+1 < len(args) {
				return args[i+1], true
			}
			return nil, false
		case strings.HasPrefix(text, "-") && !strings.HasPrefix(text, "--regex="):
			continue
		}
		return arg, true
	}
	return nil, false
}

func checkGrepPattern(p *pass, c command) {
	pattern, ok := grepPattern(c.args)
	if !ok {
		return
	}
	if isGlob(pattern) {
		p.reportNode(2062, pattern, "Quote this grep pattern so the shell does not expand it as a glob first.")
	}
	if c.options().Has("-F", "--fixed-strings", "-o", "--only-matching", "--include", "--exclude", "--exclude-dir") {
		return
	}
	text, _ := parser.Text(pattern, "_")
	if strings.HasPrefix(text, "*") || (len(text) == 2 && text[1] == '*' && text[0] != '\\' && text[0] != '.') {
		p.reportNode(2063, pattern, "grep takes a regular expression, not a glob; a leading or lone * is not 'anything'. Use .* to match any text.")
		return
	}
	if starred, ok := globStyleStar(text); ok {
		p.reportNode(2022, pattern, "In a regex, "+string(starred)+"* repeats "+string(starred)+" rather than matching anything after it, so it does not match '"+sampleWord(starred)+"'. Use "+string(starred)+".* for that.")
	}
}

func isRegexStarOperand(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '1' && c <= '9')
}

func globStyleStar(text string) (byte, bool) {
	if strings.ContainsAny(text, "[]^$+\\") {
		return 0, false
	}
	var starred byte
	for i := 1; i < len(text); i++ {
		if text[i] != '*' {
			continue
		}
		if !isRegexStarOperand(text[i-1]) {
			return 0, false
		}
		if starred == 0 {
			starred = text[i-1]
		}
	}
	return starred, starred != 0
}

var phonetic = []string{
	"alfa", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliett", "kilo", "lima", "mike",
	"november", "oscar", "papa", "quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey", "xray", "yankee", "zulu",
}

func sampleWord(c byte) string {
	switch {
	case c >= 'a' && c <= 'z':
		return phonetic[c-'a']
	case c >= 'A' && c <= 'Z':
		word := phonetic[c-'A']
		return strings.ToUpper(word[:1]) + word[1:]
	}
	return string(c) + "test"
}

func checkEarlyExitGrep(p *pass, c command) {
	parsed := c.options()
	if !parsed.Complete || !parsed.Has("-q", "--quiet", "--silent", "-m", "--max-count", "-L") {
		return
	}
	if _, index := p.enclosingPipeline(); index < 1 || !p.options().Pipefail {
		return
	}
	p.reportNode(2337, c.call, c.name+" may exit before reading all its input; with pipefail set, the writer upstream can then fail with SIGPIPE and fail the whole pipeline.")
}

func isShortCluster(lit string) bool {
	return len(lit) > 1 && (lit[0] == '-' || lit[0] == '+') && lit[1] != '-'
}
