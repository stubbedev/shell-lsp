package completion

import (
	"regexp"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

type position uint8

const (
	positionNone position = iota
	positionVariable
	positionCommand
	positionArgument
)

type site struct {
	position  position
	prefix    string
	replace   parser.Span
	command   string
	previous  []string
	paramForm string
}

var (
	variablePrefix = regexp.MustCompile(`\$(\{[#!]?)?([A-Za-z_][A-Za-z0-9_]*)?$`)
	assignmentWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=`)
)

var commandOpeners = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true,
	"!": true, "time": true, "{": true, "coproc": true,
}

func analyze(tree *parser.Tree, offset int) site {
	text := tree.Text()
	offset = min(max(offset, 0), len(text))
	if inComment(tree, offset) {
		return site{}
	}
	if doc, ok := heredocAt(tree, offset); ok {
		if doc.Quoted {
			return site{}
		}
		return variableContext(text, lineStart(text, offset), offset)
	}
	start := logicalLineStart(text, offset)
	lx := lexLine(text[start:offset])
	if lx.comment {
		return site{}
	}
	if !lx.singleQuoted {
		if st := variableContext(text, start, offset); st.position == positionVariable {
			return st
		}
	}
	if (lx.singleQuoted || lx.doubleQuoted) && !startsQuoted(lx.current) {
		return site{}
	}
	st := site{
		prefix:  lx.current,
		replace: parser.Span{Start: offset - len(lx.current), End: offset},
	}
	words := lx.words
	for len(words) > 0 && (commandOpeners[words[0]] || assignmentWord.MatchString(words[0])) {
		words = words[1:]
	}
	if len(words) == 0 {
		st.position = positionCommand
		return st
	}
	st.position = positionArgument
	st.command = words[0]
	st.previous = words[1:]
	return st
}

func startsQuoted(word string) bool {
	return strings.HasPrefix(word, "'") || strings.HasPrefix(word, `"`)
}

func variableContext(text string, from, offset int) site {
	match := variablePrefix.FindStringSubmatchIndex(text[from:offset])
	if match == nil || (from+match[0] > 0 && text[from+match[0]-1] == '\\') {
		return site{}
	}
	nameStart := from + match[1]
	if match[4] >= 0 {
		nameStart = from + match[4]
	}
	form := ""
	if match[2] >= 0 {
		form = text[from+match[2] : from+match[3]]
	}
	return site{
		position:  positionVariable,
		prefix:    text[nameStart:offset],
		replace:   parser.Span{Start: nameStart, End: offset},
		paramForm: form,
	}
}

type lexed struct {
	words        []string
	current      string
	comment      bool
	singleQuoted bool
	doubleQuoted bool
}

func lexLine(line string) lexed {
	var lx lexed
	var cur strings.Builder
	endWord := func() {
		if cur.Len() > 0 {
			lx.words = append(lx.words, cur.String())
			cur.Reset()
		}
	}
	reset := func() {
		endWord()
		lx.words = nil
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case lx.singleQuoted:
			cur.WriteByte(c)
			if c == '\'' {
				lx.singleQuoted = false
			}
		case lx.doubleQuoted:
			cur.WriteByte(c)
			switch c {
			case '\\':
				if i+1 < len(line) {
					i++
					cur.WriteByte(line[i])
				}
			case '"':
				lx.doubleQuoted = false
			}
		case c == '\\':
			if i+1 < len(line) && line[i+1] == '\n' {
				i++
				endWord()
				continue
			}
			cur.WriteByte(c)
			if i+1 < len(line) {
				i++
				cur.WriteByte(line[i])
			}
		case c == '\'':
			lx.singleQuoted = true
			cur.WriteByte(c)
		case c == '"':
			lx.doubleQuoted = true
			cur.WriteByte(c)
		case c == '#' && cur.Len() == 0:
			lx.comment = true
			return lx
		case c == ' ' || c == '\t' || c == '\n':
			endWord()
		case c == '$' && i+1 < len(line) && line[i+1] == '(':
			reset()
			i++
		case c == ';' || c == '&' || c == '|' || c == '(' || c == ')' || c == '`':
			reset()
		case (c == '{' || c == '}') && cur.Len() == 0 && (i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '\t'):
			reset()
		default:
			cur.WriteByte(c)
		}
	}
	lx.current = cur.String()
	return lx
}

func inComment(tree *parser.Tree, offset int) bool {
	for _, c := range tree.Comments {
		if c.Span.Start < offset && offset <= c.Span.End {
			return true
		}
	}
	return false
}

func heredocAt(tree *parser.Tree, offset int) (parser.Heredoc, bool) {
	for _, h := range tree.Heredocs {
		if h.Body.Start <= offset && offset < h.Body.End {
			return h, true
		}
	}
	return parser.Heredoc{}, false
}

func lineStart(text string, offset int) int {
	return strings.LastIndexByte(text[:offset], '\n') + 1
}

func logicalLineStart(text string, offset int) int {
	start := lineStart(text, offset)
	for start >= 2 && text[start-2] == '\\' {
		start = lineStart(text, start-1)
	}
	return start
}

func CommandAt(tree *parser.Tree, offset int) (string, int, bool) {
	st := analyze(tree, offset)
	if st.position != positionArgument {
		return "", 0, false
	}
	return st.command, len(st.previous), true
}
