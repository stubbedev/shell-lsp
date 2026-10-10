package parser

import "strings"

type EndTokenMistake uint8

const (
	TrailingWhitespace EndTokenMistake = iota + 1
	ClosingParenthesis
	TrailingComment
	TrailingOperator
	TrailingText
)

var endTokenMistakes = map[EndTokenMistake]string{
	TrailingWhitespace: "is followed by whitespace",
	ClosingParenthesis: "is followed by `)`, which belongs on the next line",
	TrailingComment:    "is followed by a comment, which belongs on its own line",
	TrailingOperator:   "is followed by an operator, which belongs on the line with `<<`",
	TrailingText:       "is followed by more text",
}

func (m EndTokenMistake) String() string { return endTokenMistakes[m] }

func NearMissEndToken(line, delimiter string) (EndTokenMistake, int, bool) {
	rest, ok := strings.CutPrefix(line, delimiter)
	if !ok || rest == "" || delimiter == "" {
		return 0, 0, false
	}
	trimmed := strings.TrimLeft(rest, " \t\r")
	at := len(line) - len(trimmed)
	switch {
	case trimmed == "":
		return TrailingWhitespace, len(delimiter), true
	case trimmed[0] == ')':
		return ClosingParenthesis, at, true
	case trimmed[0] == '#':
		return TrailingComment, at, true
	case strings.IndexByte(";>|&", trimmed[0]) >= 0:
		return TrailingOperator, at, true
	case len(trimmed) < len(rest):
		return TrailingText, at, true
	}
	return 0, 0, false
}
