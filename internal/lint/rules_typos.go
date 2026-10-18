package lint

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var typosFamily = family{
	codes: map[Code]meta{
		1075: {severity: SeverityError, summary: "else if starts a nested if that needs its own fi; elif was probably meant."},
		1081: {severity: SeverityError, summary: "Keywords are case sensitive, so a capitalised keyword runs as a command."},
		1097: {severity: SeverityError, summary: "== in an assignment assigns a value starting with = instead of comparing."},
		1099: {severity: SeverityError, summary: "A # glued to a keyword makes one word, so the keyword is not recognised."},
		1127: {severity: SeverityError, summary: "A comment starts with // or a similar marker instead of #, so the line runs as a command."},
		1129: {severity: SeverityError, summary: "A ! glued to a keyword makes one word, so the keyword is not recognised."},
		1130: {severity: SeverityError, summary: "A : glued to a keyword makes one word, so the keyword is not recognised."},
		1131: {severity: SeverityError, summary: "elsif, elseif or a similar spelling is not a keyword; the shell only has elif."},
	},
	nodes: []nodeRule{
		onNode(checkElseIf),
		onNode(checkKeywordLikeCommand),
		onNode(checkDoubleEquals),
	},
}

var spelledKeywords = spelledKeywordsLongestFirst()

func spelledKeywordsLongestFirst() []string {
	var out []string
	for word := range reservedWords {
		if scan.Letter(word[0]) {
			out = append(out, word)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b)) })
	return out
}

var gluedKeywordCodes = map[byte]Code{'#': 1099, '!': 1129, ':': 1130}

func checkElseIf(p *pass, clause *syntax.IfClause) {
	if clause.Else == nil || !parser.IsElse(clause.Else) || clause.Else.Braces || len(clause.Else.Then) == 0 {
		return
	}
	elseClause := clause.Else
	nested, ok := elseClause.Then[0].Cmd.(*syntax.IfClause)
	if !ok || nested.Position.Line() != elseClause.Position.Line() {
		return
	}
	at := parser.Offset(elseClause.Position)
	p.report(1075, parser.Span{Start: at, End: at + len("else")}, "Use elif instead of else if; as written the nested if needs its own fi.")
}

func checkKeywordLikeCommand(p *pass, call *syntax.CallExpr) {
	if len(call.Args) == 0 || len(call.Args[0].Parts) != 1 {
		return
	}
	lit, ok := call.Args[0].Parts[0].(*syntax.Lit)
	if !ok {
		return
	}
	span, ok := p.span(call.Args[0])
	if !ok {
		return
	}
	name := lit.Value
	switch {
	case name == "//" || strings.HasPrefix(name, "/*"):
		p.report(1127, span, "Shell comments start with #, not "+name[:2]+"; this line runs as a command.")
	case isMisspelledElif(name):
		p.report(1131, span, "The shell has no "+leadingLetters(name)+"; write elif.")
	default:
		p.checkKeywordSpelling(name, span)
	}
}

func isMisspelledElif(name string) bool {
	letters := strings.ToLower(leadingLetters(name))
	return letters == "elsif" || letters == "elseif"
}

func leadingLetters(s string) string {
	end := 0
	for end < len(s) && scan.Letter(s[end]) {
		end++
	}
	return s[:end]
}

func (p *pass) checkKeywordSpelling(name string, span parser.Span) {
	for _, keyword := range spelledKeywords {
		if strings.EqualFold(name, keyword) && name != keyword {
			p.report(1081, span, "Keywords are case sensitive, so "+name+" runs as a command; write "+keyword+".")
			return
		}
		rest, ok := strings.CutPrefix(name, keyword)
		if !ok || rest == "" {
			continue
		}
		if code, glued := gluedKeywordCodes[rest[0]]; glued {
			at := span.Start + len(keyword)
			p.report(code, parser.Span{Start: at, End: at + 1}, "Put a space between "+keyword+" and "+rest[:1]+"; glued together they form one word, so "+keyword+" is not a keyword here.")
			return
		}
	}
}

func checkDoubleEquals(p *pass, assign *syntax.Assign) {
	if assign.Name == nil || assign.Value == nil || len(assign.Value.Parts) == 0 {
		return
	}
	lit, ok := assign.Value.Parts[0].(*syntax.Lit)
	if !ok || !strings.HasPrefix(lit.Value, "=") || strings.HasPrefix(lit.Value, "==") {
		return
	}
	span, ok := p.span(lit)
	if !ok {
		return
	}
	p.report(1097, parser.Span{Start: span.Start, End: span.Start + 1}, "Assignments use a single =; this assigns a value starting with = to "+assign.Name.Value+". Use [ ] or (( )) to compare.")
}
