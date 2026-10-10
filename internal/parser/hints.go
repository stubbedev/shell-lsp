package parser

import (
	"fmt"
	"strconv"
	"strings"
)

type hint func(e Error, text string) (Error, bool)

var hints = []hint{
	keywordGluedToBracket,
	unclosedHeredoc,
	semicolonAfterAmpersand,
	semicolonAfterKeyword,
	braceGluedToCommand,
	parenthesesAfterName,
	heredocBeforeProcessSubstitution,
	dollarOnAssignedName,
	doubleBracketOperand,
}

func refine(e Error, text string) Error {
	for _, h := range hints {
		if refined, ok := h(e, text); ok {
			return refined
		}
	}
	return e
}

func lineOf(text string, offset int) (start, end int) {
	start = strings.LastIndexByte(text[:offset], '\n') + 1
	end = len(text)
	if i := strings.IndexByte(text[offset:], '\n'); i >= 0 {
		end = offset + i
	}
	return start, end
}

func wordsBefore(text string, offset int) []string {
	start, _ := lineOf(text, offset)
	return strings.Fields(text[start:offset])
}

func previousByte(text string, offset int) byte {
	i := len(strings.TrimRight(text[:offset], " \t"))
	if i == 0 {
		return 0
	}
	return text[i-1]
}

func quotedToken(message, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(message, prefix)
	if !ok {
		return "", false
	}
	token, err := strconv.Unquote(rest)
	return token, err == nil
}

var bracketKeywords = []string{"if", "elif", "while", "until"}

func keywordGluedToBracket(e Error, text string) (Error, bool) {
	start, end := lineOf(text, e.Span.Start)
	line := text[start:end]
	for _, keyword := range bracketKeywords {
		for i := 0; ; {
			j := strings.Index(line[i:], keyword+"[")
			if j < 0 {
				break
			}
			at := i + j
			i = at + len(keyword)
			if at > 0 && !strings.ContainsRune(" \t;&|(", rune(line[at-1])) {
				continue
			}
			bracket := start + at + len(keyword)
			return Error{Span: Span{Start: bracket, End: bracket + 1}, Kind: ErrSyntax, Message: fmt.Sprintf("put a space between `%s` and `[`; glued together they form one word instead of a keyword and a test", keyword)}, true
		}
	}
	return Error{}, false
}

func unclosedHeredoc(e Error, text string) (Error, bool) {
	token, ok := quotedToken(e.Message, "unclosed here-document ")
	if !ok {
		return Error{}, false
	}
	dash := strings.HasPrefix(text[e.Span.Start:], "<<-")
	_, operatorEnd := lineOf(text, e.Span.Start)
	e.Message = heredocDiagnosis(token, dash, text, operatorEnd+1)
	return e, true
}

func heredocDiagnosis(token string, dash bool, text string, bodyStart int) string {
	bare := strings.TrimLeft(token, " \t")
	lineNumber := strings.Count(text[:min(bodyStart, len(text))], "\n") + 1
	var contains, folded string
	containsLine, foldedLine, mistakeLine := 0, 0, 0
	var mistake EndTokenMistake
	for i, line := range strings.Split(text[min(bodyStart, len(text)):], "\n") {
		content := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(content)]
		if nearMiss, _, ok := NearMissEndToken(content, bare); ok && mistake == 0 {
			mistake, mistakeLine = nearMiss, lineNumber+i
		}
		switch {
		case indent != "" && strings.TrimRight(content, " \t\r") == bare && !dash:
			return fmt.Sprintf("here-document `%s` is never closed: the end token on line %d is indented; remove the indentation, or use <<- and indent with tabs", token, lineNumber+i)
		case strings.Contains(indent, " ") && strings.TrimRight(content, " \t\r") == bare:
			return fmt.Sprintf("here-document `%s` is never closed: line %d indents the end token with spaces, but <<- strips only tabs", token, lineNumber+i)
		case contains == "" && strings.Contains(line, token):
			contains, containsLine = line, lineNumber+i
		case folded == "" && strings.Contains(strings.ToLower(line), strings.ToLower(token)):
			folded, foldedLine = line, lineNumber+i
		}
	}
	switch {
	case mistake != 0:
		return fmt.Sprintf("here-document `%s` is never closed: line %d starts with the end token but %s", token, mistakeLine, mistake)
	case contains != "":
		return fmt.Sprintf("here-document `%s` is never closed: line %d (%q) contains the end token, but it must stand alone on its line", token, containsLine, contains)
	case folded != "":
		return fmt.Sprintf("here-document `%s` is never closed: line %d (%q) has the end token in different letter case", token, foldedLine, folded)
	}
	return fmt.Sprintf("here-document `%s` is never closed; end it with a line containing only `%s`", token, token)
}

func semicolonAfterAmpersand(e Error, text string) (Error, bool) {
	if e.Message != "`;` can only immediately follow a statement" || previousByte(text, e.Span.Start) != '&' {
		return Error{}, false
	}
	e.Message = "`&` already ends the command and runs it in the background; remove the `;` after it"
	return e, true
}

func semicolonAfterKeyword(e Error, text string) (Error, bool) {
	keyword, ok := strings.CutSuffix(e.Message, "` must be followed by a statement list")
	if !ok {
		return Error{}, false
	}
	keyword = strings.TrimPrefix(keyword, "`")
	after := strings.TrimLeft(text[min(e.Span.Start+len(keyword), len(text)):], " \t")
	if !strings.HasPrefix(after, ";") || strings.HasPrefix(after, ";;") {
		return Error{}, false
	}
	e.Message = fmt.Sprintf("remove the `;` after `%s`; the keyword is already followed by the commands it introduces", keyword)
	return e, true
}

func braceGluedToCommand(e Error, text string) (Error, bool) {
	if e.Message != "`}` can only be used to close a block" {
		return Error{}, false
	}
	for _, word := range wordsBefore(text, e.Span.Start) {
		if len(word) > 1 && word[0] == '{' && !strings.ContainsAny(word, ",}") {
			e.Message = fmt.Sprintf("put a space after `{` in `%s`; glued to a word, the brace does not open a block", word)
			return e, true
		}
	}
	return Error{}, false
}

func parenthesesAfterName(e Error, text string) (Error, bool) {
	if e.Message != "`foo(` must be followed by `)`" {
		return Error{}, false
	}
	name := text[e.Span.Start:e.Span.End]
	if name == "[" {
		e.Message = "inside `[ ]`, parentheses must be escaped as `\\(` and `\\)`"
		return e, true
	}
	name, _, _ = strings.Cut(name, "(")
	e.Message = fmt.Sprintf("a function definition takes no parameter list; write `%s()` and read the arguments as $1, $2, ... in the body", name)
	return e, true
}

func heredocBeforeProcessSubstitution(e Error, text string) (Error, bool) {
	if !strings.HasPrefix(text[e.Span.Start:], "(") {
		return Error{}, false
	}
	before := strings.TrimRight(text[:e.Span.Start], " \t")
	if !strings.HasSuffix(before, "<<") || strings.HasSuffix(before, "<<<") {
		return Error{}, false
	}
	e.Message = "`<<` starts a here-document; to read from a process substitution write `< <(...)` with a space between the two `<`"
	return e, true
}

func dollarOnAssignedName(e Error, text string) (Error, bool) {
	start, _ := lineOf(text, e.Span.Start)
	wordStart := max(start, strings.LastIndexAny(text[:e.Span.Start], " \t;&|")+1)
	word := text[wordStart:]
	if !strings.HasPrefix(word, "$") {
		return Error{}, false
	}
	end := 1
	for end < len(word) && IsName(word[1:end+1]) {
		end++
	}
	if end == 1 {
		return Error{}, false
	}
	rest := word[end:]
	if strings.HasPrefix(rest, "[") {
		closing := strings.IndexByte(rest, ']')
		if closing < 0 {
			return Error{}, false
		}
		rest = rest[closing+1:]
	}
	if !strings.HasPrefix(strings.TrimPrefix(rest, "+"), "=") {
		return Error{}, false
	}
	return Error{Span: e.Span, Kind: ErrSyntax, Message: fmt.Sprintf("remove the `$` from `%s`: an assignment names the variable without expanding it", word[:end])}, true
}

func doubleBracketOperand(e Error, text string) (Error, bool) {
	operator, ok := quotedToken(e.Message, "not a valid test operator: ")
	if !ok {
		return Error{}, false
	}
	words := wordsBefore(text, e.Span.Start)
	if len(words) == 0 {
		return Error{}, false
	}
	last := words[len(words)-1]
	switch {
	case operator == "]":
		e.Message = "a test opened with `[[` must be closed with `]]`, not `]`"
	case last == "]]" && len(words) > 1:
		e.Message = fmt.Sprintf("`%s` is missing its operand before `]]`", words[len(words)-2])
	case strings.HasSuffix(last, "]]"):
		e.Message = fmt.Sprintf("put a space before `]]`; glued to `%s` it does not close the test", strings.TrimSuffix(last, "]]"))
	case last == "[":
		e.Message = "inside `[[ ]]`, group conditions with `( )`, not with nested `[ ]`"
	case last == `\(` || last == `'('` || last == `"("`:
		e.Message = "inside `[[ ]]`, group conditions with plain `( )`; do not escape or quote the parentheses"
	case len(words) > 1 && words[len(words)-2] == "[[":
		e.Message = fmt.Sprintf("expected a test operator after `%s`; to check whether a command succeeds, run it without `[[ ]]`", last)
	default:
		return Error{}, false
	}
	return e, true
}
