package parser

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/internal/shlex"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

type typoLine struct {
	src        string
	tokens     []shlex.Token
	errorAt    int
	startsLine bool
}

type typo struct {
	find   func(typoLine) string
	notZsh bool
}

var typos = []typo{
	{find: operatorStartingLine},
	{find: keywordGluedToPunctuation},
	{find: elseIf},
	{find: upperCaseKeyword},
	{find: testJoinedWithOption},
	{find: wordAfterCompound, notZsh: true},
	{find: dollarOnLoopVariable},
	{find: spacedArithmeticParentheses},
	{find: singleOpeningParenthesisLoop},
	{find: singleClosingParenthesisLoop},
	{find: arithmeticAssignment},
	{find: testOperatorInArithmetic},
	{find: processSubstitutionAfterDone},
	{find: evalArrayAssignment},
}

var (
	gluedKeywords         = []string{"if", "then", "else", "elif", "fi", "do", "done", "while", "until"}
	lowerCaseKeywords     = []string{"if", "then", "else", "elif", "fi", "for", "while", "until", "do", "done", "case", "esac", "select"}
	testJoins             = map[string]string{"-o": "||", "or": "||", "-a": "&&", "and": "&&"}
	arithmeticComparisons = map[string]string{"-lt": "<", "-gt": ">", "-le": "<=", "-ge": ">=", "-eq": "==", "-ne": "!="}
)

const (
	hintWindow            = 2 << 10
	singleParenthesisLoop = "an arithmetic for loop needs double parentheses: `for ((...))`"
)

func typoHint(text string, at int, d dialect.Dialect) string {
	line, ok := lexTypoLine(text, min(at, len(text)), d)
	if !ok {
		return ""
	}
	for _, t := range typos {
		if t.notZsh && d == dialect.Zsh {
			continue
		}
		if message := t.find(line); message != "" {
			return "; " + message
		}
	}
	return ""
}

func lexTypoLine(text string, at int, d dialect.Dialect) (typoLine, bool) {
	lineStart, lineEnd := textline.Bounds(text, at)
	from, to := max(lineStart, at-hintWindow), min(lineEnd, at+hintWindow)
	src := text[from:to]
	tokens, err := shlex.Lex(src, d)
	if err != nil {
		return typoLine{}, false
	}
	startsLine := from == lineStart
	if !startsLine && len(tokens) > 0 {
		tokens = tokens[1:]
	}
	return typoLine{src: src, tokens: tokens, errorAt: at - from, startsLine: startsLine}, true
}

func (l typoLine) text(t shlex.Token) string {
	return t.Text(l.src)
}

func (l typoLine) is(t shlex.Token, word string) bool {
	literal, ok := t.Literal(l.src)
	return ok && literal == word
}

func (l typoLine) keyword(t shlex.Token, word string) bool {
	return t.Has(shlex.Keyword) && l.is(t, word)
}

func (l typoLine) commandWord(t shlex.Token) (string, bool) {
	if !t.Has(shlex.CommandStart) {
		return "", false
	}
	return t.Literal(l.src)
}

func (l typoLine) doFollows(seq []shlex.Token, i int) bool {
	if i < len(seq) && seq[i].Kind == shlex.Semi {
		i++
	}
	return i < len(seq) && l.is(seq[i], "do")
}

func operatorStartingLine(l typoLine) string {
	if !l.startsLine || len(l.tokens) == 0 {
		return ""
	}
	switch op := l.tokens[0].Kind; op {
	case shlex.AndIf, shlex.OrIf, shlex.Pipe, shlex.PipeAll:
		return "a line cannot start with `" + op.String() + "`; put it at the end of the previous line"
	default:
		return ""
	}
}

func keywordGluedToPunctuation(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		t := seq[i]
		if !t.Has(shlex.CommandStart) || t.Parts[0].Kind != shlex.Literal {
			continue
		}
		text := t.Parts[0].Text(l.src)
		if at := strings.IndexAny(text, "!:#"); at > 0 && slices.Contains(gluedKeywords, text[:at]) {
			return "`" + text[:at+1] + "` is one word, so `" + text[:at] + "` is not a keyword here; put a space after `" + text[:at] + "`"
		}
	}
	return ""
}

func elseIf(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		word, ok := l.commandWord(seq[i])
		if ok && (strings.EqualFold(word, "elseif") || strings.EqualFold(word, "elsif")) {
			return "the shell has no `" + word + "`; write `elif`"
		}
	}
	return ""
}

func upperCaseKeyword(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		word, ok := l.commandWord(seq[i])
		if !ok {
			continue
		}
		k := slices.IndexFunc(lowerCaseKeywords, func(keyword string) bool { return strings.EqualFold(keyword, word) })
		if k >= 0 && lowerCaseKeywords[k] != word {
			return "keywords are lower case, so `" + word + "` is not a keyword; write `" + lowerCaseKeywords[k] + "`"
		}
	}
	return ""
}

func testJoinedWithOption(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		if i+1 == len(seq) || !l.is(seq[i], "]]") {
			continue
		}
		option, ok := seq[i+1].Literal(l.src)
		if join, joins := testJoins[option]; ok && joins {
			return "join test commands with `" + join + "`, not `" + option + "`"
		}
	}
	return ""
}

func wordAfterCompound(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		t := seq[i]
		if t.Start != l.errorAt || i == 0 || !startsCommand(t) {
			continue
		}
		if previous := seq[i-1]; previous.End < t.Start && l.endsCompound(previous) {
			return "`" + l.text(t) + "` cannot follow `" + l.text(previous) + "` directly; separate the commands with `;`, `&&`, `||` or a line break"
		}
	}
	return ""
}

func startsCommand(t shlex.Token) bool {
	if t.Has(shlex.Arithmetic) {
		return false
	}
	switch t.Kind {
	case shlex.Word:
		return t.Parts[0].Kind != shlex.ProcessSubst
	case shlex.LParen, shlex.DLParen, shlex.LBrace:
		return true
	default:
		return false
	}
}

func (l typoLine) endsCompound(t shlex.Token) bool {
	if t.Has(shlex.Arithmetic) {
		return false
	}
	switch t.Kind {
	case shlex.RBrace, shlex.RParen, shlex.DRParen:
		return true
	case shlex.Word:
		return l.is(t, "]]") || l.keyword(t, "done") || l.keyword(t, "fi") || l.keyword(t, "esac")
	default:
		return false
	}
}

func dollarOnLoopVariable(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		if i+1 == len(seq) || !l.keyword(seq[i], "for") || seq[i+1].Kind != shlex.Word {
			continue
		}
		variable := seq[i+1].Parts[0]
		if variable.Kind != shlex.Param {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(variable.Text(l.src), "$"), "{")
		end := 0
		for end < len(name) && scan.NameChar(name[end]) {
			end++
		}
		if end > 0 {
			return "the loop variable is a name; write `for " + name[:end] + "` without `$`"
		}
	}
	return ""
}

func spacedArithmeticParentheses(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		t := seq[i]
		spacedOpening := l.keyword(t, "for") && i+2 < len(seq) && seq[i+1].Kind == shlex.LParen && seq[i+2].Kind == shlex.LParen && seq[i+1].End < seq[i+2].Start
		spacedClosing := t.Kind == shlex.RParen && i+1 < len(seq) && seq[i+1].Kind == shlex.RParen && t.End < seq[i+1].Start && l.doFollows(seq, i+2)
		if spacedOpening || spacedClosing {
			return "the parentheses of an arithmetic for loop must be written `((` and `))` without spaces"
		}
	}
	return ""
}

func singleOpeningParenthesisLoop(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		if i+1 < len(seq) && l.keyword(seq[i], "for") && seq[i+1].Kind == shlex.LParen {
			return singleParenthesisLoop
		}
	}
	return ""
}

func singleClosingParenthesisLoop(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		if i+1 == len(seq) || !l.keyword(seq[i], "for") || seq[i+1].Kind != shlex.DLParen {
			continue
		}
		closing := i + 2
		for closing < len(seq) && seq[closing].Kind != shlex.RParen && seq[closing].Kind != shlex.DRParen {
			closing++
		}
		if closing < len(seq) && seq[closing].Kind == shlex.RParen && l.doFollows(seq, closing+1) {
			return singleParenthesisLoop
		}
	}
	return ""
}

func arithmeticAssignment(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		t := seq[i]
		if t.Kind != shlex.Word || t.Has(shlex.Arithmetic) {
			continue
		}
		if value, ok := shlex.AssignmentPrefix(l.text(t)); ok && strings.HasPrefix(l.src[t.Start+value:], "((") {
			return "use `$((...))` to assign the result of arithmetic"
		}
	}
	return ""
}

func testOperatorInArithmetic(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		t := seq[i]
		if !t.Has(shlex.Arithmetic) {
			continue
		}
		operator, ok := t.Literal(l.src)
		if comparison, compares := arithmeticComparisons[operator]; ok && compares {
			return "inside `(( ))` compare with `" + comparison + "`, not `" + operator + "`"
		}
	}
	return ""
}

func processSubstitutionAfterDone(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		t := seq[i]
		separate := l.keyword(t, "done") && i+1 < len(seq) && seq[i+1].Kind == shlex.Word && l.readsProcess(seq[i+1].Parts[0])
		glued := t.Has(shlex.CommandStart) && len(t.Parts) > 1 && t.Parts[0].Kind == shlex.Literal && t.Parts[0].Text(l.src) == "done" && l.readsProcess(t.Parts[1])
		if separate || glued {
			return "to read from a process substitution write `done < <(...)`"
		}
	}
	return ""
}

func (l typoLine) readsProcess(p shlex.Part) bool {
	return p.Kind == shlex.ProcessSubst && l.src[p.Start] == '<'
}

func evalArrayAssignment(l typoLine) string {
	for seq, i := range shlex.All(l.tokens) {
		if i+1 == len(seq) || !seq[i].Has(shlex.CommandStart) || !l.is(seq[i], "eval") || seq[i+1].Kind != shlex.Word {
			continue
		}
		argument := seq[i+1]
		if value, ok := shlex.AssignmentPrefix(l.text(argument)); ok && strings.HasPrefix(l.src[argument.Start+value:], "(") {
			return "quote the arguments of eval, for example eval \"name=(...)\""
		}
	}
	return ""
}
