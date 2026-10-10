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
	if lx.arithmetic {
		return site{
			position: positionVariable,
			prefix:   lx.current,
			replace:  parser.Span{Start: offset - len(lx.current), End: offset},
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
	arithmetic   bool
}

type frameMode uint8

const (
	frameCommand frameMode = iota
	frameDouble
	frameArithmetic
)

type frame struct {
	mode  frameMode
	words []string
	cur   strings.Builder
	depth int
}

func lexLine(line string) lexed {
	stack := []*frame{{mode: frameCommand}}
	top := func() *frame { return stack[len(stack)-1] }
	push := func(mode frameMode) { stack = append(stack, &frame{mode: mode}) }
	pop := func() {
		if len(stack) > 1 {
			stack = stack[:len(stack)-1]
		}
	}
	endWord := func() {
		f := top()
		if f.cur.Len() > 0 {
			f.words = append(f.words, f.cur.String())
			f.cur.Reset()
		}
	}
	singleQuoted := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		f := top()
		next := byte(0)
		if i+1 < len(line) {
			next = line[i+1]
		}
		switch {
		case singleQuoted:
			f.cur.WriteByte(c)
			if c == '\'' {
				singleQuoted = false
			}
		case f.mode == frameArithmetic:
			switch {
			case c == ')' && next == ')' && f.depth == 0:
				pop()
				i++
			case c == '(':
				f.depth++
				f.cur.Reset()
			case c == ')':
				f.depth = max(f.depth-1, 0)
				f.cur.Reset()
			case parser.IsNameChar(c):
				f.cur.WriteByte(c)
			default:
				f.cur.Reset()
			}
		case c == '\\':
			if next == '\n' {
				i++
				endWord()
				continue
			}
			f.cur.WriteByte(c)
			if i+1 < len(line) {
				i++
				f.cur.WriteByte(line[i])
			}
		case c == '$' && next == '(' && i+2 < len(line) && line[i+2] == '(':
			push(frameArithmetic)
			i += 2
		case c == '$' && next == '(':
			push(frameCommand)
			i++
		case f.mode == frameDouble:
			f.cur.WriteByte(c)
			if c == '"' {
				pop()
				top().cur.WriteString(f.cur.String())
			}
		case c == '\'':
			singleQuoted = true
			f.cur.WriteByte(c)
		case c == '"':
			push(frameDouble)
			top().cur.WriteByte(c)
		case c == '#' && f.cur.Len() == 0:
			return lexed{comment: true}
		case c == ' ' || c == '\t' || c == '\n':
			endWord()
		case c == '(' && next == '(' && f.cur.Len() == 0 && len(f.words) == 0:
			push(frameArithmetic)
			i++
		case c == ')' && len(stack) > 1:
			pop()
		case c == ';' || c == '&' || c == '|' || c == '(' || c == ')' || c == '`':
			endWord()
			f.words = nil
		case (c == '{' || c == '}') && f.cur.Len() == 0 && (next == 0 || next == ' ' || next == '\t'):
			endWord()
			f.words = nil
		default:
			f.cur.WriteByte(c)
		}
	}
	f := top()
	lx := lexed{current: f.cur.String(), words: f.words, singleQuoted: singleQuoted}
	switch f.mode {
	case frameDouble:
		outer := stack[len(stack)-2]
		lx.doubleQuoted = true
		lx.current = outer.cur.String() + lx.current
		lx.words = outer.words
	case frameArithmetic:
		lx.arithmetic = true
	}
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
