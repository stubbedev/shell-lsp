package projection

import (
	"regexp"
	"strings"
)

type heredoc struct {
	delimiter string
	tabs      bool
}

type quoting struct {
	single, double, escaped, comment bool
	prev                             byte
	line                             strings.Builder
	reading                          []heredoc
}

var heredocOperator = regexp.MustCompile(`(?:^|[^<])<<(-?)[ \t]*['"\\]?([A-Za-z0-9_.+-]+)`)

func (q *quoting) feed(s string) {
	for i := range len(s) {
		q.next(s[i])
	}
}

func (q *quoting) next(c byte) {
	if c == '\n' {
		q.endLine()
		return
	}
	q.line.WriteByte(c)
	if len(q.reading) > 0 {
		return
	}
	switch {
	case q.comment:
	case q.escaped:
		q.escaped = false
	case q.single:
		q.single = c != '\''
	case c == '\\':
		q.escaped = true
	case q.double:
		q.double = c != '"'
	case c == '\'':
		q.single = true
	case c == '"':
		q.double = true
	case c == '#' && wordStart(q.prev):
		q.comment = true
	}
	q.prev = c
}

func wordStart(prev byte) bool {
	return prev == 0 || strings.IndexByte(" \t\n;&|()<>", prev) >= 0
}

func (q *quoting) endLine() {
	line := q.line.String()
	q.line.Reset()
	q.comment, q.escaped, q.prev = false, false, '\n'
	if len(q.reading) > 0 {
		h := q.reading[0]
		if h.tabs {
			line = strings.TrimLeft(line, "\t")
		}
		if line == h.delimiter {
			q.reading = q.reading[1:]
		}
		return
	}
	if q.single || q.double {
		return
	}
	for _, m := range heredocOperator.FindAllStringSubmatch(line, -1) {
		q.reading = append(q.reading, heredoc{delimiter: m[2], tabs: m[1] == "-"})
	}
}

func (q *quoting) inSingle() bool {
	return q.single && len(q.reading) == 0
}
